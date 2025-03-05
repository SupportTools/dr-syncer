package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// DefaultShutdownTimeout is the default timeout for graceful shutdown
	DefaultShutdownTimeout = 30 * time.Second
)

// Daemon represents the agent daemon
type Daemon struct {
	sshServer *ssh.Server
	client    kubernetes.Interface
	config    *rest.Config
	namespace string
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

// InitKeySystem initializes the SSH key management system
func (d *Daemon) InitKeySystem(ctx context.Context, client kubernetes.Interface) error {
	if d.namespace == "" {
		return fmt.Errorf("namespace not set")
	}

	d.client = client

	// Check if keys already exist
	secretName := "pvc-syncer-agent-keys"
	_, err := client.CoreV1().Secrets(d.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		fmt.Printf("SSH key secret %s already exists in namespace %s\n", secretName, d.namespace)
	} else {
		// Keys don't exist, generate them
		fmt.Printf("SSH key secret %s does not exist in namespace %s, will be created by leader\n", secretName, d.namespace)
	}

	// Schedule key rotation
	ssh.ScheduleKeyRotation(ctx, client, d.namespace, secretName, ssh.DefaultKeyRotationInterval)

	return nil
}

// InitTempManager is kept for backwards compatibility but is now a no-op
func (d *Daemon) InitTempManager(config *rest.Config) error {
	// This is now a no-op as we don't use temporary pods anymore
	// Store the config for later use
	d.config = config
	return nil
}

// CleanupTempPods is kept for backwards compatibility but is now a no-op
func (d *Daemon) CleanupTempPods(ctx context.Context) error {
	// This is now a no-op as we don't use temporary pods anymore
	return nil
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

// GetClient returns the kubernetes client
func (d *Daemon) GetClient() kubernetes.Interface {
	return d.client
}
