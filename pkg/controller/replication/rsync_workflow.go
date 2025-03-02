package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
)

// RsyncWorkflow orchestrates the rsync process between source and destination PVCs
func (p *PVCSyncer) RsyncWorkflow(ctx context.Context, sourceNamespace, sourcePVCName, destNamespace, destPVCName string) error {
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
	}).Info("[DR-SYNC-START] Starting rsync workflow")

	// Set the namespaces in the PVCSyncer
	p.SourceNamespace = sourceNamespace
	p.DestinationNamespace = destNamespace
	
	// Track resources for cleanup
	var (
		lockAcquired   bool
		destRsyncPod   *rsyncpod.RsyncDeployment
	)
	
	// Deferred function to release lock on error returns
	defer func() {
		// We only handle panic recovery here, error returns are handled inline
		if r := recover(); r != nil {
			// Handle panics
			log.WithFields(logrus.Fields{
				"source_namespace": sourceNamespace,
				"source_pvc":       sourcePVCName,
				"panic":            r,
			}).Error("[DR-SYNC-ERROR] Panic during rsync workflow")
			
			// Clean up the deployment if it exists
			if destRsyncPod != nil {
				p.cleanupResources(ctx, destRsyncPod)
			}
			
			// Release the lock if we acquired it
			if lockAcquired {
				if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
					log.WithFields(logrus.Fields{
						"source_namespace": sourceNamespace,
						"source_pvc":       sourcePVCName,
						"error":            relErr,
					}).Warn("[DR-SYNC-WARN] Failed to release lock on source PVC after panic")
				}
			}
		}
	}()
	
	// Step 0: Try to acquire a lock on the source PVC
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info("[DR-SYNC-STEP-0] Acquiring lock on source PVC")
	
	acquired, lockInfo, err := p.AcquirePVCLock(ctx, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error("[DR-SYNC-ERROR] Failed to check lock on source PVC")
		return fmt.Errorf("failed to check lock on source PVC: %v", err)
	}
	
	if !acquired {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"lock_owner":       lockInfo.ControllerPodName,
			"lock_timestamp":   lockInfo.Timestamp,
		}).Info("[DR-SYNC-SKIP] Source PVC is locked by another controller, skipping rsync")
		return nil
	}
	
	// Mark lock as acquired
	lockAcquired = true
	
	// Cleanup existing rsync deployments for this PVC if we're taking over
	rsyncMgr, err := rsyncpod.NewManager(p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to create rsync manager")
		
		// Release the lock since we're failing
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName, 
					"error":            relErr,
				}).Warn("[DR-SYNC-WARN] Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to create rsync manager: %v", err)
	}
	
	if err := rsyncMgr.CleanupExistingDeployments(ctx, destNamespace, destPVCName); err != nil {
		log.WithFields(logrus.Fields{
			"dest_namespace": destNamespace,
			"dest_pvc":       destPVCName,
			"error":          err,
		}).Warn("[DR-SYNC-WARN] Failed to cleanup existing deployments, will continue anyway")
	}
	
	log.Info("[DR-SYNC-STEP-0-COMPLETE] Lock acquired on source PVC")
	
	// Step 1: Deploy rsync deployment in destination cluster and wait for it to be ready
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
	}).Info("[DR-SYNC-STEP-1] Deploying rsync pod in destination cluster")
	
	destRsyncPod, err = p.deployRsyncPod(ctx, destNamespace, destPVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to deploy rsync pod in destination cluster")
		return fmt.Errorf("failed to deploy rsync pod in destination cluster: %v", err)
	}
	log.Info("[DR-SYNC-STEP-1-COMPLETE] Rsync pod deployed successfully")

	// Step 2: Generate SSH keys in the rsync pod
	log.WithFields(logrus.Fields{
		"pod_name": destRsyncPod.Name,
	}).Info("[DR-SYNC-STEP-2] Generating SSH keys in rsync pod")
	
	if err := p.generateSSHKeys(ctx, destRsyncPod); err != nil {
		log.WithFields(logrus.Fields{
			"pod_name": destRsyncPod.Name,
			"error":    err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}
	log.Info("[DR-SYNC-STEP-2-COMPLETE] SSH keys generated successfully")

	// Step 3: Get the public key from the rsync pod
	log.WithFields(logrus.Fields{
		"pod_name": destRsyncPod.Name,
	}).Info("[DR-SYNC-STEP-3] Getting public key from rsync pod")
	
	publicKey, err := p.getPublicKey(ctx, destRsyncPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod_name": destRsyncPod.Name,
			"error":    err,
		}).Error("[DR-SYNC-ERROR] Failed to get public key")
		return fmt.Errorf("failed to get public key: %v", err)
	}
	log.Info("[DR-SYNC-STEP-3-COMPLETE] Public key retrieved successfully")

	// Step 4: Check if source PVC is mounted
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info("[DR-SYNC-STEP-4] Checking if source PVC is mounted")
	
	mounted, err := p.HasVolumeAttachments(ctx, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error("[DR-SYNC-ERROR] Failed to check if source PVC is mounted")
		return fmt.Errorf("failed to check if source PVC is mounted: %v", err)
	}

	if !mounted {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
		}).Info("[DR-SYNC-SKIP] Source PVC is not mounted, skipping rsync")
		// Clean up resources before returning
		p.cleanupResources(ctx, destRsyncPod)
		return nil
	}
	log.Info("[DR-SYNC-STEP-4-COMPLETE] Source PVC is mounted")

	// Step 5: Find the node where the source PVC is mounted
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info("[DR-SYNC-STEP-5] Finding node where source PVC is mounted")
	
	sourceNode, err := p.FindPVCNode(ctx, p.SourceClient, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error("[DR-SYNC-ERROR] Failed to find node where source PVC is mounted")
		return fmt.Errorf("failed to find node where source PVC is mounted: %v", err)
	}
	log.WithFields(logrus.Fields{
		"source_node": sourceNode,
	}).Info("[DR-SYNC-STEP-5-COMPLETE] Found node where source PVC is mounted")

	// Step 6: Find the DR-Syncer-Agent running on that node and get the node's external IP
	log.WithFields(logrus.Fields{
		"node": sourceNode,
	}).Info("[DR-SYNC-STEP-6] Finding DR-Syncer-Agent on node")
	
	agentPod, nodeIP, err := p.FindAgentPod(ctx, sourceNode)
	if err != nil {
		log.WithFields(logrus.Fields{
			"node":  sourceNode,
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find DR-Syncer-Agent")
		return fmt.Errorf("failed to find DR-Syncer-Agent on node %s: %v", sourceNode, err)
	}
	log.WithFields(logrus.Fields{
		"node":      sourceNode,
		"agent_pod": agentPod.Name,
		"node_ip":   nodeIP,
	}).Info("[DR-SYNC-STEP-6-COMPLETE] Found DR-Syncer-Agent")

	// Step 7: Find the mount path for the PVC
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"agent_pod":        agentPod.Name,
	}).Info("[DR-SYNC-STEP-7] Finding mount path for PVC")
	
	mountPath, err := p.FindPVCMountPath(ctx, sourceNamespace, sourcePVCName, agentPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"agent_pod":        agentPod.Name,
			"error":            err,
		}).Error("[DR-SYNC-ERROR] Failed to find mount path for PVC")
		return fmt.Errorf("failed to find mount path for PVC: %v", err)
	}
	log.WithFields(logrus.Fields{
		"mount_path": mountPath,
	}).Info("[DR-SYNC-STEP-7-COMPLETE] Found mount path for PVC")

	// Step 8: Push the public key to the agent pod
	log.WithFields(logrus.Fields{
		"agent_pod": agentPod.Name,
	}).Info("[DR-SYNC-STEP-8] Pushing public key to agent pod")
	
	trackingInfo := fmt.Sprintf("dr-syncer-rsync-%s-%s", destNamespace, rand.String(8))
	if err := p.PushPublicKeyToAgent(ctx, agentPod, publicKey, trackingInfo); err != nil {
		log.WithFields(logrus.Fields{
			"agent_pod": agentPod.Name,
			"error":     err,
		}).Error("[DR-SYNC-ERROR] Failed to push public key to agent pod")
		return fmt.Errorf("failed to push public key to agent pod: %v", err)
	}
	log.Info("[DR-SYNC-STEP-8-COMPLETE] Public key pushed to agent pod")

	// Step 9: Test SSH connectivity using the node's external IP
	log.WithFields(logrus.Fields{
		"dest_pod":  destRsyncPod.Name,
		"agent_pod": agentPod.Name,
		"node_ip":   nodeIP,
		"ssh_port":  2222,
	}).Info("[DR-SYNC-STEP-9] Testing SSH connectivity")
	
	if err := p.TestSSHConnectivity(ctx, destRsyncPod, nodeIP, 2222); err != nil {
		log.WithFields(logrus.Fields{
			"dest_pod":  destRsyncPod.Name,
			"agent_pod": agentPod.Name,
			"node_ip":   nodeIP,
			"ssh_port":  2222,
			"error":     err,
		}).Error("[DR-SYNC-ERROR] Failed to test SSH connectivity")
		return fmt.Errorf("failed to test SSH connectivity: %v", err)
	}
	log.Info("[DR-SYNC-STEP-9-COMPLETE] SSH connectivity test successful")

	// Step 10: Run rsync command using the node's external IP
	log.WithFields(logrus.Fields{
		"dest_pod":   destRsyncPod.Name,
		"node_ip":    nodeIP,
		"mount_path": mountPath,
	}).Info("[DR-SYNC-STEP-10] Running rsync command")
	
	if err := p.performRsync(ctx, destRsyncPod, nodeIP, mountPath); err != nil {
		log.WithFields(logrus.Fields{
			"dest_pod":   destRsyncPod.Name,
			"node_ip":    nodeIP,
			"mount_path": mountPath,
			"error":      err,
		}).Error("[DR-SYNC-ERROR] Failed to perform rsync")
		return fmt.Errorf("failed to perform rsync: %v", err)
	}
	log.Info("[DR-SYNC-STEP-10-COMPLETE] Rsync completed successfully")

	// Step 11: Update source PVC annotations
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info("[DR-SYNC-STEP-11] Updating source PVC annotations")
	
	if err := p.UpdateSourcePVCAnnotations(ctx, sourceNamespace, sourcePVCName); err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error("[DR-SYNC-ERROR] Failed to update source PVC annotations")
		return fmt.Errorf("failed to update source PVC annotations: %v", err)
	}
	log.Info("[DR-SYNC-STEP-11-COMPLETE] Source PVC annotations updated successfully")

	// Step 12: Clean up resources
	log.WithFields(logrus.Fields{
		"dest_pod": destRsyncPod.Name,
	}).Info("[DR-SYNC-STEP-12] Cleaning up resources")
	
	p.cleanupResources(ctx, destRsyncPod)
	log.Info("[DR-SYNC-STEP-12-COMPLETE] Resource cleanup completed")

	// Step 13: Release the lock on the source PVC
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info("[DR-SYNC-STEP-13] Releasing lock on source PVC")
	
	if err := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Warn("[DR-SYNC-WARN] Failed to release lock on source PVC")
		// Continue despite error - this is just a warning
	}
	log.Info("[DR-SYNC-STEP-13-COMPLETE] Lock released on source PVC")

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
	}).Info("[DR-SYNC-COMPLETE] Rsync workflow completed successfully")

	return nil
}

// deployRsyncPod deploys an rsync deployment in the destination cluster
func (p *PVCSyncer) deployRsyncPod(ctx context.Context, namespace, pvcName string) (*rsyncpod.RsyncDeployment, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("[DR-SYNC-DETAIL] Deploying rsync deployment in destination cluster")

	// Create RsyncPod manager
	rsyncMgr, err := rsyncpod.NewManager(p.DestinationConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync manager: %v", err)
	}

	// Generate a unique ID for this sync operation
	syncID := rand.String(8)

	// Create rsync pod options
	opts := rsyncpod.RsyncPodOptions{
		Namespace:        namespace,
		PVCName:          pvcName,
		Type:             rsyncpod.DestinationPodType,
		SyncID:           syncID,
		ReplicationName:  fmt.Sprintf("pvc-sync-%s-%s", namespace, pvcName),
		DestinationInfo:  fmt.Sprintf("destination-%s-%s", namespace, pvcName),
	}

	// Create the rsync deployment
	rsyncDeployment, err := rsyncMgr.CreateRsyncDeployment(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync deployment: %v", err)
	}

	// Wait for the deployment to be ready
	timeout := 5 * time.Minute
	if err := rsyncDeployment.WaitForPodReady(ctx, timeout); err != nil {
		return nil, fmt.Errorf("timeout waiting for rsync deployment to be ready: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace":   namespace,
		"pvc_name":    pvcName,
		"deployment":  rsyncDeployment.Name,
		"pod_name":    rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] Rsync deployment is ready with running pod")

	return rsyncDeployment, nil
}

// generateSSHKeys generates SSH keys in the rsync pod
func (p *PVCSyncer) generateSSHKeys(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment) error {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] Generating SSH keys in rsync pod")

	// Generate SSH keys
	if err := rsyncDeployment.GenerateSSHKeys(ctx); err != nil {
		log.WithFields(logrus.Fields{
			"deployment": rsyncDeployment.Name,
			"pod_name":   rsyncDeployment.PodName,
			"error":      err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] SSH keys generated successfully")

	return nil
}

// getPublicKey gets the public key from the rsync pod
func (p *PVCSyncer) getPublicKey(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment) (string, error) {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] Getting public key from rsync pod")

	// Get public key
	publicKey, err := rsyncDeployment.GetPublicKey(ctx)
	if err != nil {
		log.WithFields(logrus.Fields{
			"deployment": rsyncDeployment.Name,
			"pod_name":   rsyncDeployment.PodName,
			"error":      err,
		}).Error("[DR-SYNC-ERROR] Failed to get public key")
		return "", fmt.Errorf("failed to get public key: %v", err)
	}

	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] Public key retrieved successfully")

	return publicKey, nil
}

// cleanupResources cleans up resources used in the rsync workflow
func (p *PVCSyncer) cleanupResources(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment) {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] Cleaning up resources")

	// Delete the rsync deployment
	if err := rsyncDeployment.Cleanup(ctx); err != nil {
		log.WithFields(logrus.Fields{
			"deployment": rsyncDeployment.Name,
			"pod_name":   rsyncDeployment.PodName,
			"error":      err,
		}).Error("[DR-SYNC-ERROR] Failed to cleanup rsync deployment")
	}

	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info("[DR-SYNC-DETAIL] Resource cleanup completed")
}
