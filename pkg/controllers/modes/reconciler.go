package modes

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer"
	"github.com/supporttools/dr-syncer/pkg/controllers/watch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

	// Start sync - get latest version before updating
	var latest drv1alpha1.Replication
	if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
		log.Error(err, "unable to fetch latest Replication")
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	latest.Status.Phase = drv1alpha1.SyncPhaseRunning
	latest.Status.LastSyncTime = &now

	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			log.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "failed to update initial status")
		return ctrl.Result{}, err
	}
	
	// Update our working copy with the latest status
	replication.Status = latest.Status

	// Sync resources
	startTime := time.Now()
	deploymentScales, err := r.syncResources(ctx, replication)
	syncDuration := time.Since(startTime)

	if err != nil {
		log.Error(err, "failed to sync resources")
		now := metav1.Now()
		replication.Status.Phase = drv1alpha1.SyncPhaseFailed
		replication.Status.LastSyncTime = &now
		replication.Status.LastError = &drv1alpha1.SyncError{
			Message: err.Error(),
			Time:    now,
		}
		// Get latest version before updating error status
		if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
			log.Error(err, "unable to fetch latest Replication")
			return ctrl.Result{}, err
		}

		latest.Status = replication.Status
		if err := r.Status().Update(ctx, &latest); err != nil {
			if apierrors.IsConflict(err) {
				log.Info("conflict updating error status, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "failed to update error status")
		}
		replication.Status = latest.Status
		return ctrl.Result{}, err
	}

	// Update status with success
	now = metav1.Now()
	replication.Status.Phase = drv1alpha1.SyncPhaseCompleted
	replication.Status.LastSyncTime = &now
	replication.Status.DeploymentScales = deploymentScales
	replication.Status.SyncStats = &drv1alpha1.SyncStats{
		TotalResources:   int32(len(deploymentScales)), // This should be updated to include all resource types
		SuccessfulSyncs: int32(len(deploymentScales)),
		FailedSyncs:     0,
		LastSyncDuration: syncDuration.String(),
	}

	// Calculate next sync time
	schedule := replication.Spec.Schedule
	if schedule == "" {
		schedule = "*/5 * * * *" // Default to every 5 minutes
	}

	cronSchedule, err := cron.ParseStandard(schedule)
	if err != nil {
		log.Error(err, "invalid schedule", "schedule", schedule)
		return ctrl.Result{}, err
	}

	nextRun := cronSchedule.Next(time.Now())
	nextRunTime := metav1.NewTime(nextRun)
	replication.Status.NextSyncTime = &nextRunTime

	return ctrl.Result{RequeueAfter: time.Until(nextRun)}, nil
}

// ReconcileContinuous handles continuous replication mode
func (r *ModeReconciler) ReconcileContinuous(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// If not already watching, start watching resources
	if !r.watchManager.IsWatching() {
		resources := r.getResourceGVRs(replication.Spec.ResourceTypes)
		err := r.watchManager.StartWatching(ctx, replication.Spec.SourceNamespace, resources,
			func(obj interface{}) error {
				// Start sync
				startTime := time.Now()
				now := metav1.Now()
				replication.Status.Phase = drv1alpha1.SyncPhaseRunning
				replication.Status.LastSyncTime = &now
				replication.Status.LastWatchEvent = &now
				// Get latest version before updating status
				var latest drv1alpha1.Replication
				if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
					log.Error(err, "unable to fetch latest Replication")
					return err
				}

				latest.Status = replication.Status
				if err := r.Status().Update(ctx, &latest); err != nil {
					if apierrors.IsConflict(err) {
						log.Info("conflict updating status, will retry")
						return fmt.Errorf("status update conflict, will retry")
					}
					log.Error(err, "failed to update initial status")
					return err
				}
				replication.Status = latest.Status

				// Handle resource sync
				deploymentScales, err := r.syncResources(ctx, replication)
				syncDuration := time.Since(startTime)

				if err != nil {
					log.Error(err, "failed to sync resources after watch event")
					now := metav1.Now()
					replication.Status.Phase = drv1alpha1.SyncPhaseFailed
					replication.Status.LastSyncTime = &now
					replication.Status.LastWatchEvent = &now
					replication.Status.LastError = &drv1alpha1.SyncError{
						Message: err.Error(),
						Time:    now,
					}
					if err := r.Status().Update(ctx, replication); err != nil {
						log.Error(err, "failed to update error status")
					}
					return err
				}

				// Update status with success
				now = metav1.Now()
				replication.Status.Phase = drv1alpha1.SyncPhaseCompleted
				replication.Status.LastSyncTime = &now
				replication.Status.LastWatchEvent = &now
				replication.Status.DeploymentScales = deploymentScales
				replication.Status.SyncStats = &drv1alpha1.SyncStats{
					TotalResources:   int32(len(deploymentScales)),
					SuccessfulSyncs: int32(len(deploymentScales)),
					FailedSyncs:     0,
					LastSyncDuration: syncDuration.String(),
				}
				// Get latest version before updating final status
				if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
					log.Error(err, "unable to fetch latest Replication")
					return err
				}

				latest.Status = replication.Status
				if err := r.Status().Update(ctx, &latest); err != nil {
					if apierrors.IsConflict(err) {
						return fmt.Errorf("status update conflict, will retry")
					}
					return err
				}
				replication.Status = latest.Status
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

			r.watchManager.StartBackgroundSync(ctx, interval, func() error {
				_, err := r.syncResources(ctx, replication)
				return err
			})
		}
	}

	// Requeue to periodically check watch status
	return ctrl.Result{RequeueAfter: time.Hour}, nil
}

// ReconcileManual handles manual replication mode
func (r *ModeReconciler) ReconcileManual(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Start sync - get latest version before updating
	var latest drv1alpha1.Replication
	if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
		log.Error(err, "unable to fetch latest Replication")
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	latest.Status.Phase = drv1alpha1.SyncPhaseRunning
	latest.Status.LastSyncTime = &now

	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			log.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "failed to update initial status")
		return ctrl.Result{}, err
	}
	
	// Update our working copy with the latest status
	replication.Status = latest.Status

	// Sync resources
	startTime := time.Now()
	deploymentScales, err := r.syncResources(ctx, replication)
	syncDuration := time.Since(startTime)

	if err != nil {
		log.Error(err, "failed to sync resources")
		now := metav1.Now()
		replication.Status.Phase = drv1alpha1.SyncPhaseFailed
		replication.Status.LastSyncTime = &now
		replication.Status.LastError = &drv1alpha1.SyncError{
			Message: err.Error(),
			Time:    now,
		}
		// Get latest version before updating error status
		if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
			log.Error(err, "unable to fetch latest Replication")
			return ctrl.Result{}, err
		}

		latest.Status = replication.Status
		if err := r.Status().Update(ctx, &latest); err != nil {
			if apierrors.IsConflict(err) {
				log.Info("conflict updating error status, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "failed to update error status")
		}
		replication.Status = latest.Status
		return ctrl.Result{}, err
	}

	// Update status with success
	now = metav1.Now()
	replication.Status.Phase = drv1alpha1.SyncPhaseCompleted
	replication.Status.LastSyncTime = &now
	replication.Status.DeploymentScales = deploymentScales
	replication.Status.SyncStats = &drv1alpha1.SyncStats{
		TotalResources:   int32(len(deploymentScales)),
		SuccessfulSyncs: int32(len(deploymentScales)),
		FailedSyncs:     0,
		LastSyncDuration: syncDuration.String(),
	}

	// Get latest version before updating final status
	if err := r.Get(ctx, client.ObjectKey{Name: replication.Name, Namespace: replication.Namespace}, &latest); err != nil {
		log.Error(err, "unable to fetch latest Replication")
		return ctrl.Result{}, err
	}

	latest.Status = replication.Status
	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			log.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// syncResources performs the actual resource synchronization
func (r *ModeReconciler) syncResources(ctx context.Context, replication *drv1alpha1.Replication) ([]drv1alpha1.DeploymentScale, error) {
	// Determine destination namespace
	dstNamespace := replication.Spec.DestinationNamespace
	if dstNamespace == "" {
		dstNamespace = replication.Spec.SourceNamespace
	}

	// Ensure destination namespace exists
	if err := syncer.EnsureNamespaceExists(ctx, r.k8sDest, dstNamespace, replication.Spec.SourceNamespace); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace exists: %w", err)
	}

	// Determine if deployments should be scaled to zero
	scaleToZero := true
	if replication.Spec.ScaleToZero != nil {
		scaleToZero = *replication.Spec.ScaleToZero
	}

	// Determine resource types
	resourceTypes := replication.Spec.ResourceTypes
	if len(resourceTypes) == 0 {
		resourceTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}
	}

	// Sync resources
	deploymentScales, err := syncer.SyncNamespaceResources(
		ctx,
		r.k8sSource,
		r.k8sDest,
		r.Client,
		replication.Spec.SourceNamespace,
		dstNamespace,
		resourceTypes,
		scaleToZero,
		replication.Spec.NamespaceScopedResources,
		replication.Spec.PVCConfig,
		replication.Spec.ImmutableResourceConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sync namespace resources: %w", err)
	}

	// Convert to API types
	result := make([]drv1alpha1.DeploymentScale, len(deploymentScales))
	for i, scale := range deploymentScales {
		result[i] = drv1alpha1.DeploymentScale{
			Name:             scale.Name,
			OriginalReplicas: scale.Replicas,
			LastSyncedAt:     &scale.SyncTime,
		}
	}

	return result, nil
}

// getResourceGVRs converts resource types to GroupVersionResource
func (r *ModeReconciler) getResourceGVRs(resourceTypes []string) []schema.GroupVersionResource {
	// This is a simplified version - in practice, you'd need to map
	// resource types to their proper GVRs using the discovery client
	resources := make([]schema.GroupVersionResource, 0, len(resourceTypes))
	for _, rt := range resourceTypes {
		switch rt {
		case "deployments":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			})
		case "services":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "services",
			})
		case "configmaps":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			})
		case "secrets":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			})
		case "ingresses":
			resources = append(resources, schema.GroupVersionResource{
				Group:    "networking.k8s.io",
				Version:  "v1",
				Resource: "ingresses",
			})
		}
	}
	return resources
}
