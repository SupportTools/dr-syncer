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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceMappingReconciler reconciles a NamespaceMapping object
type NamespaceMappingReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	modeHandler *modes.ModeReconciler
}

//+kubebuilder:rbac:groups=dr-syncer.io,resources=namespacemappings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=dr-syncer.io,resources=namespacemappings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=dr-syncer.io,resources=namespacemappings/finalizers,verbs=update
//+kubebuilder:rbac:groups=dr-syncer.io,resources=clustermappings,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create
//+kubebuilder:rbac:groups="",resources=configmaps;secrets;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch

const (
	// NamespaceMappingFinalizerName is the name of the finalizer added to NamespaceMapping resources
	NamespaceMappingFinalizerName = "dr-syncer.io/cleanup-namespacemapping"
)

// Reconcile handles the reconciliation loop for NamespaceMapping resources
func (r *NamespaceMappingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.Info(fmt.Sprintf("starting reconciliation for %s/%s", req.Namespace, req.Name))

	// Fetch the NamespaceMapping instance
	var namespacemapping drv1alpha1.NamespaceMapping
	if err := r.Get(ctx, req.NamespacedName, &namespacemapping); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Errorf("unable to fetch NamespaceMapping: %v", err)
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !namespacemapping.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &namespacemapping)
	}

	// Check if the NamespaceMapping is paused
	if namespacemapping.Spec.Paused != nil && *namespacemapping.Spec.Paused {
		log.Info(fmt.Sprintf("skipping reconciliation for paused NamespaceMapping %s/%s", namespacemapping.Namespace, namespacemapping.Name))
		return ctrl.Result{}, nil
	}

	// Add finalizer if it doesn't exist
	if !containsString(namespacemapping.Finalizers, NamespaceMappingFinalizerName) {
		log.Info("adding finalizer")
		namespacemapping.Finalizers = append(namespacemapping.Finalizers, NamespaceMappingFinalizerName)
		if err := r.Update(ctx, &namespacemapping); err != nil {
			log.Errorf("failed to add finalizer: %v", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check if we should proceed with reconciliation based on next sync time
	if namespacemapping.Status.NextSyncTime != nil {
		nextSync := namespacemapping.Status.NextSyncTime.Time
		now := time.Now()
		if now.Before(nextSync) {
			waitTime := nextSync.Sub(now)
			log.Info(fmt.Sprintf("skipping reconciliation, next sync at %s", nextSync.Format(time.RFC3339)))
			return ctrl.Result{RequeueAfter: waitTime}, nil
		}
	}

	log.Info("fetched namespacemapping")

	// Initialize clients if not already done
	if r.modeHandler == nil {
		// Create REST config with verbosity settings
		restConfig := ctrl.GetConfigOrDie()
		// Always disable request/response body logging
		restConfig.WrapTransport = nil
		log.Info("initializing cluster connections")

		var sourceCluster, destCluster string
		var sourceConfig, destConfig *rest.Config
		var sourceClient *kubernetes.Clientset
		var destClient *kubernetes.Clientset
		var sourceDynamicClient, destDynamicClient dynamic.Interface

		// Check if ClusterMapping is specified
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
				log.Errorf("unable to fetch ClusterMapping: %v", err)
				return ctrl.Result{}, err
			}

			// Use source and target clusters from ClusterMapping
			sourceCluster = clusterMapping.Spec.SourceCluster
			destCluster = clusterMapping.Spec.TargetCluster
		} else {
			// Use directly specified source and destination clusters
			if namespacemapping.Spec.SourceCluster == "" || namespacemapping.Spec.DestinationCluster == "" {
				err := fmt.Errorf("either ClusterMappingRef or both SourceCluster and DestinationCluster must be specified")
				log.Errorf("invalid NamespaceMapping configuration: %v", err)
				return ctrl.Result{}, err
			}

			sourceCluster = namespacemapping.Spec.SourceCluster
			destCluster = namespacemapping.Spec.DestinationCluster
		}

		// Fetch the source Cluster instance
		var sourceRemoteCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      sourceCluster,
			Namespace: namespacemapping.ObjectMeta.Namespace,
		}, &sourceRemoteCluster); err != nil {
			log.Errorf("unable to fetch source RemoteCluster: %v", err)
			return ctrl.Result{}, err
		}

		// Get the source kubeconfig secret
		var sourceKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: sourceRemoteCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      sourceRemoteCluster.Spec.KubeconfigSecretRef.Name,
		}, &sourceKubeconfigSecret); err != nil {
			log.Errorf("unable to fetch source kubeconfig secret: %v", err)
			return ctrl.Result{}, err
		}

		// Get the source kubeconfig data
		sourceKubeconfigKey := sourceRemoteCluster.Spec.KubeconfigSecretRef.Key
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
		var err error
		sourceConfig, err = clientcmd.RESTConfigFromKubeConfig(sourceKubeconfigData)
		if err != nil {
			log.Errorf("unable to create source REST config from kubeconfig: %v", err)
			return ctrl.Result{}, err
		}
		// Always disable request/response body logging
		sourceConfig.WrapTransport = nil

		sourceClient, err = kubernetes.NewForConfig(sourceConfig)
		if err != nil {
			log.Errorf("unable to create source Kubernetes client: %v", err)
			return ctrl.Result{}, err
		}

		sourceDynamicClient, err = dynamic.NewForConfig(sourceConfig)
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
		var destRemoteCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      destCluster,
			Namespace: namespacemapping.ObjectMeta.Namespace,
		}, &destRemoteCluster); err != nil {
			log.Errorf("unable to fetch destination RemoteCluster: %v", err)
			return ctrl.Result{}, err
		}

		// Get the destination kubeconfig secret
		var destKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: destRemoteCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      destRemoteCluster.Spec.KubeconfigSecretRef.Name,
		}, &destKubeconfigSecret); err != nil {
			log.Errorf("unable to fetch destination kubeconfig secret: %v", err)
			return ctrl.Result{}, err
		}

		// Get the destination kubeconfig data
		destKubeconfigKey := destRemoteCluster.Spec.KubeconfigSecretRef.Key
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
		destConfig, err = clientcmd.RESTConfigFromKubeConfig(destKubeconfigData)
		if err != nil {
			log.Errorf("unable to create destination REST config from kubeconfig: %v", err)
			return ctrl.Result{}, err
		}
		// Always disable request/response body logging
		destConfig.WrapTransport = nil

		destClient, err = kubernetes.NewForConfig(destConfig)
		if err != nil {
			log.Errorf("unable to create destination Kubernetes client: %v", err)
			return ctrl.Result{}, err
		}

		destDynamicClient, err = dynamic.NewForConfig(destConfig)
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
			sourceConfig,
			destConfig,
		)
	}

	// Handle reconciliation based on replication mode
	log.Info(fmt.Sprintf("starting %s mode reconciliation", namespacemapping.Spec.ReplicationMode))

	var result ctrl.Result
	var err error

	// Use the NamespaceMapping directly with the mode handler

	switch namespacemapping.Spec.ReplicationMode {
	case drv1alpha1.ContinuousMode:
		result, err = r.modeHandler.ReconcileContinuous(ctx, &namespacemapping)
	case drv1alpha1.ManualMode:
		result, err = r.modeHandler.ReconcileManual(ctx, &namespacemapping)
	default: // Scheduled mode is the default
		result, err = r.modeHandler.ReconcileScheduled(ctx, &namespacemapping)
	}

	if err != nil {
		log.Errorf("failed to reconcile namespacemapping: %v", err)
		return result, err // Return result along with error to respect backoff
	}

	// Get the latest version of the NamespaceMapping object before updating status
	var latestNamespaceMapping drv1alpha1.NamespaceMapping
	if err := r.Get(ctx, req.NamespacedName, &latestNamespaceMapping); err != nil {
		log.Errorf("unable to fetch latest NamespaceMapping: %v", err)
		return ctrl.Result{}, err
	}

	log.Info("fetched latest namespacemapping before status update")

	// Copy the status from our working copy to the latest version
	latestNamespaceMapping.Status = namespacemapping.Status

	// Update status on the latest version
	if err := r.Status().Update(ctx, &latestNamespaceMapping); err != nil {
		if apierrors.IsConflict(err) {
			// If we hit a conflict, log details and requeue to try again
			log.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Errorf("unable to update NamespaceMapping status: %v", err)
		return ctrl.Result{}, err
	}

	log.Info("reconciliation complete")

	return result, nil
}

// handleDeletion handles cleanup when a NamespaceMapping is being deleted
func (r *NamespaceMappingReconciler) handleDeletion(ctx context.Context, namespacemapping *drv1alpha1.NamespaceMapping) (ctrl.Result, error) {
	log.Info(fmt.Sprintf("handling deletion of namespacemapping %s/%s", namespacemapping.Namespace, namespacemapping.Name))

	// If finalizer is present, we need to clean up resources
	if containsString(namespacemapping.Finalizers, NamespaceMappingFinalizerName) {
		// Initialize clients if not already done
		if r.modeHandler == nil {
			// Create REST config with verbosity settings
			restConfig := ctrl.GetConfigOrDie()
			// Always disable request/response body logging
			restConfig.WrapTransport = nil
			log.Info("initializing cluster connections for cleanup")

			var destCluster string

			// Check if ClusterMapping is specified
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
					log.Errorf("unable to fetch ClusterMapping: %v", err)
					return ctrl.Result{}, err
				}

				// Use target cluster from ClusterMapping
				destCluster = clusterMapping.Spec.TargetCluster
			} else {
				// Use directly specified destination cluster
				if namespacemapping.Spec.DestinationCluster == "" {
					err := fmt.Errorf("either ClusterMappingRef or DestinationCluster must be specified")
					log.Errorf("invalid NamespaceMapping configuration: %v", err)
					return ctrl.Result{}, err
				}

				destCluster = namespacemapping.Spec.DestinationCluster
			}

			// Fetch the destination Cluster instance
			var destRemoteCluster drv1alpha1.RemoteCluster
			if err := r.Get(ctx, client.ObjectKey{
				Name:      destCluster,
				Namespace: namespacemapping.ObjectMeta.Namespace,
			}, &destRemoteCluster); err != nil {
				log.Errorf("unable to fetch destination RemoteCluster: %v", err)
				return ctrl.Result{}, err
			}

			// Get the destination kubeconfig secret
			var destKubeconfigSecret corev1.Secret
			if err := r.Get(ctx, client.ObjectKey{
				Namespace: destRemoteCluster.Spec.KubeconfigSecretRef.Namespace,
				Name:      destRemoteCluster.Spec.KubeconfigSecretRef.Name,
			}, &destKubeconfigSecret); err != nil {
				log.Errorf("unable to fetch destination kubeconfig secret: %v", err)
				return ctrl.Result{}, err
			}

			// Get the destination kubeconfig data
			destKubeconfigKey := destRemoteCluster.Spec.KubeconfigSecretRef.Key
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
				nil,
				destConfig,
			)
		}

		// Use the NamespaceMapping directly with the mode handler

		// Clean up synced resources in destination cluster
		if err := r.modeHandler.CleanupResources(ctx, namespacemapping); err != nil {
			log.Errorf("failed to cleanup resources: %v", err)
			return ctrl.Result{}, err
		}

		// Remove finalizer
		namespacemapping.Finalizers = removeString(namespacemapping.Finalizers, NamespaceMappingFinalizerName)
		if err := r.Update(ctx, namespacemapping); err != nil {
			log.Errorf("failed to remove finalizer: %v", err)
			return ctrl.Result{}, err
		}

		log.Info("cleanup complete")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *NamespaceMappingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.NamespaceMapping{}).
		Complete(r)
}

// Helper functions for string slice operations

// containsString checks if a string is in a slice
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// removeString removes a string from a slice
func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
