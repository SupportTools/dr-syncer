package modes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer"
	"github.com/supporttools/dr-syncer/pkg/controllers/watch"
	"github.com/supporttools/dr-syncer/pkg/logging"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultSchedule is the default cron schedule for replication (every 5 minutes)
	DefaultSchedule = "*/5 * * * *"
)

var globalLog = logging.SetupLogging()

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
	globalLog.Info(fmt.Sprintf("starting scheduled reconciliation from %s to %s", replication.Spec.SourceNamespace, replication.Spec.DestinationNamespace))

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
		globalLog.WithError(err).Error("failed to sync resources")
		shouldRetry, backoff, retryErr := r.handleRetry(ctx, replication, err)
		if retryErr != nil {
			globalLog.WithError(retryErr).Error("failed to handle retry")
			return ctrl.Result{}, retryErr
		}
		if shouldRetry {
			globalLog.Info(fmt.Sprintf("retrying sync after %s backoff", backoff))
			return ctrl.Result{RequeueAfter: backoff}, err // Return error with backoff
		}
		return ctrl.Result{}, err
	}

	// Reset retry status and update final status
	if err := r.resetRetryStatus(ctx, replication); err != nil {
		globalLog.WithError(err).Error("failed to reset retry status")
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
			globalLog.Info(fmt.Sprintf("no schedule specified, using default: %s", DefaultSchedule))
			schedule = DefaultSchedule
		}

		cronSchedule, err := cron.ParseStandard(schedule)
		if err != nil {
			globalLog.WithError(err).Error(fmt.Sprintf("invalid schedule: %s, using default interval of 5m", schedule))
			status.NextSyncTime = &metav1.Time{Time: time.Now().Add(5 * time.Minute)}
		} else {
			// Calculate exact next run time
			now := time.Now()
			nextRun := cronSchedule.Next(now)
			status.NextSyncTime = &metav1.Time{Time: nextRun}
			globalLog.Info(fmt.Sprintf("next sync scheduled for %s", nextRun.Format(time.RFC3339)))
		}
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Use the same next sync time for requeue
	if replication.Status.NextSyncTime == nil {
		globalLog.Info("next sync time not set, using default 5 minute interval")
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	requeueAfter := time.Until(replication.Status.NextSyncTime.Time)
	globalLog.Info(fmt.Sprintf("scheduled reconciliation complete, next sync in %s", requeueAfter))

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// ReconcileContinuous handles continuous replication mode
func (r *ModeReconciler) ReconcileContinuous(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	globalLog.Info(fmt.Sprintf("starting continuous reconciliation from %s to %s", replication.Spec.SourceNamespace, replication.Spec.DestinationNamespace))

	// If not already watching, start watching resources
	if !r.watchManager.IsWatching() {
		resources := r.getResourceGVRs(replication.Spec.ResourceTypes)
		globalLog.Info(fmt.Sprintf("starting resource watchers for %d resource types", len(resources)))

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
					globalLog.WithError(err).Error("failed to sync resources after watch event")
					shouldRetry, backoff, retryErr := r.handleRetry(ctx, replication, err)
					if retryErr != nil {
						globalLog.WithError(retryErr).Error("failed to handle retry")
						return retryErr
					}
					if shouldRetry {
						globalLog.Info(fmt.Sprintf("retrying sync after %s backoff", backoff))
						time.Sleep(backoff) // For continuous mode, we sleep here instead of requeueing
						return nil          // Continue watching
					}
					return err
				}

				// Reset retry status and update success status
				if err := r.resetRetryStatus(ctx, replication); err != nil {
					globalLog.WithError(err).Error("failed to reset retry status")
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

				globalLog.Info(fmt.Sprintf("watch event sync complete in %s", syncDuration))
				return nil
			})
		if err != nil {
			globalLog.WithError(err).Error("failed to start watching resources")
			return ctrl.Result{}, err
		}

		// Start background sync if configured
		if replication.Spec.Continuous != nil && replication.Spec.Continuous.BackgroundSyncInterval != "" {
			interval, err := time.ParseDuration(replication.Spec.Continuous.BackgroundSyncInterval)
			if err != nil {
				globalLog.WithError(err).Error("invalid background sync interval")
				return ctrl.Result{}, err
			}

			globalLog.Info(fmt.Sprintf("starting background sync with interval %s", interval))

			r.watchManager.StartBackgroundSync(ctx, interval, func() error {
				_, err := r.syncResources(ctx, replication)
				return err
			})
		}
	}

	globalLog.Info("continuous reconciliation complete")

	// Requeue to periodically check watch status
	return ctrl.Result{RequeueAfter: time.Hour}, nil
}

// ReconcileManual handles manual replication mode
func (r *ModeReconciler) ReconcileManual(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	globalLog.Info(fmt.Sprintf("starting manual reconciliation from %s to %s", replication.Spec.SourceNamespace, replication.Spec.DestinationNamespace))

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
		globalLog.WithError(err).Error("failed to sync resources")
		shouldRetry, backoff, retryErr := r.handleRetry(ctx, replication, err)
		if retryErr != nil {
			globalLog.WithError(retryErr).Error("failed to handle retry")
			return ctrl.Result{}, retryErr
		}
		if shouldRetry {
			globalLog.Info(fmt.Sprintf("retrying sync after %s backoff", backoff))
			return ctrl.Result{RequeueAfter: backoff}, nil
		}
		return ctrl.Result{}, err
	}

	// Reset retry status and update final status
	if err := r.resetRetryStatus(ctx, replication); err != nil {
		globalLog.WithError(err).Error("failed to reset retry status")
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

	globalLog.Info(fmt.Sprintf("manual reconciliation complete in %s", syncDuration))

	return ctrl.Result{}, nil
}

// syncResources performs the actual resource synchronization
func (r *ModeReconciler) syncResources(ctx context.Context, replication *drv1alpha1.Replication) ([]drv1alpha1.DeploymentScale, error) {
	startTime := time.Now()

	globalLog.Info(fmt.Sprintf("starting resource sync from %s to %s", replication.Spec.SourceNamespace, replication.Spec.DestinationNamespace))

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

	globalLog.Info(fmt.Sprintf("syncing %d resource types with scale to zero: %v", len(normalizedTypes), scaleToZero))

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

	globalLog.Info(fmt.Sprintf("resource sync complete in %s, synced %d deployments", time.Since(startTime), len(result)))

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
	maxRetries := 5                      // Reduced from 10 to avoid excessive retries
	retryDelay := 250 * time.Millisecond // Increased initial delay

	key := client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}

	for i := 0; i < maxRetries; i++ {
		// Get latest version
		var latest drv1alpha1.Replication
		if err := r.Get(ctx, key, &latest); err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}

		globalLog.Info(fmt.Sprintf("updating status (attempt %d/%d)", i+1, maxRetries))

		// Store current status and apply update
		oldStatus := latest.Status.DeepCopy()
		updateFn(&latest.Status)

		// Check if status actually changed
		if statusEqual(oldStatus, &latest.Status) {
			globalLog.Info("status unchanged after update function")
			replication.Status = latest.Status
			return nil
		}

		// Try to update
		err := r.Status().Update(ctx, &latest)
		if err == nil {
			globalLog.Info("status update successful")
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
			globalLog.Info("conflict resolved - desired status already set")
			replication.Status = conflicted.Status
			return nil
		}

		globalLog.Info(fmt.Sprintf("status update conflict detected, retrying in %s", retryDelay))

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

// resetRetryStatus clears the retry status after a successful sync
func (r *ModeReconciler) resetRetryStatus(ctx context.Context, replication *drv1alpha1.Replication) error {
	return r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		status.RetryStatus = nil
	})
}

// getResourceGVRs converts resource types to GroupVersionResource
func (r *ModeReconciler) getResourceGVRs(resourceTypes []string) []schema.GroupVersionResource {
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

// handleRetry updates the RetryStatus and returns whether to retry and after what duration
func (r *ModeReconciler) handleRetry(ctx context.Context, replication *drv1alpha1.Replication, err error) (bool, time.Duration, error) {
	// Initialize retry status
	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		if status.RetryStatus == nil {
			status.RetryStatus = &drv1alpha1.RetryStatus{
				RetriesRemaining: 5, // Default max retries
			}
		}
	}); err != nil {
		globalLog.WithError(err).Error("failed to initialize retry status")
		return false, 0, err
	}

	// Get retry status
	status := replication.Status.RetryStatus
	if status == nil {
		globalLog.Error("retry status is nil after initialization")
		return false, 0, fmt.Errorf("retry status is nil")
	}

	// Calculate next backoff
	backoff, err := calculateBackoff(replication.Spec.RetryConfig, status)
	if err != nil {
		return false, 0, fmt.Errorf("failed to calculate backoff: %w", err)
	}

	// Check if we should retry
	if status.RetriesRemaining <= 0 {
		globalLog.Info("no retries remaining, giving up")
		return false, 0, nil
	}

	// Update retry status
	now := metav1.Now()
	nextRetry := metav1.NewTime(now.Add(backoff))

	// Update replication status
	if err := r.updateStatus(ctx, replication, func(status *drv1alpha1.ReplicationStatus) {
		// Ensure RetryStatus exists
		if status.RetryStatus == nil {
			status.RetryStatus = &drv1alpha1.RetryStatus{
				RetriesRemaining: 5, // Default max retries
			}
		}

		// Update retry information
		status.RetryStatus.NextRetryTime = &nextRetry
		status.RetryStatus.BackoffDuration = backoff.String()
		status.RetryStatus.RetriesRemaining--

		// Update error information
		if err != nil {
			status.LastError = &drv1alpha1.SyncError{
				Message: err.Error(),
				Time:    now,
			}
		}

		// Update phase and conditions
		status.Phase = drv1alpha1.SyncPhaseFailed

		// Add failure condition with safe error message
		message := "Sync failed"
		if err != nil {
			message = fmt.Sprintf("Sync failed: %v", err)
		}
		failureCondition := metav1.Condition{
			Type:               "Failed",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "SyncFailed",
			Message:            fmt.Sprintf("%s, retrying in %s", message, backoff),
		}

		// Update conditions
		if status.Conditions == nil {
			status.Conditions = []metav1.Condition{}
		}

		// Remove old Failed condition if it exists
		conditions := []metav1.Condition{}
		for _, condition := range status.Conditions {
			if condition.Type != "Failed" {
				conditions = append(conditions, condition)
			}
		}
		conditions = append(conditions, failureCondition)
		status.Conditions = conditions
	}); err != nil {
		return false, 0, fmt.Errorf("failed to update status: %w", err)
	}

	globalLog.Info(fmt.Sprintf("scheduled retry in %s (%d retries remaining)", backoff, status.RetriesRemaining))
	return true, backoff, nil
}
