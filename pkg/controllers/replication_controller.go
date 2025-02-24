package controllers

import (
	"context"
	"fmt"
	"time"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/internal/logging"
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
	logging.Logger.Info(fmt.Sprintf("starting reconciliation for %s/%s", req.Namespace, req.Name))

	// Fetch the Replication instance
	var replication drv1alpha1.Replication
	if err := r.Get(ctx, req.NamespacedName, &replication); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logging.Logger.WithError(err).Error("unable to fetch Replication")
		return ctrl.Result{}, err
	}

	// Check if we should proceed with reconciliation based on next sync time
	if replication.Status.NextSyncTime != nil {
		nextSync := replication.Status.NextSyncTime.Time
		now := time.Now()
		if now.Before(nextSync) {
			waitTime := nextSync.Sub(now)
			logging.Logger.Info(fmt.Sprintf("skipping reconciliation, next sync at %s", nextSync.Format(time.RFC3339)))
			return ctrl.Result{RequeueAfter: waitTime}, nil
		}
	}

	logging.Logger.Info("fetched replication")

	// Initialize clients if not already done
	if r.modeHandler == nil {
		// Create REST config with verbosity settings
		restConfig := ctrl.GetConfigOrDie()
		// Always disable request/response body logging
		restConfig.WrapTransport = nil
		logging.Logger.Info("initializing cluster connections")

		// Fetch the source Cluster instance
		var sourceCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      replication.Spec.SourceCluster,
			Namespace: replication.ObjectMeta.Namespace,
		}, &sourceCluster); err != nil {
			logging.Logger.WithError(err).Error("unable to fetch source RemoteCluster")
			return ctrl.Result{}, err
		}

		// Get the source kubeconfig secret
		var sourceKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: sourceCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      sourceCluster.Spec.KubeconfigSecretRef.Name,
		}, &sourceKubeconfigSecret); err != nil {
			logging.Logger.WithError(err).Error("unable to fetch source kubeconfig secret")
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
			logging.Logger.WithError(err).Error("invalid source kubeconfig secret")
			return ctrl.Result{}, err
		}

		// Create source cluster clients
		sourceConfig, err := clientcmd.RESTConfigFromKubeConfig(sourceKubeconfigData)
		if err != nil {
			logging.Logger.WithError(err).Error("unable to create source REST config from kubeconfig")
			return ctrl.Result{}, err
		}
		// Always disable request/response body logging
		sourceConfig.WrapTransport = nil

		sourceClient, err := kubernetes.NewForConfig(sourceConfig)
		if err != nil {
			logging.Logger.WithError(err).Error("unable to create source Kubernetes client")
			return ctrl.Result{}, err
		}

		sourceDynamicClient, err := dynamic.NewForConfig(sourceConfig)
		if err != nil {
			logging.Logger.WithError(err).Error("unable to create source dynamic client")
			return ctrl.Result{}, err
		}

		// Verify source cluster connectivity
		if _, err := sourceClient.Discovery().ServerVersion(); err != nil {
			logging.Logger.WithError(err).Error("failed to connect to source cluster")
			return ctrl.Result{}, err
		}

		logging.Logger.Info("successfully connected to source cluster")

		// Fetch the destination Cluster instance
		var destCluster drv1alpha1.RemoteCluster
		if err := r.Get(ctx, client.ObjectKey{
			Name:      replication.Spec.DestinationCluster,
			Namespace: replication.ObjectMeta.Namespace,
		}, &destCluster); err != nil {
			logging.Logger.WithError(err).Error("unable to fetch destination RemoteCluster")
			return ctrl.Result{}, err
		}

		// Get the destination kubeconfig secret
		var destKubeconfigSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: destCluster.Spec.KubeconfigSecretRef.Namespace,
			Name:      destCluster.Spec.KubeconfigSecretRef.Name,
		}, &destKubeconfigSecret); err != nil {
			logging.Logger.WithError(err).Error("unable to fetch destination kubeconfig secret")
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
			logging.Logger.WithError(err).Error("invalid destination kubeconfig secret")
			return ctrl.Result{}, err
		}

		// Create destination cluster clients
		destConfig, err := clientcmd.RESTConfigFromKubeConfig(destKubeconfigData)
		if err != nil {
			logging.Logger.WithError(err).Error("unable to create destination REST config from kubeconfig")
			return ctrl.Result{}, err
		}
		// Always disable request/response body logging
		destConfig.WrapTransport = nil

		destClient, err := kubernetes.NewForConfig(destConfig)
		if err != nil {
			logging.Logger.WithError(err).Error("unable to create destination Kubernetes client")
			return ctrl.Result{}, err
		}

		destDynamicClient, err := dynamic.NewForConfig(destConfig)
		if err != nil {
			logging.Logger.WithError(err).Error("unable to create destination dynamic client")
			return ctrl.Result{}, err
		}

		// Verify destination cluster connectivity
		if _, err := destClient.Discovery().ServerVersion(); err != nil {
			logging.Logger.WithError(err).Error("failed to connect to destination cluster")
			return ctrl.Result{}, err
		}

		logging.Logger.Info("successfully connected to destination cluster")

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
	logging.Logger.Info(fmt.Sprintf("starting %s mode reconciliation", replication.Spec.ReplicationMode))

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
		logging.Logger.WithError(err).Error("failed to reconcile replication")
		return result, err // Return result along with error to respect backoff
	}

	// Get the latest version of the Replication object before updating status
	var latestReplication drv1alpha1.Replication
	if err := r.Get(ctx, req.NamespacedName, &latestReplication); err != nil {
		logging.Logger.WithError(err).Error("unable to fetch latest Replication")
		return ctrl.Result{}, err
	}

	logging.Logger.Debug("fetched latest replication before status update")

	// Copy the status from our working copy to the latest version
	latestReplication.Status = replication.Status

	// Update status on the latest version
	if err := r.Status().Update(ctx, &latestReplication); err != nil {
		if apierrors.IsConflict(err) {
			// If we hit a conflict, log details and requeue to try again
			logging.Logger.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		logging.Logger.WithError(err).Error("unable to update Replication status")
		return ctrl.Result{}, err
	}

	logging.Logger.Info("reconciliation complete")

	return result, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ReplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.Replication{}).
		Complete(r)
}
