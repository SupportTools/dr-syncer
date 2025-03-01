package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
	"github.com/supporttools/dr-syncer/pkg/agent/sshkeys"
	"github.com/supporttools/dr-syncer/pkg/agent/tempod"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// DefaultShutdownTimeout is the default timeout for graceful shutdown
	DefaultShutdownTimeout = 30 * time.Second
)

// Daemon represents the agent daemon
type Daemon struct {
	sshServer   *ssh.Server
	tempManager *tempod.Manager
	keySystem   *sshkeys.KeySystem
	config      *rest.Config
	namespace   string
}

// NewDaemon creates a new daemon instance
func NewDaemon(sshServer *ssh.Server) *Daemon {
	return &Daemon{
		sshServer: sshServer,
	}
}

// SetNamespace sets the namespace for the daemon
func (d *Daemon) SetNamespace(namespace string) {
	d.namespace = namespace
}

// InitTempManager initializes the temporary pod manager
func (d *Daemon) InitTempManager(config *rest.Config) error {
	// Create temp pod manager
	manager, err := tempod.NewManager(config)
	if err != nil {
		return fmt.Errorf("failed to create temp pod manager: %v", err)
	}

	d.tempManager = manager
	d.config = config

	return nil
}

// InitKeySystem initializes the SSH key management system
func (d *Daemon) InitKeySystem(ctx context.Context, client kubernetes.Interface) error {
	if d.namespace == "" {
		return fmt.Errorf("namespace not set")
	}

	// Create key system
	d.keySystem = sshkeys.NewKeySystem(client, d.namespace)

	// Initialize keys
	if err := d.keySystem.InitializeKeys(ctx); err != nil {
		return fmt.Errorf("failed to initialize keys: %v", err)
	}

	// Schedule key rotation
	d.keySystem.ScheduleKeyRotation(ctx, sshkeys.DefaultKeyRotationInterval)

	// Initialize secret watcher to dynamically load authorized keys from secret
	if err := sshkeys.InitializeSecretWatcher(ctx, client, d.namespace); err != nil {
		fmt.Printf("Warning: Failed to initialize secret watcher: %v\n", err)
		// Don't fail if secret watcher initialization fails
		// This allows backward compatibility with direct file updates
	}

	return nil
}

// CreateTempPodWithKeys creates a temporary pod with SSH keys
func (d *Daemon) CreateTempPodWithKeys(ctx context.Context, namespace, pvcName, nodeName string) (*tempod.TempPod, *sshkeys.KeyPair, error) {
	if d.tempManager == nil {
		return nil, nil, fmt.Errorf("temp pod manager not initialized")
	}

	if d.keySystem == nil {
		return nil, nil, fmt.Errorf("key system not initialized")
	}

	// Create temp pod
	pod, err := d.CreateTempPod(ctx, namespace, pvcName, nodeName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp pod: %v", err)
	}

	// Create keys for the pod
	keyPair, err := d.keySystem.CreateTempPodKeys(ctx, pod.Name)
	if err != nil {
		// Attempt to clean up the pod
		_ = pod.Cleanup(ctx, tempod.DefaultPodCleanupTimeout)
		return nil, nil, fmt.Errorf("failed to create keys for pod: %v", err)
	}

	return pod, keyPair, nil
}

// CleanupTempPodWithKeys cleans up a temporary pod and its keys
func (d *Daemon) CleanupTempPodWithKeys(ctx context.Context, pod *tempod.TempPod) error {
	if d.tempManager == nil || d.keySystem == nil {
		return fmt.Errorf("temp pod manager or key system not initialized")
	}

	// Clean up keys
	if err := d.keySystem.CleanupTempPodKeys(ctx, pod.Name); err != nil {
		return fmt.Errorf("failed to clean up keys: %v", err)
	}

	// Clean up pod
	if err := pod.Cleanup(ctx, tempod.DefaultPodCleanupTimeout); err != nil {
		return fmt.Errorf("failed to clean up pod: %v", err)
	}

	return nil
}

// CreateTempPod creates a temporary pod for PVC access
func (d *Daemon) CreateTempPod(ctx context.Context, namespace, pvcName, nodeName string) (*tempod.TempPod, error) {
	if d.tempManager == nil {
		return nil, fmt.Errorf("temp pod manager not initialized")
	}

	// Ensure rsync ConfigMap exists
	if err := tempod.EnsureRsyncConfigMap(ctx, d.tempManager.Client, namespace); err != nil {
		return nil, fmt.Errorf("failed to ensure rsync ConfigMap: %v", err)
	}

	// Create temp pod
	pod, err := d.tempManager.CreateTempPod(ctx, tempod.TempPodOptions{
		Namespace: namespace,
		PVCName:   pvcName,
		NodeName:  nodeName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temp pod: %v", err)
	}

	// Wait for pod to be ready
	if err := pod.WaitForPodReady(ctx, tempod.DefaultPodReadyTimeout); err != nil {
		// Attempt to clean up the pod
		_ = pod.Cleanup(ctx, tempod.DefaultPodCleanupTimeout)
		return nil, fmt.Errorf("failed to wait for pod to be ready: %v", err)
	}

	return pod, nil
}

// CreateTempPodForPVC creates a temporary pod for a PVC, automatically finding the node
func (d *Daemon) CreateTempPodForPVC(ctx context.Context, namespace, pvcName string) (*tempod.TempPod, error) {
	if d.tempManager == nil {
		return nil, fmt.Errorf("temp pod manager not initialized")
	}

	// Find node where PVC is mounted
	nodeName, err := tempod.FindPVCNode(ctx, d.tempManager.Client, namespace, pvcName)
	if err != nil {
		return nil, fmt.Errorf("failed to find node for PVC: %v", err)
	}

	// Create temp pod on the node
	return d.CreateTempPod(ctx, namespace, pvcName, nodeName)
}

// CleanupTempPods cleans up all temporary pods
func (d *Daemon) CleanupTempPods(ctx context.Context) error {
	if d.tempManager == nil {
		return nil
	}

	return d.tempManager.CleanupAll(ctx, tempod.DefaultPodCleanupTimeout)
}

// Start starts the daemon
func (d *Daemon) Start() error {
	// Start SSH server
	if err := d.sshServer.Start(); err != nil {
		return fmt.Errorf("failed to start SSH server: %v", err)
	}

	fmt.Println("Agent daemon started successfully")
	fmt.Println("SSH proxy server running on port", d.sshServer.Port())

	return nil
}

// Stop stops the daemon
func (d *Daemon) Stop() error {
	// Stop SSH server
	if err := d.sshServer.Stop(); err != nil {
		return fmt.Errorf("failed to stop SSH server: %v", err)
	}

	return nil
}

// GetProxyInfo returns information about the proxy configuration
func (d *Daemon) GetProxyInfo() map[string]interface{} {
	return map[string]interface{}{
		"ssh_port": d.sshServer.Port(),
		"status":   "running",
		"mode":     "proxy",
	}
}

// GetKeySystem returns the key system
func (d *Daemon) GetKeySystem() *sshkeys.KeySystem {
	return d.keySystem
}
