package controllers

import (
	"context"
	"fmt"
	"sync"

	drsyncerio "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// log is defined in logger.go

// statusMutex guards access to ClusterMapping status updates
var statusMutex sync.Mutex 

// updateStatusWithRetry updates ClusterMapping status with retry logic for conflicts
func (r *ClusterMappingReconciler) updateStatusWithRetry(ctx context.Context, namespacedName types.NamespacedName, updateFn func(*drsyncerio.ClusterMapping) error) error {
	// Lock for this specific ClusterMapping object
	statusMutex.Lock()
	defer statusMutex.Unlock()

	// Use retry with conflict handling
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch the latest version
		cm := &drsyncerio.ClusterMapping{}
		if err := r.Get(ctx, namespacedName, cm); err != nil {
			return err
		}

		// Apply the update function
		if err := updateFn(cm); err != nil {
			return err
		}

		// Update the resource
		return r.Status().Update(ctx, cm)
	})
}

// setFailedStatusWithRetry sets the ClusterMapping status to Failed with retry logic
func (r *ClusterMappingReconciler) setFailedStatusWithRetry(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping, message string) (ctrl.Result, error) {
	namespacedName := types.NamespacedName{
		Name:      clusterMapping.Name,
		Namespace: clusterMapping.Namespace,
	}

	err := r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
		cm.Status.Phase = drsyncerio.ClusterMappingPhaseFailed
		cm.Status.Message = message
		cm.Status.ConsecutiveFailures++
		return nil
	})

	if err != nil {
		log.Errorf("Failed to update ClusterMapping status: %v", err)
		return ctrl.Result{}, err
	}

	// Calculate backoff duration based on consecutive failures with jitter
	backoff := util.CalculateBackoff(clusterMapping.Status.ConsecutiveFailures)
	log.Info(fmt.Sprintf("Setting backoff for %s: %v (after %d consecutive failures)", 
		namespacedName, backoff, clusterMapping.Status.ConsecutiveFailures))

	return ctrl.Result{RequeueAfter: backoff}, nil
}

// resetFailureCountWithRetry resets the consecutive failures counter when an operation succeeds
func (r *ClusterMappingReconciler) resetFailureCountWithRetry(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) error {
	if clusterMapping.Status.ConsecutiveFailures == 0 {
		return nil // No need to update if already at 0
	}

	namespacedName := types.NamespacedName{
		Name:      clusterMapping.Name,
		Namespace: clusterMapping.Namespace,
	}

	return r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
		cm.Status.ConsecutiveFailures = 0
		return nil
	})
}

// updateLastAttemptTimeWithRetry updates the LastAttemptTime field with the current time
func (r *ClusterMappingReconciler) updateLastAttemptTimeWithRetry(ctx context.Context, namespacedName types.NamespacedName) error {
	return r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
		now := metav1.Now()
		cm.Status.LastAttemptTime = &now
		return nil
	})
}
