package controllers

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/modes"
	"github.com/supporttools/dr-syncer/pkg/logging"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	// NamespaceMappingFinalizerName is the name of the finalizer added to NamespaceMapping resources
	NamespaceMappingFinalizerName = "dr-syncer.io/cleanup-namespacemapping"
)

// NamespaceMappingReconciler reconciles a NamespaceMapping object
type NamespaceMappingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// No longer storing modeHandler as a field since we'll create a new one for each reconciliation
}

// SetupWithManager sets up the controller with the manager
func (r *NamespaceMappingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logging.LogInfo(nil, "setting up NamespaceMapping controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.NamespaceMapping{}).
		Owns(&drv1alpha1.ClusterMapping{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5, // Adjust based on expected load
		}).
		Complete(r)
}

// Reconcile handles the reconciliation loop for NamespaceMapping resources
func (r *NamespaceMappingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logging.LogInfo(nil, fmt.Sprintf("starting reconciliation for %s/%s", req.Namespace, req.Name))

	// Fetch the NamespaceMapping instance
	var namespacemapping drv1alpha1.NamespaceMapping
	if err := r.Get(ctx, req.NamespacedName, &namespacemapping); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logging.LogError(nil, fmt.Sprintf("unable to fetch NamespaceMapping: %v", err))
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !namespacemapping.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &namespacemapping)
	}

	// Check if the NamespaceMapping is paused
	if namespacemapping.Spec.Paused != nil && *namespacemapping.Spec.Paused {
		logging.LogInfo(nil, fmt.Sprintf("skipping reconciliation for paused NamespaceMapping %s/%s", namespacemapping.Namespace, namespacemapping.Name))
		return ctrl.Result{}, nil
	}

	// Add finalizer if it doesn't exist
	if !containsString(namespacemapping.Finalizers, NamespaceMappingFinalizerName) {
		logging.LogInfo(nil, "adding finalizer")
		namespacemapping.Finalizers = append(namespacemapping.Finalizers, NamespaceMappingFinalizerName)
		if err := r.Update(ctx, &namespacemapping); err != nil {
			logging.LogError(nil, fmt.Sprintf("failed to add finalizer: %v", err))
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Create a new mode handler for this specific reconciliation
	modeHandler, err := r.setupModeHandlerForNamespaceMapping(ctx, &namespacemapping)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Handle reconciliation based on replication mode
	logging.LogInfo(nil, fmt.Sprintf("starting %s mode reconciliation", namespacemapping.Spec.ReplicationMode))

	var result ctrl.Result

	// Use the NamespaceMapping with the newly created mode handler
	switch namespacemapping.Spec.ReplicationMode {
	case drv1alpha1.ContinuousMode:
		result, err = modeHandler.ReconcileContinuous(ctx, &namespacemapping)
	case drv1alpha1.ManualMode:
		result, err = modeHandler.ReconcileManual(ctx, &namespacemapping)
	default: // Scheduled mode is the default
		result, err = modeHandler.ReconcileScheduled(ctx, &namespacemapping)
	}

	if err != nil {
		logging.LogError(nil, fmt.Sprintf("failed to reconcile namespacemapping: %v", err))
		return result, err // Return result along with error to respect backoff
	}

	logging.LogInfo(nil, "reconciliation complete")

	return result, nil
}

// handleDeletion implements cleanup logic for when a NamespaceMapping is deleted
func (r *NamespaceMappingReconciler) handleDeletion(ctx context.Context, namespacemapping *drv1alpha1.NamespaceMapping) (ctrl.Result, error) {
	// Check if the NamespaceMapping has the finalizer
	if !containsString(namespacemapping.Finalizers, NamespaceMappingFinalizerName) {
		return ctrl.Result{}, nil
	}

	logging.LogInfo(nil, fmt.Sprintf("handling deletion for mapping %s", namespacemapping.Name))

	// Determine destination cluster from ClusterMapping or direct specification
	var destCluster string
	if namespacemapping.Spec.ClusterMappingRef != nil {
		// Fetch the ClusterMapping instance
		var clusterMapping drv1alpha1.ClusterMapping
		clusterMappingNamespace := namespacemapping.Spec.ClusterMappingRef.Namespace
		if clusterMappingNamespace == "" {
			clusterMappingNamespace = namespacemapping.Namespace
		}

		if err := r.Get(ctx, client.ObjectKey{
			Name:      namespacemapping.Spec.ClusterMappingRef.Name,
			Namespace: clusterMappingNamespace,
		}, &clusterMapping); err != nil {
			if apierrors.IsNotFound(err) {
				// ClusterMapping not found, can't determine destination cluster,
				// so we can just skip cleanup and remove the finalizer
				logging.LogInfo(nil, "skipping cleanup as ClusterMapping no longer exists")
				namespacemapping.Finalizers = removeString(namespacemapping.Finalizers, NamespaceMappingFinalizerName)
				if err := r.Update(ctx, namespacemapping); err != nil {
					logging.LogError(nil, fmt.Sprintf("failed to remove finalizer: %v", err))
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			logging.LogError(nil, fmt.Sprintf("unable to fetch ClusterMapping: %v", err))
			return ctrl.Result{}, err
		}

		// Get destination cluster from the ClusterMapping
		destCluster = clusterMapping.Spec.TargetCluster
		// Update the NamespaceMapping with the destination cluster
		namespacemapping.Spec.DestinationCluster = destCluster
	} else {
		// Use directly specified destination cluster
		if namespacemapping.Spec.DestinationCluster == "" {
			err := fmt.Errorf("either ClusterMappingRef or DestinationCluster must be specified")
			logging.LogError(nil, fmt.Sprintf("invalid NamespaceMapping configuration: %v", err))
			return ctrl.Result{}, err
		}

		destCluster = namespacemapping.Spec.DestinationCluster
	}

	logging.LogInfo(nil, fmt.Sprintf("initializing destination cluster connection for cleanup: %s", destCluster))

	// Create a new mode handler with only destination cluster clients
	cleanupModeHandler := modes.NewModeReconciler(
		r.Client,
		nil, // No source dynamic client needed for cleanup
		nil, // No destination dynamic client needed for cleanup
		nil, // No source client needed for cleanup
		nil, // No destination client needed for cleanup
		nil, // No source config needed for cleanup
		nil, // No dest config needed for CleanupResources
		"",  // No source cluster name needed for cleanup
		destCluster, // Pass destination cluster name for logging
	)

	// Clean up synced resources in destination cluster
	if err := cleanupModeHandler.CleanupResources(ctx, namespacemapping); err != nil {
		logging.LogError(nil, fmt.Sprintf("failed to cleanup resources: %v", err))
		return ctrl.Result{}, err
	}

	// Remove finalizer
	namespacemapping.Finalizers = removeString(namespacemapping.Finalizers, NamespaceMappingFinalizerName)
	if err := r.Update(ctx, namespacemapping); err != nil {
		logging.LogError(nil, fmt.Sprintf("failed to remove finalizer: %v", err))
		return ctrl.Result{}, err
	}

	logging.LogInfo(nil, "cleanup complete")
	return ctrl.Result{}, nil
}

// setupModeHandlerForNamespaceMapping creates a new ModeReconciler with fresh clients
// for the specific NamespaceMapping and its ClusterMapping
func (r *NamespaceMappingReconciler) setupModeHandlerForNamespaceMapping(
	ctx context.Context,
	namespacemapping *drv1alpha1.NamespaceMapping) (*modes.ModeReconciler, error) {

	logging.LogInfo(nil, "initializing cluster connections for namespacemapping")

	var sourceCluster, destCluster string

	// Get source and destination clusters from ClusterMapping or direct specification
	if namespacemapping.Spec.ClusterMappingRef != nil {
		// Fetch the ClusterMapping instance
		var clusterMapping drv1alpha1.ClusterMapping
		clusterMappingNamespace := namespacemapping.Spec.ClusterMappingRef.Namespace
		if clusterMappingNamespace == "" {
			clusterMappingNamespace = namespacemapping.Namespace
		}

		if err := r.Get(ctx, client.ObjectKey{
			Name:      namespacemapping.Spec.ClusterMappingRef.Name,
			Namespace: clusterMappingNamespace,
		}, &clusterMapping); err != nil {
			logging.LogError(nil, fmt.Sprintf("unable to fetch ClusterMapping: %v", err))
			return nil, err
		}

		// Get source and destination clusters from the ClusterMapping
		sourceCluster = clusterMapping.Spec.SourceCluster
		destCluster = clusterMapping.Spec.TargetCluster

		// Add these to the NamespaceMapping spec for easier access
		namespacemapping.Spec.SourceCluster = sourceCluster
		namespacemapping.Spec.DestinationCluster = destCluster
	} else {
		// Use directly specified source and destination clusters
		if namespacemapping.Spec.SourceCluster == "" || namespacemapping.Spec.DestinationCluster == "" {
			err := fmt.Errorf("either ClusterMappingRef or both SourceCluster and DestinationCluster must be specified")
			logging.LogError(nil, fmt.Sprintf("invalid NamespaceMapping configuration: %v", err))
			return nil, err
		}

		sourceCluster = namespacemapping.Spec.SourceCluster
		destCluster = namespacemapping.Spec.DestinationCluster
	}

	// Create a new mode handler to use in the reconciliation
	return modes.NewModeReconciler(
		r.Client,
		nil, // Source dynamic client
		nil, // Destination dynamic client
		nil, // Source client
		nil, // Destination client
		nil, // Source config
		nil, // Destination config
		sourceCluster,
		destCluster,
	), nil
}

// Helper functions
// containsString checks if a string slice contains a particular string
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// removeString removes a string from a string slice
func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
