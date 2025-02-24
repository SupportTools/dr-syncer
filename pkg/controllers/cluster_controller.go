package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configCli "github.com/supporttools/dr-syncer/pkg/config"
	"github.com/supporttools/dr-syncer/pkg/controllers/internal/logging"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemoteClusterReconciler reconciles a RemoteCluster object
type RemoteClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
func (r *RemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the RemoteCluster instance
	var cluster drv1alpha1.RemoteCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			// RemoteCluster not found. Ignoring since object must be deleted.
			return ctrl.Result{}, nil
		}
		logging.Logger.WithError(err).Error("unable to fetch RemoteCluster")
		return ctrl.Result{}, err
	}

	// Validate default schedule if provided
	if cluster.Spec.DefaultSchedule != "" {
		if _, err := cron.ParseStandard(cluster.Spec.DefaultSchedule); err != nil {
			logging.Logger.WithError(err).Error(fmt.Sprintf("invalid default schedule: %s", cluster.Spec.DefaultSchedule))
			setRemoteClusterCondition(&cluster, "ScheduleValid", metav1.ConditionFalse, "InvalidSchedule", err.Error())
			// Get latest version before updating status
			var latest drv1alpha1.RemoteCluster
			if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
				logging.Logger.WithError(err).Error("unable to fetch latest RemoteCluster")
				return ctrl.Result{}, err
			}
			latest.Status = cluster.Status
			if err := r.Status().Update(ctx, &latest); err != nil {
				if apierrors.IsConflict(err) {
					logging.Logger.Info("conflict updating status, will retry")
					return ctrl.Result{Requeue: true}, nil
				}
				logging.Logger.WithError(err).Error("unable to update RemoteCluster status")
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
		logging.Logger.WithError(err).Error("unable to fetch kubeconfig secret")
		setRemoteClusterCondition(&cluster, "KubeconfigAvailable", metav1.ConditionFalse, "KubeconfigSecretNotFound", err.Error())
		// Get latest version before updating status
		var latest drv1alpha1.RemoteCluster
		if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
			logging.Logger.WithError(err).Error("unable to fetch latest RemoteCluster")
			return ctrl.Result{}, err
		}
		latest.Status = cluster.Status
		if err := r.Status().Update(ctx, &latest); err != nil {
			if apierrors.IsConflict(err) {
				logging.Logger.Info("conflict updating status, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			logging.Logger.WithError(err).Error("unable to update RemoteCluster status")
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
		logging.Logger.WithError(err).Error("invalid kubeconfig secret")
		setRemoteClusterCondition(&cluster, "KubeconfigAvailable", metav1.ConditionFalse, "KubeconfigKeyNotFound", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Load and parse the kubeconfig
	kubeconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		logging.Logger.WithError(err).Error("unable to load kubeconfig")
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}
	logging.Logger.Info("successfully loaded kubeconfig")

	// Create client config from the loaded kubeconfig with overrides
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterInfo: clientcmdapi.Cluster{
			InsecureSkipTLSVerify: configCli.CFG.IgnoreCert,
		},
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*kubeconfig, configOverrides)
	config, err := clientConfig.ClientConfig()
	if err != nil {
		logging.Logger.WithError(err).Error("unable to create REST config from kubeconfig")
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Always disable request/response body logging
	config.WrapTransport = nil
	if err != nil {
		logging.Logger.WithError(err).Error("unable to create REST config from kubeconfig")
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Ensure TLS config is properly set if not already
	if configCli.CFG.IgnoreCert {
		logging.Logger.Info("Ignoring certificate validation as per configuration")
		config.TLSClientConfig.Insecure = true
		config.TLSClientConfig.CAData = nil
		config.TLSClientConfig.CAFile = ""
	} else if len(config.TLSClientConfig.CAData) == 0 && config.TLSClientConfig.CAFile == "" {
		logging.Logger.Info("No CAData or CAFile found in the REST config. Attempting to set CAData from kubeconfig clusters.")
		for _, cluster := range kubeconfig.Clusters {
			if len(cluster.CertificateAuthorityData) > 0 {
				config.TLSClientConfig.CAData = cluster.CertificateAuthorityData
				break
			}
		}

		if len(config.TLSClientConfig.CAData) == 0 {
			err := fmt.Errorf("CA data not found in kubeconfig clusters")
			logging.Logger.WithError(err).Error("unable to find CA data in kubeconfig for any cluster")
			setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
			_ = r.Status().Update(ctx, &cluster)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	} else {
		logging.Logger.Info("REST config already includes CA information")
	}

	logging.Logger.Info("TLS client config configured")

	// Create a Kubernetes client for the remote cluster
	remoteClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		logging.Logger.WithError(err).Error("unable to create Kubernetes client")
		setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionFalse, "InvalidKubeconfig", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Test connection to the remote cluster
	_, err = remoteClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		logging.Logger.WithError(err).Error("unable to connect to remote cluster - certificate or network issue likely")
		setRemoteClusterCondition(&cluster, "ClusterAvailable", metav1.ConditionFalse, "ConnectionFailed", err.Error())
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Update status conditions to reflect successful connection
	setRemoteClusterCondition(&cluster, "KubeconfigAvailable", metav1.ConditionTrue, "KubeconfigFound", "Kubeconfig secret is available")
	setRemoteClusterCondition(&cluster, "KubeconfigValid", metav1.ConditionTrue, "KubeconfigValidated", "Kubeconfig is valid")
	setRemoteClusterCondition(&cluster, "ClusterAvailable", metav1.ConditionTrue, "ConnectionSuccessful", "Successfully connected to remote cluster")
	cluster.Status.LastSyncTime = &metav1.Time{Time: time.Now()}

	// Get latest version before updating final status
	var latest drv1alpha1.RemoteCluster
	if err := r.Get(ctx, req.NamespacedName, &latest); err != nil {
		logging.Logger.WithError(err).Error("unable to fetch latest RemoteCluster")
		return ctrl.Result{}, err
	}
	latest.Status = cluster.Status
	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			logging.Logger.Info("conflict updating status, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		logging.Logger.WithError(err).Error("unable to update RemoteCluster status")
		return ctrl.Result{}, err
	}
	cluster.Status = latest.Status

	// Requeue after 5 minutes to validate connection and schedule again
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.RemoteCluster{}).
		Complete(r)
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
