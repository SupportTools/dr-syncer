package controllers

import (
	"context"
	"fmt"
	"time"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/modes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReplicationReconciler reconciles a Replication object
type ReplicationReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	modeHandler *modes.ModeReconciler
}



//+kubebuilder:rbac:groups=dr-syncer.io,resources=replications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=dr-syncer.io,resources=replications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=dr-syncer.io,resources=replications/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create
//+kubebuilder:rbac:groups="",resources=configmaps;secrets;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch

const (
	// FinalizerName is the name of the finalizer added to Replication resources
	FinalizerName = "dr-syncer.io/cleanup"
)

// Reconcile handles the reconciliation loop for Replication resources
func (r *ReplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.Info(fmt.Sprintf("starting reconciliation for %s/%s", req.Namespace, req.Name))

	// Fetch the Replication instance
	var replication drv1alpha1.Replication
	if err := r.Get(ctx, req.NamespacedName, &replication); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Errorf("unable to fetch Replication: %v", err)
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !replication.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &replication)
	}

	// Add finalizer if it doesn't exist
	if !containsString(replication.Finalizers, FinalizerName) {
		log.Info("adding finalizer")
		replication.Finalizers = append(replication.Finalizers, FinalizerName)
		if err := r.Update(ctx, &replication); err != nil {
			log.Errorf("failed to add finalizer: %v", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check if we should proceed with reconciliation based on next sync time
	if replication.Status.NextSyncTime != nil {
		nextSync := replication.Status.NextSyncTime.Time
		now := time.Now()
		if now.Before(nextSync) {
			waitTime := nextSync.Sub(now)
			log.Info(fmt.Sprintf("skipping reconciliation, next sync at %s", nextSync.Format(time.RFC3339)))
			return ctrl.Result{RequeueAfter: waitTime}, nil
		}
	}

	log.Info("fetched replication")

	// Initialize clients if not already done
	if r.modeHandler == nil {
		// Create REST config with verbosity settings
		restConfig := ctrl.GetConfigOrDie()
		// Always disable request/response body logging
		restConfig.WrapTransport = nil
		log.Info("initializing cluster connections")

		// Fetch the source Cluster instance
		var sourceCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      replication.Spec.SourceCluster,
			Namespace: replication.ObjectMeta.Namespace,
		}, &sourceCluster); err != nil {
			log.Errorf("unable to fetch source RemoteCluster: %v", err)
			return ctrl.Result{}, err
		}

		// Get the source kubeconfig secret
		var sourceKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: sourceCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      sourceCluster.Spec.KubeconfigSecretRef.Name,
		}, &sourceKubeconfigSecret); err != nil {
			log.Errorf("unable to fetch source kubeconfig secret: %v", err)
			return ctrl.Result{}, err
		}

		// Get the source kubeconfig data
		sourceKubeconfigKey := sourceCluster.Spec.KubeconfigSecretRef.Key
		if sourceKubeconfigKey == "" {
			sourceKubeconfigKey = "kubeconfig"
		}
		sourceKubeconfigData, ok := sourceKubeconfigSecret.Data[sourceKubeconfigKey]
		if !ok {
			err := fmt.Errorf("kubeconfig key %s not found in source secret", sourceKubeconfigKey)
			log.Errorf("invalid source kubeconfig secret: %v", err)
			return ctrl.Result{}, err
		}

		// Create source cluster clients
		sourceConfig, err := clientcmd.RESTConfigFromKubeConfig(sourceKubeconfigData)
		if err != nil {
			log.Errorf("unable to create source REST config from kubeconfig: %v", err)
			return ctrl.Result{}, err
		}
		// Always disable request/response body logging
		sourceConfig.WrapTransport = nil

		sourceClient, err := kubernetes.NewForConfig(sourceConfig)
		if err != nil {
			log.Errorf("unable to create source Kubernetes client: %v", err)
			return ctrl.Result{}, err
		}

		sourceDynamicClient, err := dynamic.NewForConfig(sourceConfig)
		if err != nil {
			log.Errorf("unable to create source dynamic client: %v", err)
			return ctrl.Result{}, err
		}

		// Verify source cluster connectivity
		if _, err := sourceClient.Discovery().ServerVersion(); err != nil {
			log.Errorf("failed to connect to source cluster: %v", err)
			return ctrl.Result{}, err
		}

		log.Info("successfully connected to source cluster")

		// Fetch the destination Cluster instance
		var destCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      replication.Spec.DestinationCluster,
			Namespace: replication.ObjectMeta.Namespace,
		}, &destCluster); err != nil {
			log.Errorf("unable to fetch destination RemoteCluster: %v", err)
			return ctrl.Result{}, err
		}

		// Get the destination kubeconfig secret
		var destKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: destCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      destCluster.Spec.KubeconfigSecretRef.Name,
		}, &destKubeconfigSecret); err != nil {
			log.Errorf("unable to fetch destination kubeconfig secret: %v", err)
			return ctrl.Result{}, err
		}

		// Get the destination kubeconfig data
		destKubeconfigKey := destCluster.Spec.KubeconfigSecretRef.Key
		if destKubeconfigKey == "" {
			destKubeconfigKey = "kubeconfig"
		}
		destKubeconfigData, ok := destKubeconfigSecret.Data[destKubeconfigKey]
		if !ok {
			err := fmt.Errorf("kubeconfig key %s not found in destination secret", destKubeconfigKey)
			log.Errorf("invalid destination kubeconfig secret: %v", err)
			return ctrl.Result{}, err
		}

		// Create destination cluster clients
		destConfig, err := clientcmd.RESTConfigFromKubeConfig(destKubeconfigData)
		if err != nil {
			log.Errorf("unable to create destination REST config from kubeconfig: %v", err)
			return ctrl.Result{}, err
		}
		// Always disable request/response body logging
		destConfig.WrapTransport = nil

		destClient, err := kubernetes.NewForConfig(destConfig)
		if err != nil {
			log.Errorf("unable to create destination Kubernetes client: %v", err)
			return ctrl.Result{}, err
		}

		destDynamicClient, err := dynamic.NewForConfig(destConfig)
		if err != nil {
			log.Errorf("unable to create destination dynamic client: %v", err)
			return ctrl.Result{}, err
		}

		// Verify destination cluster connectivity
		if _, err := destClient.Discovery().ServerVersion(); err != nil {
			log.Errorf("failed to connect to destination cluster: %v", err)
			return ctrl.Result{}, err
		}

		log.Info("successfully connected to destination cluster")

		// Initialize mode handler
		r.modeHandler = modes.NewModeReconciler(
			r.Client,
			sourceDynamicClient,
			destDynamicClient,
			sourceClient,
			destClient,
		)
	}

	// Handle reconciliation based on replication mode
	log.Info(fmt.Sprintf("starting %s mode reconciliation", replication.Spec.ReplicationMode))

	var result ctrl.Result
	var err error

	switch replication.Spec.ReplicationMode {
	case drv1alpha1.ContinuousMode:
		result, err = r.modeHandler.ReconcileContinuous(ctx, &replication)
	case drv1alpha1.ManualMode:
		result, err = r.modeHandler.ReconcileManual(ctx, &replication)
	default: // Scheduled mode is the default
		result, err = r.modeHandler.ReconcileScheduled(ctx, &replication)
	}

	if err != nil {
		log.Errorf("failed to reconcile replication: %v", err)
		return result, err // Return result along with error to respect backoff
	}

	// Get the latest version of the Replication object before updating status
	var latestReplication drv1alpha1.Replication
	if err := r.Get(ctx, req.NamespacedName, &latestReplication); err != nil {
		log.Errorf("unable to fetch latest Replication: %v", err)
		return ctrl.Result{}, err
	}

	log.Info("fetched latest replication before status update")

	// Copy the status from our working copy to the latest version
	latestReplication.Status = replication.Status

	// Update status on the latest version
	if err := r.Status().Update(ctx, &latestReplication); err != nil {
		if apierrors.IsConflict(err) {
			// If we hit a conflict, log details and requeue to try again
			log.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Errorf("unable to update Replication status: %v", err)
		return ctrl.Result{}, err
	}

	log.Info("reconciliation complete")

	return result, nil
}

// handleDeletion handles cleanup when a Replication is being deleted
func (r *ReplicationReconciler) handleDeletion(ctx context.Context, replication *drv1alpha1.Replication) (ctrl.Result, error) {
	log.Info(fmt.Sprintf("handling deletion of replication %s/%s", replication.Namespace, replication.Name))

	// If finalizer is present, we need to clean up resources
	if containsString(replication.Finalizers, FinalizerName) {
		// Initialize clients if not already done
		if r.modeHandler == nil {
			// Create REST config with verbosity settings
			restConfig := ctrl.GetConfigOrDie()
			// Always disable request/response body logging
			restConfig.WrapTransport = nil
			log.Info("initializing cluster connections for cleanup")

			// Fetch the destination Cluster instance
			var destCluster drv1alpha1.RemoteCluster
			if err := r.Get(ctx, client.ObjectKey{
				Name:      replication.Spec.DestinationCluster,
				Namespace: replication.ObjectMeta.Namespace,
			}, &destCluster); err != nil {
				log.Errorf("unable to fetch destination RemoteCluster: %v", err)
				return ctrl.Result{}, err
			}

			// Get the destination kubeconfig secret
			var destKubeconfigSecret corev1.Secret
			if err := r.Get(ctx, client.ObjectKey{
				Namespace: destCluster.Spec.KubeconfigSecretRef.Namespace,
				Name:      destCluster.Spec.KubeconfigSecretRef.Name,
			}, &destKubeconfigSecret); err != nil {
				log.Errorf("unable to fetch destination kubeconfig secret: %v", err)
				return ctrl.Result{}, err
			}

			// Get the destination kubeconfig data
			destKubeconfigKey := destCluster.Spec.KubeconfigSecretRef.Key
			if destKubeconfigKey == "" {
				destKubeconfigKey = "kubeconfig"
			}
			destKubeconfigData, ok := destKubeconfigSecret.Data[destKubeconfigKey]
			if !ok {
				err := fmt.Errorf("kubeconfig key %s not found in destination secret", destKubeconfigKey)
				log.Errorf("invalid destination kubeconfig secret: %v", err)
				return ctrl.Result{}, err
			}

			// Create destination cluster clients
			destConfig, err := clientcmd.RESTConfigFromKubeConfig(destKubeconfigData)
			if err != nil {
				log.Errorf("unable to create destination REST config from kubeconfig: %v", err)
				return ctrl.Result{}, err
			}
			// Always disable request/response body logging
			destConfig.WrapTransport = nil

			destClient, err := kubernetes.NewForConfig(destConfig)
			if err != nil {
				log.Errorf("unable to create destination Kubernetes client: %v", err)
				return ctrl.Result{}, err
			}

			destDynamicClient, err := dynamic.NewForConfig(destConfig)
			if err != nil {
				log.Errorf("unable to create destination dynamic client: %v", err)
				return ctrl.Result{}, err
			}

			// Initialize mode handler with nil source clients since we only need destination for cleanup
			r.modeHandler = modes.NewModeReconciler(
				r.Client,
				nil,
				destDynamicClient,
				nil,
				destClient,
			)
		}

		// Clean up synced resources in destination cluster
		if err := r.modeHandler.CleanupResources(ctx, replication); err != nil {
			log.Errorf("failed to cleanup resources: %v", err)
			return ctrl.Result{}, err
		}

		// Remove finalizer
		replication.Finalizers = removeString(replication.Finalizers, FinalizerName)
		if err := r.Update(ctx, replication); err != nil {
			log.Errorf("failed to remove finalizer: %v", err)
			return ctrl.Result{}, err
		}

		log.Info("cleanup complete")
	}

	return ctrl.Result{}, nil
}

// Helper functions for string slice operations
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

// SetupWithManager sets up the controller with the Manager
func (r *ReplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.Replication{}).
		Complete(r)
}
