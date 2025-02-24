package remotecluster

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/deploy"
	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
)

// PVCSyncManager handles PVC sync operations
type PVCSyncManager struct {
	client     client.Client
	keyManager *ssh.KeyManager
	deployer   *deploy.Deployer
}

// NewPVCSyncManager creates a new PVC sync manager
func NewPVCSyncManager(client client.Client) *PVCSyncManager {
	return &PVCSyncManager{
		client:     client,
		keyManager: ssh.NewKeyManager(client),
		deployer:   deploy.NewDeployer(client),
	}
}

// Reconcile handles PVC sync reconciliation
func (p *PVCSyncManager) Reconcile(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Check if PVC sync is enabled
	if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
		return p.cleanupPVCSync(ctx, rc)
	}

	// Initialize status if needed
	if rc.Status.PVCSync == nil {
		rc.Status.PVCSync = &drv1alpha1.PVCSyncStatus{
			Phase: "Initializing",
		}
	}

	// Ensure SSH keys exist
	if err := p.keyManager.EnsureKeys(ctx, rc); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to ensure SSH keys: %v", err)
		return err
	}

	// Deploy agent components
	if err := p.deployer.Deploy(ctx, rc); err != nil {
		rc.Status.PVCSync.Phase = "Failed"
		rc.Status.PVCSync.Message = fmt.Sprintf("Failed to deploy agent: %v", err)
		return err
	}

	// Update status
	rc.Status.PVCSync.Phase = "Running"
	rc.Status.PVCSync.Message = "PVC sync agent deployed successfully"

	return nil
}

// cleanupPVCSync removes PVC sync components
func (p *PVCSyncManager) cleanupPVCSync(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Delete SSH keys
	if err := p.keyManager.DeleteKeys(ctx, rc); err != nil {
		return fmt.Errorf("failed to delete SSH keys: %v", err)
	}

	// TODO: Add cleanup for agent components
	// This will be implemented when we add the agent cleanup functionality

	// Clear status
	rc.Status.PVCSync = nil

	return nil
}

// RotateSSHKeys rotates SSH keys for the PVC sync agent
func (p *PVCSyncManager) RotateSSHKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
		return nil
	}

	if err := p.keyManager.RotateKeys(ctx, rc); err != nil {
		return fmt.Errorf("failed to rotate SSH keys: %v", err)
	}

	// TODO: Add agent pod restart logic to pick up new keys
	// This will be implemented when we add pod management functionality

	return nil
}
