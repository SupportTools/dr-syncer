package controllers

import (
	"context"
	"fmt"
	"time"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/contextkeys"
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
	Scheme *runtime.Scheme
	// No longer storing modeHandler as a field since we'll create a new one for each reconciliation
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

// setupClusterClients sets up the clients for a single cluster
func (r *NamespaceMappingReconciler) setupClusterClients(
	ctx context.Context,
	namespace string,
	clusterName string,
	clientType string) (*rest.Config, *kubernetes.Clientset, dynamic.Interface, error) {

	// Fetch the RemoteCluster instance
	var remoteCluster drv1alpha1.RemoteCluster
	if err := r.Get(ctx, client.ObjectKey{
		Name:      clusterName,
		Namespace: namespace,
	}, &remoteCluster); err != nil {
		log.Errorf("unable to fetch %s RemoteCluster %s: %v", clientType, clusterName, err)
		return nil, nil, nil, err
	}

	// Get the kubeconfig secret
	var kubeconfigSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: remoteCluster.Spec.KubeconfigSecretRef.Namespace,
		Name:      remoteCluster.Spec.KubeconfigSecretRef.Name,
	}, &kubeconfigSecret); err != nil {
		log.Errorf("unable to fetch %s kubeconfig secret: %v", clientType, err)
		return nil, nil, nil, err
	}

	// Get the kubeconfig data
	kubeconfigKey := remoteCluster.Spec.KubeconfigSecretRef.Key
	if kubeconfigKey == "" {
		kubeconfigKey = "kubeconfig"
	}

	kubeconfigData, ok := kubeconfigSecret.Data[kubeconfigKey]
	if !ok {
		err := fmt.Errorf("kubeconfig key %s not found in %s secret", kubeconfigKey, clientType)
		log.Errorf("invalid %s kubeconfig secret: %v", clientType, err)
		return nil, nil, nil, err
	}

	// Create cluster clients
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		log.Errorf("unable to create %s REST config from kubeconfig: %v", clientType, err)
		return nil, nil, nil, err
	}
	// Always disable request/response body logging
	config.WrapTransport = nil

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf("unable to create %s Kubernetes client: %v", clientType, err)
		return nil, nil, nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Errorf("unable to create %s dynamic client: %v", clientType, err)
		return nil, nil, nil, err
	}

	// Verify cluster connectivity
	if _, err := client.Discovery().ServerVersion(); err != nil {
		log.Errorf("failed to connect to %s cluster: %v", clientType, err)
		return nil, nil, nil, err
	}

	log.Info(fmt.Sprintf("successfully connected to %s cluster", clientType), "cluster", clusterName)

	return config, client, dynamicClient, nil
}

// setupModeHandlerForNamespaceMapping creates a new ModeReconciler with fresh clients
// for the specific NamespaceMapping and its ClusterMapping
func (r *NamespaceMappingReconciler) setupModeHandlerForNamespaceMapping(
	ctx context.Context,
	namespacemapping *drv1alpha1.NamespaceMapping) (*modes.ModeReconciler, error) {

	log.Info("initializing cluster connections for namespacemapping",
		"name", namespacemapping.Name,
		"namespace", namespacemapping.Namespace)

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
			log.Errorf("unable to fetch ClusterMapping: %v", err)
			return nil, err
		}

		// Use source and target clusters from ClusterMapping
		sourceCluster = clusterMapping.Spec.SourceCluster
		destCluster = clusterMapping.Spec.TargetCluster

		// Store the cluster names in the namespacemapping for logging purposes
		// This doesn't change the original object, just our working copy
		namespacemapping.Spec.SourceCluster = sourceCluster
		namespacemapping.Spec.DestinationCluster = destCluster

		log.Info("using clusters from ClusterMapping",
			"mapping", clusterMapping.Name,
			"sourceCluster", sourceCluster,
			"destCluster", destCluster)
	} else {
		// Use directly specified source and destination clusters
		if namespacemapping.Spec.SourceCluster == "" || namespacemapping.Spec.DestinationCluster == "" {
			err := fmt.Errorf("either ClusterMappingRef or both SourceCluster and DestinationCluster must be specified")
			log.Errorf("invalid NamespaceMapping configuration: %v", err)
			return nil, err
		}

		sourceCluster = namespacemapping.Spec.SourceCluster
		destCluster = namespacemapping.Spec.DestinationCluster

		log.Info("using directly specified clusters",
			"sourceCluster", sourceCluster,
			"destCluster", destCluster)
	}

	// Add cluster names to the context
	ctxWithClusters := context.WithValue(ctx, contextkeys.SourceClusterKey, sourceCluster)
	ctxWithClusters = context.WithValue(ctxWithClusters, contextkeys.DestClusterKey, destCluster)

	// Setup source cluster clients - pass source cluster in context
	sourceCtx := context.WithValue(ctxWithClusters, contextkeys.ClusterTypeKey, "source")
	sourceConfig, sourceClient, sourceDynamicClient, err := r.setupClusterClients(
		sourceCtx, namespacemapping.Namespace, sourceCluster, "source")
	if err != nil {
		return nil, err
	}

	// Setup destination cluster clients - pass destination cluster in context
	destCtx := context.WithValue(ctxWithClusters, contextkeys.ClusterTypeKey, "destination")
	destConfig, destClient, destDynamicClient, err := r.setupClusterClients(
		destCtx, namespacemapping.Namespace, destCluster, "destination")
	if err != nil {
		return nil, err
	}

	// Initialize mode handler with fresh clients and context with cluster names
	return modes.NewModeReconciler(
		r.Client,
		sourceDynamicClient,
		destDynamicClient,
		sourceClient,
		destClient,
		sourceConfig,
		destConfig,
		sourceCluster,
		destCluster,
	), nil
}

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

	// Create a new mode handler for this specific reconciliation
	modeHandler, err := r.setupModeHandlerForNamespaceMapping(ctx, &namespacemapping)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Handle reconciliation based on replication mode
	log.Info(fmt.Sprintf("starting %s mode reconciliation", namespacemapping.Spec.ReplicationMode))

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
		// Create a mode handler specifically for cleanup
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
			
			// Store for logging
			namespacemapping.Spec.DestinationCluster = destCluster
		} else {
			// Use directly specified destination cluster
			if namespacemapping.Spec.DestinationCluster == "" {
				err := fmt.Errorf("either ClusterMappingRef or DestinationCluster must be specified")
				log.Errorf("invalid NamespaceMapping configuration: %v", err)
				return ctrl.Result{}, err
			}

			destCluster = namespacemapping.Spec.DestinationCluster
		}

		log.Info("initializing destination cluster connection for cleanup", 
			"cluster", destCluster)

		// Setup destination cluster only - we don't need source for cleanup
		_, destClient, destDynamicClient, err := r.setupClusterClients(
			ctx, namespacemapping.Namespace, destCluster, "destination")
		if err != nil {
			return ctrl.Result{}, err
		}

		// Create a new mode handler with only destination cluster clients
		cleanupModeHandler := modes.NewModeReconciler(
			r.Client,
			nil,                // No source dynamic client needed for cleanup
			destDynamicClient,
			nil,                // No source client needed for cleanup
			destClient,
			nil,                // No source config needed for cleanup
			nil,                // No dest config needed for CleanupResources
			"",                 // No source cluster name needed for cleanup
			destCluster,        // Pass destination cluster name for logging
		)

		// Clean up synced resources in destination cluster
		if err := cleanupModeHandler.CleanupResources(ctx, namespacemapping); err != nil {
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
