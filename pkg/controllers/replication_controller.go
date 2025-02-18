package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/sync"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReplicationReconciler reconciles a Replication object
type ReplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

// Reconcile handles the reconciliation loop for Replication resources
func (r *ReplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Replication instance
	var replication drv1alpha1.Replication
	if err := r.Get(ctx, req.NamespacedName, &replication); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Replication")
		return ctrl.Result{}, err
	}

	// Get source client
	var sourceClient kubernetes.Interface

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

	// Create source cluster client
	sourceConfig, err := clientcmd.RESTConfigFromKubeConfig(sourceKubeconfigData)
	if err != nil {
		log.Error(err, "unable to create source REST config from kubeconfig")
		return ctrl.Result{}, err
	}

	sourceClient, err = kubernetes.NewForConfig(sourceConfig)
	if err != nil {
		log.Error(err, "unable to create source Kubernetes client")
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

	// Create destination cluster client
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

	// Determine destination namespace
	dstNamespace := replication.Spec.DestinationNamespace
	if dstNamespace == "" {
		dstNamespace = replication.Spec.SourceNamespace
	}

	// Ensure destination namespace exists
	if err := sync.EnsureNamespaceExists(ctx, destClient, dstNamespace, replication.Spec.SourceNamespace); err != nil {
		log.Error(err, "failed to ensure namespace exists", "namespace", dstNamespace)
		return ctrl.Result{}, err
	}

	// Determine resource types to sync
	resourceTypes := replication.Spec.ResourceTypes
	if len(resourceTypes) == 0 {
		resourceTypes = destCluster.Spec.DefaultResourceTypes
	}
	if len(resourceTypes) == 0 {
		resourceTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses"}
	}

	// Determine if deployments should be scaled to zero
	scaleToZero := true // default to true
	if replication.Spec.ScaleToZero != nil {
		scaleToZero = *replication.Spec.ScaleToZero
	}

	// Sync resources
	deploymentScales, err := sync.SyncNamespaceResources(ctx, sourceClient, destClient, replication.Spec.SourceNamespace, dstNamespace, resourceTypes, scaleToZero, replication.Spec.NamespaceScopedResources, replication.Spec.PVCConfig)
	if err != nil {
		log.Error(err, "failed to sync namespace resources",
			"sourceNamespace", replication.Spec.SourceNamespace,
			"destinationNamespace", dstNamespace)
		return ctrl.Result{}, err
	}

	// Update status with deployment scales and sync time
	now := metav1.Now()
	replication.Status.LastSyncTime = &now
	replication.Status.DeploymentScales = make([]drv1alpha1.DeploymentScale, len(deploymentScales))
	for i, scale := range deploymentScales {
		syncTime := metav1.NewTime(scale.SyncTime)
		replication.Status.DeploymentScales[i] = drv1alpha1.DeploymentScale{
			Name:             scale.Name,
			OriginalReplicas: scale.Replicas,
			LastSyncedAt:     &syncTime,
		}
	}

	// Calculate next sync time based on schedule
	schedule := replication.Spec.Schedule
	if schedule == "" {
		schedule = destCluster.Spec.DefaultSchedule
	}
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

	if err := r.Status().Update(ctx, &replication); err != nil {
		log.Error(err, "unable to update Replication status")
		return ctrl.Result{}, err
	}

	// Requeue at the next scheduled time
	return ctrl.Result{RequeueAfter: time.Until(nextRun)}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ReplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.Replication{}).
		Complete(r)
}
