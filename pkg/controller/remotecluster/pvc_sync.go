package remotecluster

import (
	"context"
	"fmt"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/deploy"
	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
	"github.com/supporttools/dr-syncer/pkg/controller/remotecluster/temp"
)

const (
	// DefaultSyncPeriod is the default period between agent deployments
	DefaultSyncPeriod = 60 * time.Minute
	// MinSyncPeriod is the minimum allowed sync period
	MinSyncPeriod = 5 * time.Minute
)

// PVCSyncManager handles PVC sync operations
type PVCSyncManager struct {
	remoteClient     client.Client
	controllerClient client.Client
	keyManager       *ssh.KeyManager
	deployer         *deploy.Deployer
}

// NewPVCSyncManager creates a new PVC sync manager
func NewPVCSyncManager(remoteClient client.Client, controllerClient client.Client) *PVCSyncManager {
	return &PVCSyncManager{
		remoteClient:     remoteClient,
		controllerClient: controllerClient,
		keyManager:       ssh.NewKeyManager(controllerClient),
		deployer:         deploy.NewDeployer(remoteClient),
	}
}

// ForceSync forces agent deployment regardless of last deployment time
func (p *PVCSyncManager) ForceSync(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Check if PVC sync is enabled
	if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
		// If PVC sync was previously enabled, clean up
		if rc.Status.PVCSync != nil && rc.Status.PVCSync.Phase != "" {
			log.Infof("PVC sync disabled for cluster %s, cleaning up", rc.Name)
			return p.cleanupPVCSync(ctx, rc)
		}
		// PVC sync was never enabled, nothing to do
		log.Infof("PVC sync not enabled for cluster %s, skipping", rc.Name)
		return nil
	}

	// Initialize status if needed
	if rc.Status.PVCSync == nil {
		rc.Status.PVCSync = &drv1alpha1.PVCSyncStatus{
			Phase: "Initializing",
		}
	}

	// Get remote client for the remote cluster
	remoteClientset, err := p.getRemoteClient(ctx, rc)
	if err != nil {
		log.Errorf("Failed to get remote client: %v", err)
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to get remote client: %v", err)
		return err
	}

	// Ensure the dr-syncer namespace exists in the remote cluster
	if err := p.ensureDrSyncerNamespace(ctx, remoteClientset); err != nil {
		log.Errorf("Failed to ensure dr-syncer namespace exists: %v", err)
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to ensure dr-syncer namespace: %v", err)
		return err
	}

	// Ensure SSH keys exist
	secret, err := p.keyManager.EnsureKeys(ctx, rc)
	if err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to ensure SSH keys: %v", err)
		return err
	}

	// Push SSH keys to remote cluster
	if err := p.keyManager.PushKeysToRemoteCluster(ctx, rc, p.remoteClient, secret); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to push SSH keys to remote cluster: %v", err)
		return err
	}

	// Store public keys in controller cluster
	if err := p.keyManager.EnsureKeysInControllerCluster(ctx, rc, secret); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to store public SSH keys in controller cluster: %v", err)
		return err
	}

	// Wait for the SSH key secrets to be fully available in both clusters
	if err := p.waitForKeySecret(ctx, rc); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to wait for SSH key secret: %v", err)
		return err
	}

	// Deploy agent components
	log.Infof("Force deploying agent for cluster %s", rc.Name)
	if err := p.deployer.Deploy(ctx, rc); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to deploy agent: %v", err)
		return err
	}

	// Note: SSH key watcher has been removed in favor of per-replication keypairs

	// Initialize agent status if needed
	if rc.Status.PVCSync.AgentStatus == nil {
		rc.Status.PVCSync.AgentStatus = &drv1alpha1.PVCSyncAgentStatus{
			NodeStatuses: make(map[string]drv1alpha1.PVCSyncNodeStatus),
		}
	}

	// Update agent status from DaemonSet
	ds := &appsv1.DaemonSet{}
	if err := p.remoteClient.Get(ctx, client.ObjectKey{Name: "dr-syncer-agent", Namespace: "dr-syncer"}, ds); err == nil {
		rc.Status.PVCSync.AgentStatus.TotalNodes = ds.Status.DesiredNumberScheduled
		rc.Status.PVCSync.AgentStatus.ReadyNodes = ds.Status.NumberReady

		// Update overall status based on DaemonSet status
		if ds.Status.NumberReady > 0 {
			rc.Status.PVCSync.Phase = "Running"
			rc.Status.PVCSync.Message = "PVC sync agent is running"
		} else if ds.Status.DesiredNumberScheduled > 0 {
			rc.Status.PVCSync.Phase = "Degraded"
			rc.Status.PVCSync.Message = fmt.Sprintf("PVC sync agent is degraded: 0/%d pods ready", ds.Status.DesiredNumberScheduled)
		} else {
			rc.Status.PVCSync.Phase = "Running"
			rc.Status.PVCSync.Message = "PVC sync agent deployed successfully"
		}
	} else {
		// If we can't get the DaemonSet, assume it's still starting up
		rc.Status.PVCSync.Phase = "Running"
		rc.Status.PVCSync.Message = "PVC sync agent deployed successfully"
	}

	// Update deployment time
	rc.Status.PVCSync.LastDeploymentTime = &metav1.Time{Time: time.Now()}

	return nil
}

// Reconcile handles PVC sync reconciliation
func (p *PVCSyncManager) Reconcile(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Check if PVC sync is enabled
	if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
		// If PVC sync was previously enabled, clean up
		if rc.Status.PVCSync != nil && rc.Status.PVCSync.Phase != "" {
			log.Infof("PVC sync disabled for cluster %s, cleaning up", rc.Name)
			return p.cleanupPVCSync(ctx, rc)
		}
		// PVC sync was never enabled, nothing to do
		log.Infof("PVC sync not enabled for cluster %s, skipping", rc.Name)
		return nil
	}

	// Initialize status if needed
	if rc.Status.PVCSync == nil {
		rc.Status.PVCSync = &drv1alpha1.PVCSyncStatus{
			Phase: "Initializing",
		}
	}

	// Get remote client for the remote cluster
	remoteClientset, err := p.getRemoteClient(ctx, rc)
	if err != nil {
		log.Errorf("Failed to get remote client: %v", err)
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to get remote client: %v", err)
		return err
	}

	// Ensure the dr-syncer namespace exists in the remote cluster
	if err := p.ensureDrSyncerNamespace(ctx, remoteClientset); err != nil {
		log.Errorf("Failed to ensure dr-syncer namespace exists: %v", err)
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to ensure dr-syncer namespace: %v", err)
		return err
	}

	// Check if agents are already deployed and running
	agentsRunning := false
	if rc.Status.PVCSync != nil && rc.Status.PVCSync.AgentStatus != nil &&
		rc.Status.PVCSync.AgentStatus.ReadyNodes > 0 && rc.Status.PVCSync.Phase == "Running" {
		agentsRunning = true
	}

	// Check if enough time has passed since the last deployment
	if agentsRunning && rc.Status.PVCSync != nil && rc.Status.PVCSync.LastDeploymentTime != nil {
		timeSinceLastDeployment := time.Since(rc.Status.PVCSync.LastDeploymentTime.Time)
		if timeSinceLastDeployment < DefaultSyncPeriod {
			// Not enough time has passed and agents are already running, skip deployment
			log.Infof("Skipping agent deployment for cluster %s - last deployment was %v ago, waiting for %v",
				rc.Name, timeSinceLastDeployment.Round(time.Second), DefaultSyncPeriod)
			return nil
		}
	}

	// If no agents are running or enough time has passed, proceed with deployment
	if !agentsRunning {
		log.Infof("No agents running for cluster %s, forcing deployment", rc.Name)
	} else {
		log.Infof("Periodic sync for cluster %s, last deployment was %v ago",
			rc.Name, time.Since(rc.Status.PVCSync.LastDeploymentTime.Time).Round(time.Second))
	}

	// Ensure SSH keys exist
	secret, err := p.keyManager.EnsureKeys(ctx, rc)
	if err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to ensure SSH keys: %v", err)
		return err
	}

	// Push SSH keys to remote cluster
	if err := p.keyManager.PushKeysToRemoteCluster(ctx, rc, p.remoteClient, secret); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to push SSH keys to remote cluster: %v", err)
		return err
	}

	// Store public keys in controller cluster
	if err := p.keyManager.EnsureKeysInControllerCluster(ctx, rc, secret); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to store public SSH keys in controller cluster: %v", err)
		return err
	}

	// Wait for the SSH key secrets to be fully available in both clusters
	if err := p.waitForKeySecret(ctx, rc); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to wait for SSH key secret: %v", err)
		return err
	}

	// Deploy agent components
	log.Infof("Deploying agent for cluster %s", rc.Name)
	if err := p.deployer.Deploy(ctx, rc); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to deploy agent: %v", err)
		return err
	}

	// Note: SSH key watcher has been removed in favor of per-replication keypairs

	// Initialize agent status if needed
	if rc.Status.PVCSync.AgentStatus == nil {
		rc.Status.PVCSync.AgentStatus = &drv1alpha1.PVCSyncAgentStatus{
			NodeStatuses: make(map[string]drv1alpha1.PVCSyncNodeStatus),
		}
	}

	// Update agent status from DaemonSet
	ds := &appsv1.DaemonSet{}
	if err := p.remoteClient.Get(ctx, client.ObjectKey{Name: "dr-syncer-agent", Namespace: "dr-syncer"}, ds); err == nil {
		rc.Status.PVCSync.AgentStatus.TotalNodes = ds.Status.DesiredNumberScheduled
		rc.Status.PVCSync.AgentStatus.ReadyNodes = ds.Status.NumberReady

		// Update overall status based on DaemonSet status
		if ds.Status.NumberReady > 0 {
			rc.Status.PVCSync.Phase = "Running"
			rc.Status.PVCSync.Message = "PVC sync agent is running"
		} else if ds.Status.DesiredNumberScheduled > 0 {
			rc.Status.PVCSync.Phase = "Degraded"
			rc.Status.PVCSync.Message = fmt.Sprintf("PVC sync agent is degraded: 0/%d pods ready", ds.Status.DesiredNumberScheduled)
		} else {
			rc.Status.PVCSync.Phase = "Running"
			rc.Status.PVCSync.Message = "PVC sync agent deployed successfully"
		}
	} else {
		// If we can't get the DaemonSet, assume it's still starting up
		rc.Status.PVCSync.Phase = "Running"
		rc.Status.PVCSync.Message = "PVC sync agent deployed successfully"
	}

	// Update deployment time
	rc.Status.PVCSync.LastDeploymentTime = &metav1.Time{Time: time.Now()}

	return nil
}

// cleanupPVCSync removes PVC sync components
func (p *PVCSyncManager) cleanupPVCSync(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Delete SSH keys
	if err := p.keyManager.DeleteKeys(ctx, rc); err != nil {
		return fmt.Errorf("failed to delete SSH keys: %v", err)
	}

	// Clean up agent components
	if err := p.deployer.Cleanup(ctx); err != nil {
		return fmt.Errorf("failed to clean up agent components: %v", err)
	}

	// Clear status
	rc.Status.PVCSync = nil

	return nil
}

// RotateSSHKeys rotates SSH keys for the PVC sync agent
func (p *PVCSyncManager) RotateSSHKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
		return nil
	}

	// Rotate SSH keys
	secret, err := p.keyManager.RotateKeys(ctx, rc)
	if err != nil {
		return fmt.Errorf("failed to rotate SSH keys: %v", err)
	}

	// Push rotated keys to remote cluster
	if err := p.keyManager.PushKeysToRemoteCluster(ctx, rc, p.remoteClient, secret); err != nil {
		return fmt.Errorf("failed to push rotated SSH keys to remote cluster: %v", err)
	}

	// Store public keys in controller cluster
	if err := p.keyManager.EnsureKeysInControllerCluster(ctx, rc, secret); err != nil {
		return fmt.Errorf("failed to store public SSH keys in controller cluster: %v", err)
	}

	// Redeploy the agent to pick up new keys
	// The DaemonSet pod template includes the ResourceVersion as an annotation,
	// which will trigger a rolling update when the RemoteCluster is updated
	if err := p.deployer.Deploy(ctx, rc); err != nil {
		return fmt.Errorf("failed to redeploy agent after key rotation: %v", err)
	}

	// Update status
	if rc.Status.PVCSync != nil {
		rc.Status.PVCSync.Message = "SSH keys rotated, agent pods restarting"
		rc.Status.PVCSync.LastDeploymentTime = &metav1.Time{Time: time.Now()}
	}

	return nil
}

// waitForKeySecret waits for the SSH key secret to be fully available
func (p *PVCSyncManager) waitForKeySecret(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil {
		return fmt.Errorf("PVCSync SSH configuration not found")
	}

	// Use a default secret name if keySecretRef is not specified
	secretName := "pvc-syncer-agent-keys"
	if rc.Spec.PVCSync.SSH.KeySecretRef != nil {
		secretName = rc.Spec.PVCSync.SSH.KeySecretRef.Name
	}
	
	// Use the dr-syncer namespace for the secret
	secretNamespace := "dr-syncer"

	// Get kubeconfig secret name for logging
	kubeconfigSecretName := rc.Spec.KubeconfigSecretRef.Name
	kubeconfigSecretNamespace := "dr-syncer" // We always use dr-syncer namespace for kubeconfig secrets

	log.Infof("Waiting for SSH key secret %s/%s to be fully available in REMOTE cluster %s (using kubeconfig from secret %s/%s)",
		secretNamespace, secretName, rc.Name, kubeconfigSecretNamespace, kubeconfigSecretName)

	// Define backoff parameters
	backoff := wait.Backoff{
		Duration: 500 * time.Millisecond, // Initial duration
		Factor:   1.5,                    // Factor to increase duration each retry
		Jitter:   0.1,                    // Jitter factor
		Steps:    10,                     // Maximum number of retries
		Cap:      30 * time.Second,       // Maximum duration between retries
	}

	// Use exponential backoff to wait for the secret to be available
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		log.Infof("Checking for SSH key secret %s/%s in REMOTE cluster %s (attempt)", 
			secretNamespace, secretName, rc.Name)
		
		// Try to get the secret from the remote cluster
		secret := &corev1.Secret{}
		err := p.remoteClient.Get(ctx, client.ObjectKey{
			Name:      secretName,
			Namespace: secretNamespace,
		}, secret)

		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Infof("SSH key secret %s/%s not found in REMOTE cluster %s yet, retrying...", 
					secretNamespace, secretName, rc.Name)
				return false, nil // Not found, retry
			}
			log.Errorf("Error getting SSH key secret from REMOTE cluster %s: %v", rc.Name, err)
			return false, err // Other error, stop retrying
		}

		// Log the keys found in the secret
		keys := []string{}
		for k := range secret.Data {
			keys = append(keys, k)
		}
		log.Infof("Found SSH key secret %s/%s in REMOTE cluster %s with keys: %v", 
			secretNamespace, secretName, rc.Name, keys)

		// Check if the secret has the required keys
		// Look for either "ssh-private-key" or "id_rsa" for the private key
		privateKeyFound := false
		if _, ok := secret.Data["ssh-private-key"]; ok {
			privateKeyFound = true
		} else if _, ok := secret.Data["id_rsa"]; ok {
			privateKeyFound = true
		}
		
		if !privateKeyFound {
			log.Infof("SSH key secret %s/%s in REMOTE cluster %s missing private key (checked for 'ssh-private-key' and 'id_rsa'), retrying...", 
				secretNamespace, secretName, rc.Name)
			return false, nil // Missing private key, retry
		}

		// Look for either "ssh-public-key" or "id_rsa.pub" for the public key
		publicKeyFound := false
		if _, ok := secret.Data["ssh-public-key"]; ok {
			publicKeyFound = true
		} else if _, ok := secret.Data["id_rsa.pub"]; ok {
			publicKeyFound = true
		}
		
		if !publicKeyFound {
			log.Infof("SSH key secret %s/%s in REMOTE cluster %s missing public key (checked for 'ssh-public-key' and 'id_rsa.pub'), retrying...", 
				secretNamespace, secretName, rc.Name)
			return false, nil // Missing public key, retry
		}

		log.Infof("SSH key secret %s/%s is fully available in REMOTE cluster %s", 
			secretNamespace, secretName, rc.Name)
		
		// Now check for the controller cluster secret with the standardized naming convention
		controllerSecretName := "dr-syncer-sshkey-" + rc.Name
		controllerSecretNamespace := "dr-syncer"
		
		log.Infof("Checking for SSH key secret %s/%s in CONTROLLER cluster", 
			controllerSecretNamespace, controllerSecretName)
		
		controllerSecret := &corev1.Secret{}
		err = p.controllerClient.Get(ctx, client.ObjectKey{
			Name:      controllerSecretName,
			Namespace: controllerSecretNamespace,
		}, controllerSecret)
		
		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Infof("SSH key secret %s/%s not found in CONTROLLER cluster yet, retrying...", 
					controllerSecretNamespace, controllerSecretName)
				return false, nil // Not found, retry
			}
			log.Errorf("Error getting SSH key secret from CONTROLLER cluster: %v", err)
			return false, err // Other error, stop retrying
		}
		
		// Log the keys found in the controller secret
		controllerKeys := []string{}
		for k := range controllerSecret.Data {
			controllerKeys = append(controllerKeys, k)
		}
		log.Infof("Found SSH key secret %s/%s in CONTROLLER cluster with keys: %v", 
			controllerSecretNamespace, controllerSecretName, controllerKeys)
		
		// Check if the controller secret has the required public key
		controllerPublicKeyFound := false
		if _, ok := controllerSecret.Data["ssh-public-key"]; ok {
			controllerPublicKeyFound = true
		} else if _, ok := controllerSecret.Data["id_rsa.pub"]; ok {
			controllerPublicKeyFound = true
		}
		
		if !controllerPublicKeyFound {
			log.Infof("SSH key secret %s/%s in CONTROLLER cluster missing public key (checked for 'ssh-public-key' and 'id_rsa.pub'), retrying...", 
				controllerSecretNamespace, controllerSecretName)
			return false, nil // Missing public key, retry
		}
		
		log.Infof("SSH key secrets are fully available in both REMOTE and CONTROLLER clusters")
		return true, nil // Both secrets are available with required keys
	})

	if err != nil {
		return fmt.Errorf("timed out waiting for SSH key secrets: %v", err)
	}

	return nil
}

// ensureDrSyncerNamespace creates the dr-syncer namespace in the remote cluster if it doesn't exist
func (p *PVCSyncManager) ensureDrSyncerNamespace(ctx context.Context, remoteClient *kubernetes.Clientset) error {
	log.Infof("Ensuring dr-syncer namespace exists in remote cluster")

	// Create namespace object
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dr-syncer",
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer",
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
	}

	// Try to create the namespace
	_, err := remoteClient.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil {
		// If the namespace already exists, that's fine
		if k8serrors.IsAlreadyExists(err) {
			log.Infof("Namespace dr-syncer already exists in remote cluster")
			return nil
		}
		return fmt.Errorf("failed to create dr-syncer namespace: %w", err)
	}

	log.Infof("Successfully created dr-syncer namespace in remote cluster")
	return nil
}

// getRemoteClient creates a Kubernetes client for the remote cluster
func (p *PVCSyncManager) getRemoteClient(ctx context.Context, rc *drv1alpha1.RemoteCluster) (*kubernetes.Clientset, error) {
	// Get the secret name from KubeconfigSecretRef
	secretName := rc.Spec.KubeconfigSecretRef.Name

	// Use the dr-syncer namespace for the secret
	secretNamespace := "dr-syncer"

	log.Infof("Getting kubeconfig secret %s/%s for remote cluster %s", secretNamespace, secretName, rc.Name)

	// Get the kubeconfig secret
	secret := &corev1.Secret{}
	if err := p.controllerClient.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: secretNamespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig secret: %v", err)
	}

	// Get the kubeconfig data
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig not found in secret %s/%s", secretNamespace, secretName)
	}

	// Create a temporary file for the kubeconfig
	kubeconfigFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary kubeconfig file: %v", err)
	}
	defer os.Remove(kubeconfigFile.Name())

	// Write the kubeconfig data to the file
	if _, err := kubeconfigFile.Write(kubeconfigData); err != nil {
		return nil, fmt.Errorf("failed to write kubeconfig data to file: %v", err)
	}
	if err := kubeconfigFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close kubeconfig file: %v", err)
	}

	// Create a Kubernetes client from the kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %v", err)
	}

	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %v", err)
	}

	return clientset, nil
}

// CreateTempPodForPVC creates a temporary pod that mounts the specified PVC
// The pod will be scheduled on the same node where the PVC is already mounted
// If the PVC is not mounted, the pod will be scheduled on any available node
func (p *PVCSyncManager) CreateTempPodForPVC(ctx context.Context, namespace, pvcName string, config *rest.Config) (*corev1.Pod, error) {
	log.Infof("Creating temporary pod for PVC %s/%s", namespace, pvcName)

	// Create a Kubernetes clientset from the config
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %v", err)
	}

	// Create pod manager
	podManager := temp.NewPodManager(p.remoteClient, k8sClient)

	// Find the node where the PVC is mounted
	nodeName, err := podManager.FindPVCNode(ctx, namespace, pvcName)
	if err != nil {
		log.Warnf("Could not find node for PVC %s/%s: %v", namespace, pvcName, err)
		log.Infof("Will create pod without node affinity")
		// Continue without node affinity
		nodeName = ""
	} else {
		log.Infof("Found node %s for PVC %s/%s", nodeName, namespace, pvcName)
	}

	// Create the pod
	pod, err := podManager.CreateTempPodForPVC(ctx, temp.PodOptions{
		Namespace: namespace,
		PVCName:   pvcName,
		NodeName:  nodeName,
		Image:     "busybox:latest",
		Command:   []string{"sleep", "3600"}, // Sleep for 1 hour
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary pod: %v", err)
	}

	// Wait for pod to be ready
	if err := podManager.WaitForPodReady(ctx, namespace, pod.Name, 2*time.Minute); err != nil {
		// Clean up the pod if it fails to become ready
		_ = podManager.DeletePod(ctx, namespace, pod.Name)
		return nil, fmt.Errorf("failed to wait for pod to be ready: %v", err)
	}

	log.Infof("Successfully created temporary pod %s for PVC %s/%s", pod.Name, namespace, pvcName)
	return pod, nil
}

// SyncAllAgents performs an initial sync of all agent deployments
func SyncAllAgents(ctx context.Context, client client.Client) error {
	// List all RemoteClusters
	var remoteClusterList drv1alpha1.RemoteClusterList
	if err := client.List(ctx, &remoteClusterList); err != nil {
		return fmt.Errorf("failed to list RemoteClusters: %v", err)
	}

	log.Infof("Performing initial sync for %d RemoteClusters", len(remoteClusterList.Items))

	// Process each RemoteCluster
	for i := range remoteClusterList.Items {
		rc := &remoteClusterList.Items[i]
		// Skip if PVC sync is not enabled
		if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
			log.Infof("Skipping initial sync for RemoteCluster %s: PVC sync not enabled", rc.Name)
			continue
		}

		log.Infof("Performing initial sync for RemoteCluster %s", rc.Name)

		// Create a PVCSyncManager for this RemoteCluster
		pvcSyncManager := NewPVCSyncManager(client, client)

		// Force sync by ignoring the time check
		if err := pvcSyncManager.ForceSync(ctx, rc); err != nil {
			log.Errorf("Failed to force sync agent for RemoteCluster %s: %v", rc.Name, err)
			// Continue with other clusters even if one fails
		} else {
			log.Infof("Successfully performed initial sync for RemoteCluster %s", rc.Name)
		}
	}

	return nil
}
