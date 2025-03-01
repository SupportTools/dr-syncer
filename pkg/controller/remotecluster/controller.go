package remotecluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// RemoteClusterReconciler reconciles a RemoteCluster object
type RemoteClusterReconciler struct {
	client         client.Client
	scheme         *runtime.Scheme
	pvcSyncManager *PVCSyncManager
}

// NewRemoteClusterReconciler creates a new RemoteCluster reconciler
func NewRemoteClusterReconciler(client client.Client, scheme *runtime.Scheme) *RemoteClusterReconciler {
	return &RemoteClusterReconciler{
		client:         client,
		scheme:         scheme,
		pvcSyncManager: NewPVCSyncManager(client, client),
	}
}

// Reconcile handles RemoteCluster reconciliation
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
func (r *RemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch RemoteCluster instance
	rc := &drv1alpha1.RemoteCluster{}
	if err := r.client.Get(ctx, req.NamespacedName, rc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Initialize status if needed
	if rc.Status.Health == "" {
		rc.Status.Health = "Unknown"
	}

	// Handle PVC sync reconciliation
	if err := r.pvcSyncManager.Reconcile(ctx, rc); err != nil {
		log.Errorf("Failed to reconcile PVC sync: %v", err)
		rc.Status.Health = "Unhealthy"
		if err := r.client.Status().Update(ctx, rc); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status: %v", err)
		}
		return ctrl.Result{}, err
	}

	// Update status
	rc.Status.Health = "Healthy"
	if err := r.client.Status().Update(ctx, rc); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %v", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.RemoteCluster{}).
		Complete(r)
}
