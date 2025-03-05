package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/logging"
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
	}).Info(logging.LogTagInfo + " Starting rsync workflow")

	// Set the namespaces in the PVCSyncer
	p.SourceNamespace = sourceNamespace
	p.DestinationNamespace = destNamespace

	// Track resources for cleanup
	var (
		lockAcquired bool
		destRsyncPod *rsyncpod.RsyncDeployment
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
			}).Error(logging.LogTagError + " Panic during rsync workflow")

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
					}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after panic")
				}
			}
		}
	}()

	// Step 0: Try to acquire a lock on the source PVC
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info(logging.LogTagStep0 + " Acquiring lock on source PVC")

	acquired, lockInfo, err := p.AcquirePVCLock(ctx, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error(logging.LogTagError + " Failed to check lock on source PVC")
		return fmt.Errorf("failed to check lock on source PVC: %v", err)
	}

	if !acquired {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"lock_owner":       lockInfo.ControllerPodName,
			"lock_timestamp":   lockInfo.Timestamp,
		}).Info(logging.LogTagSkip + " Source PVC is locked by another controller, skipping rsync")
		return nil
	}

	// Mark lock as acquired
	lockAcquired = true

	// Cleanup existing rsync deployments for this PVC if we're taking over
	rsyncMgr, err := rsyncpod.NewManager(p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error(logging.LogTagError + " Failed to create rsync manager")

		// Release the lock since we're failing
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to create rsync manager: %v", err)
	}

	if err := rsyncMgr.CleanupExistingDeployments(ctx, destNamespace, destPVCName); err != nil {
		log.WithFields(logrus.Fields{
			"dest_namespace": destNamespace,
			"dest_pvc":       destPVCName,
			"error":          err,
		}).Warn(logging.LogTagWarn + " Failed to cleanup existing deployments, will continue anyway")
	}

	log.Info(logging.LogTagStep0Complete + " Lock acquired on source PVC")

	// Step 1: Deploy rsync deployment in destination cluster and wait for it to be ready
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
	}).Info(logging.LogTagStep1 + " Deploying rsync pod in destination cluster")

	// Deploy the rsync pod which will start in waiting state (sleep infinity)
	destRsyncPod, err = p.deployRsyncPod(ctx, destNamespace, destPVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error(logging.LogTagError + " Failed to deploy rsync pod in destination cluster")

		// Release the lock since we're failing
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to deploy rsync pod in destination cluster: %v", err)
	}
	log.Info(logging.LogTagStep1Complete + " Rsync pod deployed successfully")

	// Step 2: Generate SSH keys in the rsync pod
	log.WithFields(logrus.Fields{
		"pod_name": destRsyncPod.Name,
	}).Info(logging.LogTagStep2 + " Generating SSH keys in rsync pod")

	if err := p.generateSSHKeys(ctx, destRsyncPod); err != nil {
		log.WithFields(logrus.Fields{
			"pod_name": destRsyncPod.Name,
			"error":    err,
		}).Error(logging.LogTagError + " Failed to generate SSH keys")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}
	log.Info(logging.LogTagStep2Complete + " SSH keys generated successfully")

	// Step 3: Get the public key from the rsync pod
	log.WithFields(logrus.Fields{
		"pod_name": destRsyncPod.Name,
	}).Info(logging.LogTagStep3 + " Getting public key from rsync pod")

	publicKey, err := p.getPublicKey(ctx, destRsyncPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod_name": destRsyncPod.Name,
			"error":    err,
		}).Error(logging.LogTagError + " Failed to get public key")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to get public key: %v", err)
	}
	log.Info(logging.LogTagStep3Complete + " Public key retrieved successfully")

	// Step 4: Check if source PVC is mounted
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info(logging.LogTagStep4 + " Checking if source PVC is mounted")

	mounted, err := p.HasVolumeAttachments(ctx, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error(logging.LogTagError + " Failed to check if source PVC is mounted")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to check if source PVC is mounted: %v", err)
	}

	if !mounted {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
		}).Info(logging.LogTagSkip + " Source PVC is not mounted, skipping rsync")
		// Clean up resources before returning
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after skipping")
			}
		}
		return nil
	}
	log.Info(logging.LogTagStep4Complete + " Source PVC is mounted")

	// Step 5: Find the node(s) where the source PVC is mounted
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info(logging.LogTagStep5 + " Finding node where source PVC is mounted")

	// Find the node where the PVC is mounted
	sourceNode, err := p.FindPVCNode(ctx, p.SourceClient, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error(logging.LogTagError + " Failed to find node where source PVC is mounted")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to find node where source PVC is mounted: %v", err)
	}

	log.WithFields(logrus.Fields{
		"source_node": sourceNode,
	}).Info(logging.LogTagStep5Complete + " Found node where source PVC is mounted")

	// Step 6: Find the DR-Syncer-Agent running on that node and get the node's external IP
	log.WithFields(logrus.Fields{
		"node": sourceNode,
	}).Info(logging.LogTagStep6 + " Finding DR-Syncer-Agent on node")

	agentPod, nodeIP, err := p.FindAgentPod(ctx, sourceNode)
	if err != nil {
		log.WithFields(logrus.Fields{
			"node":  sourceNode,
			"error": err,
		}).Error(logging.LogTagError + " Failed to find DR-Syncer-Agent")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to find DR-Syncer-Agent on node %s: %v", sourceNode, err)
	}
	log.WithFields(logrus.Fields{
		"node":      sourceNode,
		"agent_pod": agentPod.Name,
		"node_ip":   nodeIP,
	}).Info(logging.LogTagStep6Complete + " Found DR-Syncer-Agent")

	// Step 7: Find the mount path for the PVC
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"agent_pod":        agentPod.Name,
	}).Info(logging.LogTagStep7 + " Finding mount path for PVC")

	mountPath, err := p.FindPVCMountPath(ctx, sourceNamespace, sourcePVCName, agentPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"agent_pod":        agentPod.Name,
			"error":            err,
		}).Error(logging.LogTagError + " Failed to find mount path for PVC")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to find mount path for PVC: %v", err)
	}
	log.WithFields(logrus.Fields{
		"mount_path": mountPath,
	}).Info(logging.LogTagStep7Complete + " Found mount path for PVC")

	// Step 8: Push the public key to the agent pod
	log.WithFields(logrus.Fields{
		"agent_pod": agentPod.Name,
	}).Info(logging.LogTagStep8 + " Pushing public key to agent pod")

	trackingInfo := fmt.Sprintf("dr-syncer-rsync-%s-%s", destNamespace, rand.String(8))
	if err := p.PushPublicKeyToAgent(ctx, agentPod, publicKey, trackingInfo); err != nil {
		log.WithFields(logrus.Fields{
			"agent_pod": agentPod.Name,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to push public key to agent pod")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to push public key to agent pod: %v", err)
	}
	log.Info(logging.LogTagStep8Complete + " Public key pushed to agent pod")

	// Step 9: Test SSH connectivity
	log.WithFields(logrus.Fields{
		"dest_pod": destRsyncPod.Name,
		"node_ip":  nodeIP,
	}).Info(logging.LogTagStep9 + " Testing SSH connectivity")

	// Test SSH connectivity to make sure we can reach the agent
	err = p.TestSSHConnectivity(ctx, destRsyncPod, nodeIP, 2222, p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"dest_pod": destRsyncPod.Name,
			"node_ip":  nodeIP,
			"error":    err,
		}).Error(logging.LogTagError + " Failed to test SSH connectivity")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to test SSH connectivity: %v", err)
	}
	log.Info(logging.LogTagStep9Complete + " SSH connectivity test successful")

	// Step 10: Run rsync command using the node's external IP
	log.WithFields(logrus.Fields{
		"dest_pod":   destRsyncPod.Name,
		"node_ip":    nodeIP,
		"mount_path": mountPath,
	}).Info(logging.LogTagStep10 + " Running rsync command")

	if err := p.performRsync(ctx, destRsyncPod, nodeIP, mountPath); err != nil {
		log.WithFields(logrus.Fields{
			"dest_pod":   destRsyncPod.Name,
			"node_ip":    nodeIP,
			"mount_path": mountPath,
			"error":      err,
		}).Error(logging.LogTagError + " Failed to perform rsync")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to perform rsync: %v", err)
	}
	log.Info(logging.LogTagStep10Complete + " Rsync completed successfully")

	// Step 11: Update source PVC annotations
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info(logging.LogTagStep11 + " Updating source PVC annotations")

	if err := p.UpdateSourcePVCAnnotations(ctx, sourceNamespace, sourcePVCName); err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error(logging.LogTagError + " Failed to update source PVC annotations")

		// Clean up resources
		p.cleanupResources(ctx, destRsyncPod)

		// Release the lock
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to update source PVC annotations: %v", err)
	}
	log.Info(logging.LogTagStep11Complete + " Source PVC annotations updated successfully")

	// Step 12: Clean up resources
	log.WithFields(logrus.Fields{
		"dest_pod": destRsyncPod.Name,
	}).Info(logging.LogTagStep12 + " Cleaning up resources")

	p.cleanupResources(ctx, destRsyncPod)
	log.Info(logging.LogTagStep12Complete + " Resource cleanup completed")

	// Step 13: Release the lock on the source PVC
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info(logging.LogTagStep13 + " Releasing lock on source PVC")

	if err := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC")
		// Continue despite error - this is just a warning
	}
	log.Info(logging.LogTagStep13Complete + " Lock released on source PVC")

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
	}).Info(logging.LogTagComplete + " Rsync workflow completed successfully")

	return nil
}

// deployRsyncPod deploys an rsync deployment in the destination cluster
func (p *PVCSyncer) deployRsyncPod(ctx context.Context, namespace, pvcName string) (*rsyncpod.RsyncDeployment, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info(logging.LogTagDetail + " Deploying rsync deployment in destination cluster")

	// Create RsyncPod manager
	rsyncMgr, err := rsyncpod.NewManager(p.DestinationConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync manager: %v", err)
	}

	// Generate a unique ID for this sync operation
	syncID := rand.String(8)

	// Create rsync pod options
	opts := rsyncpod.RsyncPodOptions{
		Namespace:       namespace,
		PVCName:         pvcName,
		Type:            rsyncpod.DestinationPodType,
		SyncID:          syncID,
		ReplicationName: fmt.Sprintf("pvc-sync-%s-%s", namespace, pvcName),
		DestinationInfo: fmt.Sprintf("destination-%s-%s", namespace, pvcName),
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
		"namespace":  namespace,
		"pvc_name":   pvcName,
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info(logging.LogTagDetail + " Rsync deployment is ready with running pod")

	return rsyncDeployment, nil
}

// The custom context key types are defined in pvc_sync.go

// generateSSHKeys generates SSH keys in the rsync pod
func (p *PVCSyncer) generateSSHKeys(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment) error {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info(logging.LogTagDetail + " Generating SSH keys in rsync pod")

	// Put the PVCSyncer in the context for SSH key generation
	syncerCtx := context.WithValue(ctx, syncerKey, p)

	// Generate SSH keys - use the context with PVCSyncer and explicit config
	log.WithFields(logrus.Fields{
		"deployment":       rsyncDeployment.Name,
		"pod_name":         rsyncDeployment.PodName,
		"dest_config_host": p.DestinationConfig.Host,
	}).Info(logging.LogTagDetail + " Executing SSH key generation with explicit destination config")

	if err := rsyncDeployment.GenerateSSHKeys(syncerCtx, p.DestinationConfig); err != nil {
		log.WithFields(logrus.Fields{
			"deployment": rsyncDeployment.Name,
			"pod_name":   rsyncDeployment.PodName,
			"error":      err,
		}).Error(logging.LogTagError + " Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info(logging.LogTagDetail + " SSH keys generated successfully")

	return nil
}

// getPublicKey gets the public key from the rsync pod
func (p *PVCSyncer) getPublicKey(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment) (string, error) {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info(logging.LogTagDetail + " Getting public key from rsync pod")

	// Put the PVCSyncer in the context for getting public key
	syncerCtx := context.WithValue(ctx, syncerKey, p)

	// Get public key - use the context with PVCSyncer and explicit config
	log.WithFields(logrus.Fields{
		"deployment":       rsyncDeployment.Name,
		"pod_name":         rsyncDeployment.PodName,
		"dest_config_host": p.DestinationConfig.Host,
	}).Info(logging.LogTagDetail + " Getting public key with explicit destination config")

	publicKey, err := rsyncDeployment.GetPublicKey(syncerCtx, p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"deployment": rsyncDeployment.Name,
			"pod_name":   rsyncDeployment.PodName,
			"error":      err,
		}).Error(logging.LogTagError + " Failed to get public key")
		return "", fmt.Errorf("failed to get public key: %v", err)
	}

	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info(logging.LogTagDetail + " Public key retrieved successfully")

	return publicKey, nil
}

// cleanupResources cleans up resources used in the rsync workflow
func (p *PVCSyncer) cleanupResources(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment) {
	if rsyncDeployment == nil {
		log.Warn(logging.LogTagWarn + " Skipping cleanup, rsyncDeployment is nil")
		return
	}

	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
	}).Info(logging.LogTagDetail + " Cleaning up resources")

	// Put the PVCSyncer in the context for cleanup
	syncerCtx := context.WithValue(ctx, syncerKey, p)

	// Delete the rsync deployment with context containing PVCSyncer
	log.WithFields(logrus.Fields{
		"deployment":       rsyncDeployment.Name,
		"pod_name":         rsyncDeployment.PodName,
		"dest_config_host": p.DestinationConfig.Host,
	}).Info(logging.LogTagDetail + " Executing cleanup with destination config context")

	if err := rsyncDeployment.Cleanup(syncerCtx); err != nil {
		log.WithFields(logrus.Fields{
			"deployment": rsyncDeployment.Name,
			"pod_name":   rsyncDeployment.PodName,
			"error":      err,
		}).Warn(logging.LogTagWarn + " Failed to cleanup rsync deployment, will continue anyway")
	}
}
