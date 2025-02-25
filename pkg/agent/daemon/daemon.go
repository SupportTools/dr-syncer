package daemon

import (
	"fmt"
	"os"

	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
)

// Daemon represents the agent daemon
type Daemon struct {
	sshServer *ssh.Server
}

// NewDaemon creates a new daemon instance
func NewDaemon(sshServer *ssh.Server) *Daemon {
	return &Daemon{
		sshServer: sshServer,
	}
}

// Start starts the daemon
func (d *Daemon) Start() error {
	// Verify kubelet path is mounted
	if err := d.verifyKubeletMount(); err != nil {
		return fmt.Errorf("kubelet mount verification failed: %v", err)
	}

	// Start SSH server
	if err := d.sshServer.Start(); err != nil {
		return fmt.Errorf("failed to start SSH server: %v", err)
	}

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

// verifyKubeletMount checks that /var/lib/kubelet is mounted
func (d *Daemon) verifyKubeletMount() error {
	kubeletPath := "/var/lib/kubelet"

	// Check if path exists
	if _, err := os.Stat(kubeletPath); err != nil {
		return fmt.Errorf("kubelet path not found: %s", kubeletPath)
	}

	// TODO: Add more specific checks for PVC mounts
	// This will be expanded when implementing the actual PVC sync logic

	return nil
}
