package modes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer"
	"github.com/supporttools/dr-syncer/pkg/controllers/watch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultSchedule is the default cron schedule for replication (every 5 minutes)
	DefaultSchedule = "*/5 * * * *"
)

// ModeReconciler handles reconciliation for different replication modes
type ModeReconciler struct {
	client.Client
	sourceClient dynamic.Interface
	destClient   dynamic.Interface
	k8sSource    kubernetes.Interface
	k8sDest      kubernetes.Interface
	watchManager *watch.WatchManager
}

// NewModeReconciler creates a new ModeReconciler
func NewModeReconciler(c client.Client, sourceClient, destClient dynamic.Interface, k8sSource, k8sDest kubernetes.Interface) *ModeReconciler {
	return &ModeReconciler{
		Client:       c,
		sourceClient: sourceClient,
		destClient:   destClient,
		k8sSource:    k8sSource,
		k8sDest:      k8sDest,
		watchManager: watch.NewWatchManager(sourceClient, destClient),
	}
}

// ReconcileScheduled handles scheduled replication mode
func (r *ModeReconciler) ReconcileScheduled(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.V(1).Info("starting scheduled reconciliation",
		"sourceNS", replication.Spec.SourceNamespace,
		"destNS", replication.Spec.DestinationNamespace,
		"schedule", replication.Spec.Schedule,
		"sourceCluster", replication.Spec.SourceCluster,
		"destinationCluster", replication.Spec.DestinationCluster)

	// Update status to Running
	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		now := metav1.Now()
		status.Phase = drv1alpha1.SyncPhaseRunning
		status.LastSyncTime = &now
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Sync resources
	startTime := time.Now()
	deploymentScales, err := r.syncResources(ctx, replication)
	syncDuration := time.Since(startTime)

	if err != nil {
		log.Error(err, "failed to sync resources")
		shouldRetry, backoff, retryErr := r.handleRetry(ctx, replication, err)
		if retryErr != nil {
			log.Error(retryErr, "failed to handle retry")
			return ctrl.Result{}, retryErr
		}
		if shouldRetry {
			log.V(1).Info("retrying sync after backoff", "backoff", backoff)
			return ctrl.Result{RequeueAfter: backoff}, nil
		}
		return ctrl.Result{}, err
	}

	// Reset retry status and update final status
	if err := r.resetRetryStatus(ctx, replication); err != nil {
		log.Error(err, "failed to reset retry status")
	}

	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		now := metav1.Now()
		status.Phase = drv1alpha1.SyncPhaseCompleted
		status.LastSyncTime = &now
		status.DeploymentScales = deploymentScales
		status.SyncStats = &drv1alpha1.SyncStats{
			TotalResources:   int32(len(deploymentScales)),
			SuccessfulSyncs:  int32(len(deploymentScales)),
			FailedSyncs:      0,
			LastSyncDuration: formatDuration(syncDuration),
		}

		// Update the Synced condition
		syncedCondition := metav1.Condition{
			Type:               "Synced",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "SyncCompleted",
			Message:            "Resources successfully synced",
		}

		// Update conditions
		if status.Conditions == nil {
			status.Conditions = []metav1.Condition{}
		}

		// Remove old Synced condition if it exists
		conditions := []metav1.Condition{}
		for _, condition := range status.Conditions {
			if condition.Type != "Synced" {
				conditions = append(conditions, condition)
			}
		}
		conditions = append(conditions, syncedCondition)
		status.Conditions = conditions

		// Get schedule with default
		schedule := replication.Spec.Schedule
		if schedule == "" {
			log.V(1).Info("no schedule specified, using default",
				"defaultSchedule", DefaultSchedule)
			schedule = DefaultSchedule
		}

		log.V(1).Info("calculating next run time",
			"schedule", schedule,
			"isDefault", schedule == DefaultSchedule)

		cronSchedule, err := cron.ParseStandard(schedule)
		if err != nil {
			log.Error(err, "invalid schedule, using default interval",
				"schedule", schedule,
				"defaultInterval", "5m")
			status.NextSyncTime = &metav1.Time{Time: time.Now().Add(5 * time.Minute)}
		} else {
			// Calculate exact next run time
			now := time.Now()
			nextRun := cronSchedule.Next(now)
			status.NextSyncTime = &metav1.Time{Time: nextRun}

			log.V(1).Info("calculated next sync time",
				"currentTime", now.Format(time.RFC3339),
				"nextRunTime", nextRun.Format(time.RFC3339),
				"interval", nextRun.Sub(now).String())
		}
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Use the same next sync time for requeue
	if replication.Status.NextSyncTime == nil {
		log.Error(nil, "next sync time not set, using default 5 minute interval")
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	requeueAfter := time.Until(replication.Status.NextSyncTime.Time)
	log.V(1).Info("scheduled reconciliation complete",
		"duration", time.Since(startTime),
		"schedule", replication.Spec.Schedule,
		"currentTime", time.Now().Format(time.RFC3339),
		"nextRunTime", replication.Status.NextSyncTime.Format(time.RFC3339),
		"requeueAfter", requeueAfter.String())

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// ReconcileContinuous handles continuous replication mode
func (r *ModeReconciler) ReconcileContinuous(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.V(1).Info("starting continuous reconciliation",
		"sourceNS", replication.Spec.SourceNamespace,
		"destNS", replication.Spec.DestinationNamespace,
		"sourceCluster", replication.Spec.SourceCluster,
		"destinationCluster", replication.Spec.DestinationCluster)

	// If not already watching, start watching resources
	if !r.watchManager.IsWatching() {
		resources := r.getResourceGVRs(replication.Spec.ResourceTypes)
		log.V(1).Info("starting resource watchers",
			"resourceTypes", replication.Spec.ResourceTypes,
			"resourceCount", len(resources))

		err := r.watchManager.StartWatching(ctx, replication.Spec.SourceNamespace, resources,
			func(obj interface{}) error {
				// Start sync and update status
				startTime := time.Now()
				if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
					now := metav1.Now()
					status.Phase = drv1alpha1.SyncPhaseRunning
					status.LastSyncTime = &now
					status.LastWatchEvent = &now
				}); err != nil {
					return err
				}

				// Handle resource sync
				deploymentScales, err := r.syncResources(ctx, replication)
				syncDuration := time.Since(startTime)

				if err != nil {
					log.Error(err, "failed to sync resources after watch event")
					shouldRetry, backoff, retryErr := r.handleRetry(ctx, replication, err)
					if retryErr != nil {
						log.Error(retryErr, "failed to handle retry")
						return retryErr
					}
					if shouldRetry {
						log.V(1).Info("retrying sync after backoff", "backoff", backoff)
						time.Sleep(backoff) // For continuous mode, we sleep here instead of requeueing
						return nil          // Continue watching
					}
					return err
				}

				// Reset retry status and update success status
				if err := r.resetRetryStatus(ctx, replication); err != nil {
					log.Error(err, "failed to reset retry status")
				}

				if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
					now := metav1.Now()
					status.Phase = drv1alpha1.SyncPhaseCompleted
					status.LastSyncTime = &now
					status.LastWatchEvent = &now
					status.DeploymentScales = deploymentScales
					status.SyncStats = &drv1alpha1.SyncStats{
						TotalResources:   int32(len(deploymentScales)),
						SuccessfulSyncs:  int32(len(deploymentScales)),
						FailedSyncs:      0,
						LastSyncDuration: formatDuration(syncDuration),
					}

					// Update the Synced condition
					syncedCondition := metav1.Condition{
						Type:               "Synced",
						Status:             metav1.ConditionTrue,
						LastTransitionTime: now,
						Reason:             "SyncCompleted",
						Message:            "Resources successfully synced",
					}

					// Update conditions
					if status.Conditions == nil {
						status.Conditions = []metav1.Condition{}
					}

					// Remove old Synced condition if it exists
					conditions := []metav1.Condition{}
					for _, condition := range status.Conditions {
						if condition.Type != "Synced" {
							conditions = append(conditions, condition)
						}
					}
					conditions = append(conditions, syncedCondition)
					status.Conditions = conditions
				}); err != nil {
					return err
				}

				log.V(1).Info("watch event sync complete",
					"duration", syncDuration,
					"resourceCount", len(deploymentScales))

				return nil
			})
		if err != nil {
			log.Error(err, "failed to start watching resources")
			return ctrl.Result{}, err
		}

		// Start background sync if configured
		if replication.Spec.Continuous != nil && replication.Spec.Continuous.BackgroundSyncInterval != "" {
			interval, err := time.ParseDuration(replication.Spec.Continuous.BackgroundSyncInterval)
			if err != nil {
				log.Error(err, "invalid background sync interval")
				return ctrl.Result{}, err
			}

			log.V(1).Info("starting background sync",
				"interval", interval)

			r.watchManager.StartBackgroundSync(ctx, interval, func() error {
				_, err := r.syncResources(ctx, replication)
				return err
			})
		}
	}

	log.V(1).Info("continuous reconciliation complete")

	// Requeue to periodically check watch status
	return ctrl.Result{RequeueAfter: time.Hour}, nil
}

// ReconcileManual handles manual replication mode
func (r *ModeReconciler) ReconcileManual(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.V(1).Info("starting manual reconciliation",
		"sourceNS", replication.Spec.SourceNamespace,
		"destNS", replication.Spec.DestinationNamespace)

	// Update status to Running
	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		now := metav1.Now()
		status.Phase = drv1alpha1.SyncPhaseRunning
		status.LastSyncTime = &now
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Sync resources
	startTime := time.Now()
	deploymentScales, err := r.syncResources(ctx, replication)
	syncDuration := time.Since(startTime)

	if err != nil {
		log.Error(err, "failed to sync resources")
		shouldRetry, backoff, retryErr := r.handleRetry(ctx, replication, err)
		if retryErr != nil {
			log.Error(retryErr, "failed to handle retry")
			return ctrl.Result{}, retryErr
		}
		if shouldRetry {
			log.V(1).Info("retrying sync after backoff", "backoff", backoff)
			return ctrl.Result{RequeueAfter: backoff}, nil
		}
		return ctrl.Result{}, err
	}

	// Reset retry status and update final status
	if err := r.resetRetryStatus(ctx, replication); err != nil {
		log.Error(err, "failed to reset retry status")
	}

	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		now := metav1.Now()
		status.Phase = drv1alpha1.SyncPhaseCompleted
		status.LastSyncTime = &now
		status.DeploymentScales = deploymentScales
		status.SyncStats = &drv1alpha1.SyncStats{
			TotalResources:   int32(len(deploymentScales)),
			SuccessfulSyncs:  int32(len(deploymentScales)),
			FailedSyncs:      0,
			LastSyncDuration: formatDuration(syncDuration),
		}

		// Update the Synced condition
		syncedCondition := metav1.Condition{
			Type:               "Synced",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "SyncCompleted",
			Message:            "Resources successfully synced",
		}

		// Update conditions
		if status.Conditions == nil {
			status.Conditions = []metav1.Condition{}
		}

		// Remove old Synced condition if it exists
		conditions := []metav1.Condition{}
		for _, condition := range status.Conditions {
			if condition.Type != "Synced" {
				conditions = append(conditions, condition)
			}
		}
		conditions = append(conditions, syncedCondition)
		status.Conditions = conditions
	}); err != nil {
		return ctrl.Result{}, err
	}

	log.V(1).Info("manual reconciliation complete",
		"duration", syncDuration,
		"resourceCount", len(deploymentScales))

	return ctrl.Result{}, nil
}

// syncResources performs the actual resource synchronization
func (r *ModeReconciler) syncResources(ctx context.Context, replication *drv1alpha1.Replication) ([]drv1alpha1.DeploymentScale, error) {
	log := log.FromContext(ctx)
	startTime := time.Now()

	log.V(1).Info("starting resource sync",
		"sourceNS", replication.Spec.SourceNamespace,
		"destNS", replication.Spec.DestinationNamespace,
		"sourceCluster", replication.Spec.SourceCluster,
		"destinationCluster", replication.Spec.DestinationCluster)

	// Determine destination namespace
	dstNamespace := replication.Spec.DestinationNamespace
	if dstNamespace == "" {
		dstNamespace = replication.Spec.SourceNamespace
	}

	// Determine if deployments should be scaled to zero
	scaleToZero := true
	if replication.Spec.ScaleToZero != nil {
		scaleToZero = *replication.Spec.ScaleToZero
	}

	// Determine resource types
	resourceTypes := replication.Spec.ResourceTypes
	defaultTypes := []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}

	// Normalize resource types
	normalizedTypes := make([]string, len(resourceTypes))
	for i, rt := range resourceTypes {
		normalizedTypes[i] = strings.ToLower(rt)
	}

	// Handle empty or wildcard resource types
	if len(normalizedTypes) == 0 || (len(normalizedTypes) == 1 && normalizedTypes[0] == "*") {
		normalizedTypes = defaultTypes
	}

	log.V(1).Info("syncing resources",
		"originalTypes", resourceTypes,
		"normalizedTypes", normalizedTypes,
		"scaleToZero", scaleToZero,
		"isWildcard", len(replication.Spec.ResourceTypes) == 1 && replication.Spec.ResourceTypes[0] == "*",
		"sourceCluster", replication.Spec.SourceCluster,
		"destinationCluster", replication.Spec.DestinationCluster)

	// Sync resources
	syncerScales, err := syncer.SyncNamespaceResources(
		ctx,
		r.k8sSource,
		r.k8sDest,
		r.sourceClient,
		r.destClient,
		r.Client,
		replication.Spec.SourceNamespace,
		dstNamespace,
		normalizedTypes,
		scaleToZero,
		replication.Spec.NamespaceScopedResources,
		replication.Spec.PVCConfig,
		replication.Spec.ImmutableResourceConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sync namespace resources: %w", err)
	}

	// Convert syncer.DeploymentScale to drv1alpha1.DeploymentScale
	result := make([]drv1alpha1.DeploymentScale, len(syncerScales))
	for i, scale := range syncerScales {
		result[i] = drv1alpha1.DeploymentScale{
			Name:             scale.Name,
			OriginalReplicas: scale.Replicas,
			LastSyncedAt:     &scale.SyncTime,
		}
	}

	log.V(1).Info("resource sync complete",
		"duration", time.Since(startTime),
		"deploymentCount", len(result),
		"sourceCluster", replication.Spec.SourceCluster,
		"destinationCluster", replication.Spec.DestinationCluster)

	return result, nil
}

// formatDuration formats a duration to match the pattern ^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
func formatDuration(d time.Duration) string {
	h := d / time.Hour
	d = d % time.Hour
	m := d / time.Minute
	d = d % time.Minute
	s := d / time.Second

	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	if s > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", s))
	}
	return strings.Join(parts, "")
}

// updateStatus updates the status of a Replication resource using optimistic concurrency control
func (r *ModeReconciler) updateStatus(ctx context.Context, replication *drv1alpha1.Replication, updateFn func(*drv1alpha1.ReplicationStatus)) error {
	log := log.FromContext(ctx)
	maxRetries := 5                      // Reduced from 10 to avoid excessive retries
	retryDelay := 250 * time.Millisecond // Increased initial delay

	key := client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}

	for i := 0; i < maxRetries; i++ {
		// Get latest version
		var latest drv1alpha1.Replication
		if err := r.Get(ctx, key, &latest); err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}

		// Log current status details
		log.V(1).Info("current status before update",
			"phase", latest.Status.Phase,
			"lastSyncTime", latest.Status.LastSyncTime,
			"resourceVersion", latest.ResourceVersion,
			"deploymentScales", len(latest.Status.DeploymentScales))

		// Store current status and apply update
		oldStatus := latest.Status.DeepCopy()
		updateFn(&latest.Status)

		// Log proposed changes
		log.V(1).Info("proposed status changes",
			"phase", latest.Status.Phase,
			"lastSyncTime", latest.Status.LastSyncTime,
			"deploymentScales", len(latest.Status.DeploymentScales))

		// Check if status actually changed
		if statusEqual(oldStatus, &latest.Status) {
			log.V(1).Info("status unchanged after update function",
				"resourceVersion", latest.ResourceVersion)
			replication.Status = latest.Status
			return nil
		}

		log.V(1).Info("attempting status update",
			"currentResourceVersion", latest.ResourceVersion,
			"originalResourceVersion", replication.ResourceVersion)

		// Log status comparison details
		log.V(1).Info("comparing status fields",
			"phaseChanged", latest.Status.Phase != oldStatus.Phase,
			"lastSyncTimeChanged", !timeEqual(latest.Status.LastSyncTime, oldStatus.LastSyncTime),
			"nextSyncTimeChanged", !timeEqual(latest.Status.NextSyncTime, oldStatus.NextSyncTime),
			"deploymentScalesChanged", len(latest.Status.DeploymentScales) != len(oldStatus.DeploymentScales))

		// Try to update
		err := r.Status().Update(ctx, &latest)
		if err == nil {
			log.V(1).Info("status update successful",
				"newResourceVersion", latest.ResourceVersion,
				"phase", latest.Status.Phase,
				"lastSyncTime", latest.Status.LastSyncTime)
			replication.Status = latest.Status
			return nil
		}

		if !apierrors.IsConflict(err) {
			// Return non-conflict errors immediately
			return fmt.Errorf("failed to update status: %w", err)
		}

		// For conflict errors, check if the conflict is due to a status change
		// that would make our update unnecessary
		var conflicted drv1alpha1.Replication
		if err := r.Get(ctx, key, &conflicted); err != nil {
			return fmt.Errorf("failed to get latest version after conflict: %w", err)
		}

		// If the conflicted version has the same status we were trying to set,
		// we can consider this a success
		if statusEqual(&conflicted.Status, &latest.Status) {
			log.V(1).Info("conflict resolved - desired status already set",
				"resourceVersion", conflicted.ResourceVersion)
			replication.Status = conflicted.Status
			return nil
		}

		// Log detailed conflict information
		log.V(1).Info("status update conflict detected",
			"attempt", i+1,
			"maxRetries", maxRetries,
			"delay", retryDelay,
			"originalResourceVersion", replication.ResourceVersion,
			"latestResourceVersion", latest.ResourceVersion,
			"conflictedResourceVersion", conflicted.ResourceVersion)

		// Wait before retrying
		time.Sleep(retryDelay)
		retryDelay = time.Duration(float64(retryDelay) * 1.2) // Even gentler backoff (20% increase instead of 50%)
	}

	return fmt.Errorf("failed to update status after %d attempts", maxRetries)
}

// statusEqual compares two ReplicationStatus objects
func statusEqual(a, b *drv1alpha1.ReplicationStatus) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Compare relevant fields
	if a.Phase != b.Phase {
		return false
	}
	if !timeEqual(a.LastSyncTime, b.LastSyncTime) {
		return false
	}
	if !timeEqual(a.NextSyncTime, b.NextSyncTime) {
		return false
	}
	if !timeEqual(a.LastWatchEvent, b.LastWatchEvent) {
		return false
	}

	// Compare deployment scales
	if len(a.DeploymentScales) != len(b.DeploymentScales) {
		return false
	}
	for i := range a.DeploymentScales {
		if a.DeploymentScales[i] != b.DeploymentScales[i] {
			return false
		}
	}

	// Compare sync stats
	if !syncStatsEqual(a.SyncStats, b.SyncStats) {
		return false
	}

	// Compare conditions
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if !conditionEqual(&a.Conditions[i], &b.Conditions[i]) {
			return false
		}
	}

	return true
}

// timeEqual compares two metav1.Time pointers
func timeEqual(a, b *metav1.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(b)
}

// syncStatsEqual compares two SyncStats pointers
func syncStatsEqual(a, b *drv1alpha1.SyncStats) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// conditionEqual compares two metav1.Condition pointers
func conditionEqual(a, b *metav1.Condition) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// calculateBackoff calculates the next backoff duration based on the RetryConfig and current RetryStatus
func calculateBackoff(config *drv1alpha1.RetryConfig, status *drv1alpha1.RetryStatus) (time.Duration, error) {
	// Use defaults if config is nil
	initialBackoff := "5s"
	maxBackoff := "5m"
	backoffMultiplier := int32(200) // 200% = 2x

	if config != nil {
		if config.InitialBackoff != "" {
			initialBackoff = config.InitialBackoff
		}
		if config.MaxBackoff != "" {
			maxBackoff = config.MaxBackoff
		}
		if config.BackoffMultiplier != nil {
			backoffMultiplier = *config.BackoffMultiplier
		}
	}

	// Parse durations
	initial, err := time.ParseDuration(initialBackoff)
	if err != nil {
		return 0, fmt.Errorf("invalid initial backoff: %w", err)
	}

	max, err := time.ParseDuration(maxBackoff)
	if err != nil {
		return 0, fmt.Errorf("invalid max backoff: %w", err)
	}

	// If no current backoff, start with initial
	if status == nil || status.BackoffDuration == "" {
		return initial, nil
	}

	// Parse current backoff
	current, err := time.ParseDuration(status.BackoffDuration)
	if err != nil {
		return 0, fmt.Errorf("invalid current backoff: %w", err)
	}

	// Calculate next backoff using percentage multiplier
	multiplier := float64(backoffMultiplier) / 100.0
	next := time.Duration(float64(current) * multiplier)
	if next > max {
		next = max
	}

	return next, nil
}

// handleRetry updates the RetryStatus and returns whether to retry and after what duration
func (r *ModeReconciler) handleRetry(ctx context.Context, replication *drv1alpha1.Replication, err error) (bool, time.Duration, error) {
	log := log.FromContext(ctx)

	// Get current retry status
	status := replication.Status.RetryStatus
	if status == nil {
		status = &drv1alpha1.RetryStatus{
			RetriesRemaining: 5, // Default max retries
		}
	}

	// Calculate next backoff
	backoff, err := calculateBackoff(replication.Spec.RetryConfig, status)
	if err != nil {
		return false, 0, fmt.Errorf("failed to calculate backoff: %w", err)
	}

	// Update retry status
	now := metav1.Now()
	nextRetry := metav1.NewTime(now.Add(backoff))
	status.NextRetryTime = &nextRetry
	status.BackoffDuration = backoff.String()
	status.RetriesRemaining--

	// Update replication status
	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		status.RetryStatus = replication.Status.RetryStatus
		status.LastError = &drv1alpha1.SyncError{
			Message: err.Error(),
			Time:    now,
		}
	}); err != nil {
		log.Error(err, "failed to update retry status")
		return false, 0, err
	}

	// Check if we should retry
	if status.RetriesRemaining <= 0 {
		log.V(1).Info("no retries remaining, giving up",
			"error", err)
		return false, 0, nil
	}

	log.V(1).Info("scheduling retry",
		"backoff", backoff.String(),
		"retriesRemaining", status.RetriesRemaining,
		"error", err)
	return true, backoff, nil
}

// resetRetryStatus clears the retry status after a successful sync
func (r *ModeReconciler) resetRetryStatus(ctx context.Context, replication *drv1alpha1.Replication) error {
	return r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		status.RetryStatus = nil
	})
}

// getResourceGVRs converts resource types to GroupVersionResource
func (r *ModeReconciler) getResourceGVRs(resourceTypes []string) []schema.GroupVersionResource {
	// This is a simplified version - in practice, you'd need to map
	// resource types to their proper GVRs using the discovery client
	resources := make([]schema.GroupVersionResource, 0, len(resourceTypes))

	// Normalize resource types
	normalizedTypes := make([]string, len(resourceTypes))
	for i, rt := range resourceTypes {
		normalizedTypes[i] = strings.ToLower(rt)
	}

	// Handle empty or wildcard resource types
	if len(normalizedTypes) == 0 || (len(normalizedTypes) == 1 && normalizedTypes[0] == "*") {
		normalizedTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}
	}

	for _, rtLower := range normalizedTypes {
		switch rtLower {
		case "deployments", "deployment":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			})
		case "services", "service":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "services",
			})
		case "configmaps", "configmap":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			})
		case "secrets", "secret":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			})
		case "ingresses", "ingress":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "networking.k8s.io",
				Version:  "v1",
				Resource: "ingresses",
			})
		case "persistentvolumeclaims", "persistentvolumeclaim", "pvc":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "persistentvolumeclaims",
			})
		}
	}
	return resources
}
