package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configCli "github.com/supporttools/dr-syncer/pkg/config"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controller/remotecluster"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemoteClusterReconciler reconciles a RemoteCluster object
type RemoteClusterReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	pvcSyncManager *remotecluster.PVCSyncManager
}

// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
func (r *RemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the RemoteCluster instance
	var cluster drv1alpha1.RemoteCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			// RemoteCluster not found. Ignoring since object must be deleted.
			return ctrl.Result{}, nil
		}
		log.Errorf("[Reconcile][Get] unable to fetch RemoteCluster %s", cluster.Name)
		return ctrl.Result{}, err
	}

	// Validate default schedule if provided
	if cluster.Spec.DefaultSchedule != "" {
		if _, err := cron.ParseStandard(cluster.Spec.DefaultSchedule); err != nil {
			log.Errorf("[Reconcile][ParseStandard] invalid default schedule for cluster %s: %s", cluster.Name, cluster.Spec.DefaultSchedule)
			setRemoteClusterCondition(&cluster, "ScheduleValid", metav1.ConditionFalse, "InvalidSchedule", err.Error())
			// Get latest version before updating status
			var latest drv1alpha1.RemoteCluster
			if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
				log.Errorf("[Reconcile][Get] unable to fetch latest RemoteCluster %s", cluster.Name)
				return ctrl.Result{}, err
			}
			latest.Status = cluster.Status
			if err := r.Status().Update(ctx, &latest); err != nil {
				if apierrors.IsConflict(err) {
					log.Info("[Reconcile][Update] conflict updating status, will retry")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Errorf("[Reconcile][Update] unable to update RemoteCluster status for cluster %s", cluster.Name)
				return ctrl.Result{}, err
			}
			cluster.Status = latest.Status
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
		setRemoteClusterCondition(&cluster, "ScheduleValid", metav1.ConditionTrue, "ScheduleValidated", "Default schedule is valid")
	}

	// Get the kubeconfig secret
	var kubeconfigSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Spec.KubeconfigSecretRef.Namespace,
		Name:      cluster.Spec.KubeconfigSecretRef.Name,
	}, &kubeconfigSecret); err != nil {
		log.Errorf("[Reconcile][Get] unable to fetch kubeconfig secret for cluster %s", cluster.Name)
		setRemoteClusterCondition(&cluster, "KubeconfigAvailable", metav1.ConditionFalse, "KubeconfigSecretNotFound", err.Error())
		// Get latest version before updating status
		var latest drv1alpha1.RemoteCluster
		if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
			log.Errorf("[Reconcile][Get] unable to fetch latest RemoteCluster %s", cluster.Name)
			return ctrl.Result{}, err
		}
		latest.Status = cluster.Status
		if err := r.Status().Update(ctx, &latest); err != nil {
			if apierrors.IsConflict(err) {
				log.Info("[Reconcile][Update] conflict updating status, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Errorf("[Reconcile][Update] unable to update RemoteCluster status: %v", err)
			return ctrl.Result{}, err
		}
		cluster.Status = latest.Status
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Get the kubeconfig data
	kubeconfigKey := cluster.Spec.KubeconfigSecretRef.Key
	if kubeconfigKey == "" {
		kubeconfigKey = "kubeconfig"
	}
	kubeconfigData, ok := kubeconfigSecret.Data[kubeconfigKey]
	if !ok {
		err := fmt.Errorf("kubeconfig key %s not found in secret", kubeconfigKey)
		log.Errorf("[Reconcile][GetKubeconfig] invalid kubeconfig secret for cluster %s: %v", cluster.Name, err)
		setRemoteClusterCondition(&cluster, "KubeconfigAvailable", metav1.ConditionFalse, "KubeconfigKeyNotFound", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Load and parse the kubeconfig
	kubeconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		log.Errorf("[Reconcile][Load] unable to load kubeconfig for cluster %s: %v", cluster.Name, err)
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}
	// Create client config from the loaded kubeconfig with overrides
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterInfo: clientcmdapi.Cluster{
			InsecureSkipTLSVerify: configCli.CFG.IgnoreCert,
		},
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*kubeconfig, configOverrides)
	config, err := clientConfig.ClientConfig()
	if err != nil {
		log.Errorf("[Reconcile][ClientConfig] unable to create REST config from kubeconfig for cluster %s: %v", cluster.Name, err)
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Always disable request/response body logging
	config.WrapTransport = nil

	// Ensure TLS config is properly set if not already
	if configCli.CFG.IgnoreCert {
		config.TLSClientConfig.Insecure = true
		config.TLSClientConfig.CAData = nil
		config.TLSClientConfig.CAFile = ""
	} else if len(config.TLSClientConfig.CAData) == 0 && config.TLSClientConfig.CAFile == "" {
		for _, cluster := range kubeconfig.Clusters {
			if len(cluster.CertificateAuthorityData) > 0 {
				config.TLSClientConfig.CAData = cluster.CertificateAuthorityData
				break
			}
		}

		if len(config.TLSClientConfig.CAData) == 0 {
			err := fmt.Errorf("CA data not found in kubeconfig clusters")
			log.Errorf("[Reconcile][GetCAData] unable to find CA data in kubeconfig for cluster %s: %v", cluster.Name, err)
			setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
			_ = r.Status().Update(ctx, &cluster)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	}

	// Create a Kubernetes client for the remote cluster
	remoteClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf("[Reconcile][NewForConfig] unable to create Kubernetes client for cluster %s: %v", cluster.Name, err)
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Test connection to the remote cluster
	_, err = remoteClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Errorf("[Reconcile][List] unable to connect to cluster %s - certificate or network issue likely: %v", cluster.Name, err)
		setRemoteClusterCondition(&cluster, "ClusterAvailable", metav1.ConditionFalse, "ConnectionFailed", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Check if cluster state has changed
	oldStatus := cluster.Status.DeepCopy()

	// Update status conditions
	setRemoteClusterCondition(&cluster, "KubeconfigAvailable", metav1.ConditionTrue, "KubeconfigFound", fmt.Sprintf("Kubeconfig secret is available for cluster %s", cluster.Name))
	setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionTrue, "KubeconfigValidated", fmt.Sprintf("Kubeconfig is valid for cluster %s", cluster.Name))
	setRemoteClusterCondition(&cluster, "ClusterAvailable", metav1.ConditionTrue, "ConnectionSuccessful", fmt.Sprintf("Successfully connected to cluster %s", cluster.Name))
	cluster.Status.LastSyncTime = &metav1.Time{Time: time.Now()}

	// Only log if conditions changed
	if !conditionsEqual(oldStatus.Conditions, cluster.Status.Conditions) {
		log.Info(fmt.Sprintf("[Reconcile][Connect] Successfully connected to cluster %s", cluster.Name))
	}

	// Get latest version before updating final status
	var latest drv1alpha1.RemoteCluster
	if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
		log.Errorf("[Reconcile][Get] unable to fetch latest RemoteCluster %s: %v", cluster.Name, err)
		return ctrl.Result{}, err
	}
	latest.Status = cluster.Status

	// Create a controller-runtime client for the remote cluster
	remoteRuntimeClient, err := client.New(config, client.Options{})
	if err != nil {
		log.Errorf("[Reconcile][NewClient] unable to create controller-runtime client for cluster %s: %v", cluster.Name, err)
		setRemoteClusterCondition(&latest, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &latest)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Handle PVC sync reconciliation
	// Always create a new PVCSyncManager with the remote client for this reconciliation
	pvcSyncManager := remotecluster.NewPVCSyncManager(remoteRuntimeClient, r.Client)

	if err := pvcSyncManager.Reconcile(ctx, &latest); err != nil {
		log.Errorf("[Reconcile][PVCSync] failed to reconcile PVC sync for cluster %s: %v", cluster.Name, err)
		setRemoteClusterCondition(&latest, "PVCSyncReady", metav1.ConditionFalse, "ReconciliationFailed", err.Error())
	} else {
		// Check if any nodes are not ready
		if latest.Status.PVCSync != nil && latest.Status.PVCSync.AgentStatus != nil {
			readyNodes := latest.Status.PVCSync.AgentStatus.ReadyNodes
			totalNodes := latest.Status.PVCSync.AgentStatus.TotalNodes

			if readyNodes == 0 && totalNodes > 0 {
				setRemoteClusterCondition(&latest, "PVCSyncReady", metav1.ConditionFalse, "NoReadyAgents",
					"No agent nodes are ready")
			} else if readyNodes < totalNodes {
				setRemoteClusterCondition(&latest, "PVCSyncReady", metav1.ConditionFalse, "PartiallyReady",
					fmt.Sprintf("%d/%d agent nodes are ready", readyNodes, totalNodes))
			} else if readyNodes > 0 {
				setRemoteClusterCondition(&latest, "PVCSyncReady", metav1.ConditionTrue, "ReconciliationSuccessful",
					"PVC sync agent deployed successfully")
			} else {
				// No nodes yet, but reconciliation was successful
				setRemoteClusterCondition(&latest, "PVCSyncReady", metav1.ConditionTrue, "ReconciliationSuccessful",
					"PVC sync agent deployed successfully")
			}
		} else {
			// No status yet, but reconciliation was successful
			setRemoteClusterCondition(&latest, "PVCSyncReady", metav1.ConditionTrue, "ReconciliationSuccessful",
				"PVC sync agent deployed successfully")
		}
	}

	// Update status
	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			log.Info(fmt.Sprintf("[Reconcile][Update] conflict updating status for cluster %s, will retry", cluster.Name))
			return ctrl.Result{Requeue: true}, nil
		}
		log.Errorf("[Reconcile][Update] unable to update RemoteCluster status for cluster %s: %v", cluster.Name, err)
		return ctrl.Result{}, err
	}
	cluster.Status = latest.Status

	// Requeue after the default sync period to validate connection and schedule again
	return ctrl.Result{RequeueAfter: remotecluster.DefaultSyncPeriod}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Set up a controller that watches RemoteCluster resources
	// We also watch for Secret resources referenced by RemoteCluster's KubeconfigSecretRef
	// to trigger reconciliation when the kubeconfig secret changes
	
	// Create a predicate that ignores status-only updates
	statusChangePredicate := predicate.Or(
		// Reconcile on Create events
		predicate.GenerationChangedPredicate{},
		// Also reconcile on delete events
		predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return !e.DeleteStateUnknown
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Only reconcile if the resource generation changed,
				// which indicates spec changes (not just status)
				oldGeneration := e.ObjectOld.GetGeneration()
				newGeneration := e.ObjectNew.GetGeneration()
				return oldGeneration != newGeneration
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		},
	)
	
	return ctrl.NewControllerManagedBy(mgr).
		// Watch RemoteCluster but only trigger reconciliation for spec changes, not status changes
		For(&drv1alpha1.RemoteCluster{}, builder.WithPredicates(statusChangePredicate)).
		// Watch for changes to Secrets that could be referenced by RemoteCluster resources
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findRemoteClustersForSecret),
		).
		Complete(r)
}

// findRemoteClustersForSecret maps from a Secret to the RemoteCluster resources that reference it
func (r *RemoteClusterReconciler) findRemoteClustersForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	// List all RemoteCluster resources
	var remoteClusters drv1alpha1.RemoteClusterList
	err := r.List(ctx, &remoteClusters)
	if err != nil {
		log.Errorf("Failed to list RemoteClusters: %v", err)
		return nil
	}

	// Find all RemoteClusters that reference this secret
	for _, rc := range remoteClusters.Items {
		if rc.Spec.KubeconfigSecretRef.Namespace == secret.Namespace &&
			rc.Spec.KubeconfigSecretRef.Name == secret.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      rc.Name,
					Namespace: rc.Namespace,
				},
			})
		}
	}

	return requests
}

// conditionsEqual compares two slices of conditions for equality
func conditionsEqual(a, b []metav1.Condition) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for easier comparison
	aMap := make(map[string]metav1.Condition)
	for _, condition := range a {
		aMap[condition.Type] = condition
	}

	for _, condition := range b {
		aCondition, exists := aMap[condition.Type]
		if !exists {
			return false
		}
		if aCondition.Status != condition.Status ||
			aCondition.Reason != condition.Reason ||
			aCondition.Message != condition.Message {
			return false
		}
	}

	return true
}

// setRemoteClusterCondition updates or adds the specified condition to the RemoteCluster status
func setRemoteClusterCondition(c *drv1alpha1.RemoteCluster, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i := range c.Status.Conditions {
		if c.Status.Conditions[i].Type == conditionType {
			// Only update if something actually changed
			if c.Status.Conditions[i].Status != status ||
				c.Status.Conditions[i].Reason != reason ||
				c.Status.Conditions[i].Message != message {
				c.Status.Conditions[i].Status = status
				c.Status.Conditions[i].Reason = reason
				c.Status.Conditions[i].Message = message
				c.Status.Conditions[i].LastTransitionTime = now
			}
			return
		}
	}
	c.Status.Conditions = append(c.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}
