package controllers

import (
	"context"
	"fmt"
	"time"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/modes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ReplicationReconciler reconciles a Replication object
type ReplicationReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	modeHandler  *modes.ModeReconciler
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

// Reconcile handles the reconciliation loop for Replication resources
func (r *ReplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reconcileStart := time.Now()
	
	log.V(1).Info("starting reconciliation",
		"name", req.Name,
		"namespace", req.Namespace)

	// Fetch the Replication instance
	var replication drv1alpha1.Replication
	if err := r.Get(ctx, req.NamespacedName, &replication); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Replication")
		return ctrl.Result{}, err
	}

	log.V(1).Info("fetched replication",
		"generation", replication.Generation,
		"resourceVersion", replication.ResourceVersion,
		"mode", replication.Spec.ReplicationMode,
		"sourceCluster", replication.Spec.SourceCluster,
		"destCluster", replication.Spec.DestinationCluster)

	// Initialize clients if not already done
	if r.modeHandler == nil {
		log.V(1).Info("initializing mode handler and clients")

		// Fetch the source Cluster instance
		var sourceCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      replication.Spec.SourceCluster,
			Namespace: replication.ObjectMeta.Namespace,
		}, &sourceCluster); err != nil {
			log.Error(err, "unable to fetch source RemoteCluster")
			return ctrl.Result{}, err
		}

		// Get the source kubeconfig secret
		var sourceKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: sourceCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      sourceCluster.Spec.KubeconfigSecretRef.Name,
		}, &sourceKubeconfigSecret); err != nil {
			log.Error(err, "unable to fetch source kubeconfig secret")
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
			log.Error(err, "invalid source kubeconfig secret")
			return ctrl.Result{}, err
		}

		// Create source cluster clients
		sourceConfig, err := clientcmd.RESTConfigFromKubeConfig(sourceKubeconfigData)
		if err != nil {
			log.Error(err, "unable to create source REST config from kubeconfig")
			return ctrl.Result{}, err
		}

		sourceClient, err := kubernetes.NewForConfig(sourceConfig)
		if err != nil {
			log.Error(err, "unable to create source Kubernetes client")
			return ctrl.Result{}, err
		}

		sourceDynamicClient, err := dynamic.NewForConfig(sourceConfig)
		if err != nil {
			log.Error(err, "unable to create source dynamic client")
			return ctrl.Result{}, err
		}

		// Fetch the destination Cluster instance
		var destCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      replication.Spec.DestinationCluster,
			Namespace: replication.ObjectMeta.Namespace,
		}, &destCluster); err != nil {
			log.Error(err, "unable to fetch destination RemoteCluster")
			return ctrl.Result{}, err
		}

		// Get the destination kubeconfig secret
		var destKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: destCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      destCluster.Spec.KubeconfigSecretRef.Name,
		}, &destKubeconfigSecret); err != nil {
			log.Error(err, "unable to fetch destination kubeconfig secret")
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
			log.Error(err, "invalid destination kubeconfig secret")
			return ctrl.Result{}, err
		}

		// Create destination cluster clients
		destConfig, err := clientcmd.RESTConfigFromKubeConfig(destKubeconfigData)
		if err != nil {
			log.Error(err, "unable to create destination REST config from kubeconfig")
			return ctrl.Result{}, err
		}

		destClient, err := kubernetes.NewForConfig(destConfig)
		if err != nil {
			log.Error(err, "unable to create destination Kubernetes client")
			return ctrl.Result{}, err
		}

		destDynamicClient, err := dynamic.NewForConfig(destConfig)
		if err != nil {
			log.Error(err, "unable to create destination dynamic client")
			return ctrl.Result{}, err
		}

		log.V(1).Info("initialized mode handler and clients successfully",
			"sourceClient", fmt.Sprintf("%T", sourceClient),
			"destClient", fmt.Sprintf("%T", destClient),
			"sourceDynamic", fmt.Sprintf("%T", sourceDynamicClient),
			"destDynamic", fmt.Sprintf("%T", destDynamicClient))
		
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
	log.V(1).Info("starting mode reconciliation",
		"mode", replication.Spec.ReplicationMode,
		"sourceNS", replication.Spec.SourceNamespace,
		"destNS", replication.Spec.DestinationNamespace)
	modeStart := time.Now()
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
		log.Error(err, "failed to reconcile replication",
			"duration", time.Since(modeStart))
		return ctrl.Result{}, err
	}

	log.V(1).Info("completed mode reconciliation",
		"duration", time.Since(modeStart),
		"result", result)

	// Update status using optimistic concurrency control
	if err := r.updateReplicationStatus(ctx, req.NamespacedName, &replication); err != nil {
		if apierrors.IsConflict(err) {
			// For conflicts, requeue with a short delay to prevent tight loops
			log.V(1).Info("status update conflict, requeueing with short delay")
			return ctrl.Result{RequeueAfter: time.Second * 2}, nil
		}
		log.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	log.V(1).Info("reconciliation complete",
		"name", req.Name,
		"namespace", req.Namespace,
		"duration", time.Since(reconcileStart),
		"nextRequeue", result.RequeueAfter)

	return result, nil
}

// updateReplicationStatus updates the status of a Replication resource using optimistic concurrency
func (r *ReplicationReconciler) updateReplicationStatus(ctx context.Context, key client.ObjectKey, replication *drv1alpha1.Replication) error {
	log := log.FromContext(ctx)
	maxRetries := 10
	retryDelay := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		// Get latest version
		var latest drv1alpha1.Replication
		if err := r.Get(ctx, key, &latest); err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}

		// Log status details before update
		log.V(1).Info("current status details",
			"phase", latest.Status.Phase,
			"lastSyncTime", latest.Status.LastSyncTime,
			"resourceVersion", latest.ResourceVersion,
			"deploymentScales", len(latest.Status.DeploymentScales))

		// Log new status details
		log.V(1).Info("new status details",
			"phase", replication.Status.Phase,
			"lastSyncTime", replication.Status.LastSyncTime,
			"deploymentScales", len(replication.Status.DeploymentScales))

		// Check if status has actually changed
		if statusEqual(&latest.Status, &replication.Status) {
			log.V(1).Info("status unchanged, skipping update",
				"resourceVersion", latest.ResourceVersion)
			replication.Status = latest.Status
			return nil
		}

		log.V(1).Info("status changed, attempting update",
			"currentResourceVersion", latest.ResourceVersion,
			"originalResourceVersion", replication.ResourceVersion)

		// Update status fields while preserving others
		latest.Status = replication.Status

		// Try to update
		err := r.Status().Update(ctx, &latest)
		if err == nil {
			// Success - update our working copy
			replication.Status = latest.Status
			return nil
		}

		if !apierrors.IsConflict(err) {
			// Return non-conflict errors immediately
			return err
		}

		// Log conflict details
		log.V(1).Info("conflict updating status, retrying",
			"attempt", i+1,
			"maxRetries", maxRetries,
			"delay", retryDelay,
			"originalResourceVersion", replication.ResourceVersion,
			"latestResourceVersion", latest.ResourceVersion)

		// Wait before retrying
		time.Sleep(retryDelay)
		retryDelay = time.Duration(float64(retryDelay) * 1.5) // Gentler backoff
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

// SetupWithManager sets up the controller with the Manager
func (r *ReplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.Replication{}).
		Complete(r)
}
