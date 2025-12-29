package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
	"github.com/supporttools/dr-syncer/pkg/logging"
)

// RsyncWorkflow orchestrates the rsync process between source and destination PVCs
func (p *PVCSyncer) RsyncWorkflow(ctx context.Context, sourceNamespace, sourcePVCName, destNamespace, destPVCName string) error {
	// Track start time for duration calculation
	startTime := time.Now()

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
	}).Info(logging.LogTagInfo + " Starting rsync workflow")

	// Emit SyncStarted event
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncStarted,
		"Starting PVC data sync to %s/%s", destNamespace, destPVCName)

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

		// Emit SyncSkipped event
		p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncSkipped,
			"PVC is locked by %s, skipping sync", lockInfo.ControllerPodName)
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

	// Emit LockAcquired event
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonLockAcquired,
		"Acquired sync lock for PVC")

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

		// Emit SyncFailed event
		p.RecordWarningEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncFailed,
			"Failed to deploy rsync pod: %v", err)

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

	// Emit RsyncPodDeployed event
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonRsyncPodDeployed,
		"Rsync pod deployed in destination cluster")

	// Steps 2-3: Generate SSH keys and get public key (skip if using cached keys)
	var publicKey string
	if destRsyncPod.HasCachedKeys {
		log.WithFields(logrus.Fields{
			"pod_name": destRsyncPod.Name,
		}).Info(logging.LogTagStep2 + " Skipping SSH key generation - using pre-provisioned cached keys")
		log.Info(logging.LogTagStep2Complete + " SSH keys already mounted from cache")
		log.Info(logging.LogTagStep3 + " Skipping public key retrieval - using cached keys")
		log.Info(logging.LogTagStep3Complete + " Public key already provisioned on agent")
	} else {
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

		publicKey, err = p.getPublicKey(ctx, destRsyncPod)
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
	}

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

		// Emit SyncSkipped event
		p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncSkipped,
			"Source PVC is not mounted by any pod, skipping sync")

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

	// Step 8: Push the public key to the agent pod (skip if using cached keys)
	if destRsyncPod.HasCachedKeys {
		log.WithFields(logrus.Fields{
			"agent_pod": agentPod.Name,
		}).Info(logging.LogTagStep8 + " Skipping public key push - agent already has authorized_keys from cached secret")
		log.Info(logging.LogTagStep8Complete + " Public key already provisioned on agent via cached secret")
	} else {
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
	}

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

	// Emit SSHConnected event
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSSHConnected,
		"SSH connectivity established to source agent on node %s", sourceNode)

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

		// Emit SyncFailed event
		p.RecordWarningEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncFailed,
			"Rsync operation failed: %v", err)

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

	// Emit LockReleased event
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonLockReleased,
		"Released sync lock for PVC")

	// Calculate duration and emit SyncCompleted event
	duration := time.Since(startTime)
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncCompleted,
		"PVC data sync completed successfully (duration: %s)", duration.Round(time.Second))

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
		"duration":         duration.Round(time.Second),
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

	// Check for cached rsync SSH keys if we know the source RemoteCluster
	var cachedKeySecretName string
	if p.SourceRemoteClusterName != "" {
		// Check if the cached rsync key secret exists in the destination namespace
		rsyncKeySecretName := ssh.GetRsyncKeySecretName(p.SourceRemoteClusterName)
		secret := &corev1.Secret{}
		err := p.DestinationClient.Get(ctx, client.ObjectKey{
			Name:      rsyncKeySecretName,
			Namespace: namespace,
		}, secret)
		if err == nil {
			// Cached keys exist, use them
			cachedKeySecretName = rsyncKeySecretName
			log.WithFields(logrus.Fields{
				"namespace":      namespace,
				"pvc_name":       pvcName,
				"remote_cluster": p.SourceRemoteClusterName,
				"secret_name":    rsyncKeySecretName,
			}).Info(logging.LogTagDetail + " Using cached rsync SSH keys from secret")
		} else {
			log.WithFields(logrus.Fields{
				"namespace":      namespace,
				"pvc_name":       pvcName,
				"remote_cluster": p.SourceRemoteClusterName,
				"secret_name":    rsyncKeySecretName,
				"error":          err,
			}).Info(logging.LogTagDetail + " Cached rsync SSH keys not found, will generate new keys")
		}
	} else {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug(logging.LogTagDetail + " SourceRemoteClusterName not set, will generate new SSH keys")
	}

	// Create rsync pod options
	opts := rsyncpod.RsyncPodOptions{
		Namespace:           namespace,
		PVCName:             pvcName,
		Type:                rsyncpod.DestinationPodType,
		SyncID:              syncID,
		ReplicationName:     fmt.Sprintf("pvc-sync-%s-%s", namespace, pvcName),
		DestinationInfo:     fmt.Sprintf("destination-%s-%s", namespace, pvcName),
		CachedKeySecretName: cachedKeySecretName, // Will be empty if no cached keys
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
		"namespace":       namespace,
		"pvc_name":        pvcName,
		"deployment":      rsyncDeployment.Name,
		"pod_name":        rsyncDeployment.PodName,
		"has_cached_keys": rsyncDeployment.HasCachedKeys,
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

// cleanupDaemonSetResources cleans up temporary resources used by the DaemonSet-based rsync workflow
func (p *PVCSyncer) cleanupDaemonSetResources(ctx context.Context, dsPod *rsyncpod.RsyncDaemonSetPod) {
	if dsPod == nil {
		log.Warn(logging.LogTagWarn + " Skipping cleanup, DaemonSet pod is nil")
		return
	}

	log.WithFields(logrus.Fields{
		"pod_name": dsPod.PodName,
		"node":     dsPod.NodeName,
	}).Info(logging.LogTagDetail + " Cleaning up DaemonSet sync resources")

	if err := dsPod.Cleanup(ctx); err != nil {
		log.WithFields(logrus.Fields{
			"pod_name": dsPod.PodName,
			"error":    err,
		}).Warn(logging.LogTagWarn + " Failed to cleanup DaemonSet sync resources, will continue anyway")
	}
}

// findRsyncDaemonSetPod finds an existing DaemonSet pod for the rsync operation.
// This method:
// 1. Finds the node where the destination PVC is (or should be) mounted
// 2. Finds the DaemonSet pod on that node
// 3. Resolves the destination path using the hybrid approach (kubelet path or TempPod)
func (p *PVCSyncer) findRsyncDaemonSetPod(ctx context.Context, destNamespace, destPVCName string) (*rsyncpod.RsyncDaemonSetPod, error) {
	log.WithFields(logrus.Fields{
		"namespace": destNamespace,
		"pvc_name":  destPVCName,
	}).Info(logging.LogTagDetail + " Finding DaemonSet pod for rsync operation")

	if p.RsyncDaemonSet == nil {
		return nil, fmt.Errorf("RsyncDaemonSet is not initialized")
	}

	// Step 1: Find the node where the destination PVC should be written
	// First, try to find an existing node where the PVC is already mounted
	destNode, err := p.FindPVCNode(ctx, p.DestinationClient, destNamespace, destPVCName)
	if err != nil {
		// If PVC is not mounted anywhere, we need to pick a node
		// The DaemonSet ResolveDestinationPath will create a placeholder pod if needed
		log.WithFields(logrus.Fields{
			"namespace": destNamespace,
			"pvc_name":  destPVCName,
			"error":     err,
		}).Info(logging.LogTagDetail + " Destination PVC not mounted, will select any available DaemonSet pod")

		// Get any ready DaemonSet pod as the starting point
		ds, dsErr := p.DestinationK8sClient.AppsV1().DaemonSets(p.RsyncDaemonSet.Namespace).Get(ctx, p.RsyncDaemonSet.Name, metav1.GetOptions{})
		if dsErr != nil {
			return nil, fmt.Errorf("failed to get rsync DaemonSet: %w", dsErr)
		}

		if ds.Status.NumberReady == 0 {
			return nil, fmt.Errorf("no ready rsync DaemonSet pods available")
		}

		// List DaemonSet pods and pick the first ready one
		pods, err := p.DestinationK8sClient.CoreV1().Pods(p.RsyncDaemonSet.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", rsyncpod.RsyncDaemonSetLabelKey, rsyncpod.RsyncDaemonSetLabelValue),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list rsync DaemonSet pods: %w", err)
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				destNode = pod.Spec.NodeName
				break
			}
		}

		if destNode == "" {
			return nil, fmt.Errorf("no running rsync DaemonSet pods found")
		}
	}

	log.WithFields(logrus.Fields{
		"namespace": destNamespace,
		"pvc_name":  destPVCName,
		"node":      destNode,
	}).Info(logging.LogTagDetail + " Found target node for destination PVC")

	// Step 2: Find the DaemonSet pod on that node
	dsPod, err := p.RsyncDaemonSet.FindPodOnNode(ctx, destNode)
	if err != nil {
		return nil, fmt.Errorf("failed to find DaemonSet pod on node %s: %w", destNode, err)
	}

	log.WithFields(logrus.Fields{
		"namespace": destNamespace,
		"pvc_name":  destPVCName,
		"node":      destNode,
		"pod":       dsPod.Name,
	}).Info(logging.LogTagDetail + " Found DaemonSet pod on target node")

	// Step 3: Resolve the destination path using hybrid approach
	destPath, cleanup, err := p.RsyncDaemonSet.ResolveDestinationPath(ctx, destNode, destNamespace, destPVCName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve destination path for PVC %s/%s: %w", destNamespace, destPVCName, err)
	}

	log.WithFields(logrus.Fields{
		"namespace": destNamespace,
		"pvc_name":  destPVCName,
		"node":      destNode,
		"dest_path": destPath,
	}).Info(logging.LogTagDetail + " Resolved destination path for PVC")

	// Create and return the DaemonSet pod wrapper
	return rsyncpod.NewRsyncDaemonSetPod(p.DestinationK8sClient, dsPod, destPVCName, destNamespace, destPath, cleanup), nil
}

// RsyncWorkflowWithDaemonSet performs the rsync workflow using the DaemonSet-based pod pool.
// This eliminates the 1-5 minute pod startup overhead by using pre-existing rsync pods.
func (p *PVCSyncer) RsyncWorkflowWithDaemonSet(ctx context.Context, sourceNamespace, sourcePVCName, destNamespace, destPVCName string) error {
	// Track start time for duration calculation
	startTime := time.Now()

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
		"mode":             "daemonset",
	}).Info(logging.LogTagInfo + " Starting rsync workflow with DaemonSet pool (fast path)")

	// Emit SyncStarted event
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncStarted,
		"Starting PVC data sync to %s/%s (DaemonSet mode)", destNamespace, destPVCName)

	// Set the namespaces in the PVCSyncer
	p.SourceNamespace = sourceNamespace
	p.DestinationNamespace = destNamespace

	// Track resources for cleanup
	var (
		lockAcquired bool
		dsPod        *rsyncpod.RsyncDaemonSetPod
	)

	// Deferred function for panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.WithFields(logrus.Fields{
				"source_namespace": sourceNamespace,
				"source_pvc":       sourcePVCName,
				"panic":            r,
			}).Error(logging.LogTagError + " Panic during DaemonSet rsync workflow")

			// Clean up resources if any
			if dsPod != nil {
				p.cleanupDaemonSetResources(ctx, dsPod)
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

		p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncSkipped,
			"PVC is locked by %s, skipping sync", lockInfo.ControllerPodName)
		return nil
	}

	lockAcquired = true
	log.Info(logging.LogTagStep0Complete + " Lock acquired on source PVC")

	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonLockAcquired,
		"Acquired sync lock for PVC")

	// Ensure DaemonSet is deployed (idempotent - creates if not exists, updates if needed)
	log.Info(logging.LogTagInfo + " Ensuring rsync DaemonSet is deployed")
	if p.RsyncDaemonSet == nil {
		log.Error(logging.LogTagError + " RsyncDaemonSet is not initialized")
		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock after DaemonSet init error")
			}
		}
		return fmt.Errorf("RsyncDaemonSet is not initialized, cannot use DaemonSet mode")
	}

	if err := p.RsyncDaemonSet.Deploy(ctx); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error(logging.LogTagError + " Failed to deploy rsync DaemonSet")

		p.RecordWarningEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncFailed,
			"Failed to deploy rsync DaemonSet: %v", err)

		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock after DaemonSet deployment error")
			}
		}
		return fmt.Errorf("failed to deploy rsync DaemonSet: %v", err)
	}
	log.Info(logging.LogTagInfo + " Rsync DaemonSet deployed/verified successfully")

	// Step 1: Find DaemonSet pod (FAST - no deployment creation)
	log.WithFields(logrus.Fields{
		"dest_namespace": destNamespace,
		"dest_pvc":       destPVCName,
	}).Info(logging.LogTagStep1 + " Finding rsync DaemonSet pod (fast path - no deployment)")

	dsPod, err = p.findRsyncDaemonSetPod(ctx, destNamespace, destPVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"dest_namespace": destNamespace,
			"dest_pvc":       destPVCName,
			"error":          err,
		}).Error(logging.LogTagError + " Failed to find DaemonSet pod")

		p.RecordWarningEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncFailed,
			"Failed to find DaemonSet rsync pod: %v", err)

		if lockAcquired {
			if relErr := p.ReleasePVCLock(ctx, sourceNamespace, sourcePVCName); relErr != nil {
				log.WithFields(logrus.Fields{
					"source_namespace": sourceNamespace,
					"source_pvc":       sourcePVCName,
					"error":            relErr,
				}).Warn(logging.LogTagWarn + " Failed to release lock on source PVC after failure")
			}
		}
		return fmt.Errorf("failed to find DaemonSet pod: %v", err)
	}
	log.WithFields(logrus.Fields{
		"pod_name":  dsPod.PodName,
		"node":      dsPod.NodeName,
		"dest_path": dsPod.DestinationPath,
	}).Info(logging.LogTagStep1Complete + " Found DaemonSet pod (skipped deployment creation)")

	// DaemonSet pods have pre-provisioned SSH keys - skip steps 2-3
	log.Info(logging.LogTagStep2 + " Skipping SSH key generation - DaemonSet pods have pre-mounted keys")
	log.Info(logging.LogTagStep2Complete + " SSH keys already mounted from secret")
	log.Info(logging.LogTagStep3 + " Skipping public key retrieval - using cached keys")
	log.Info(logging.LogTagStep3Complete + " Public key already provisioned on agent")

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

		p.cleanupDaemonSetResources(ctx, dsPod)
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

		p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncSkipped,
			"Source PVC is not mounted by any pod, skipping sync")

		p.cleanupDaemonSetResources(ctx, dsPod)
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

	// Step 5: Find the node where the source PVC is mounted
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
	}).Info(logging.LogTagStep5 + " Finding node where source PVC is mounted")

	sourceNode, err := p.FindPVCNode(ctx, p.SourceClient, sourceNamespace, sourcePVCName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNamespace,
			"source_pvc":       sourcePVCName,
			"error":            err,
		}).Error(logging.LogTagError + " Failed to find node where source PVC is mounted")

		p.cleanupDaemonSetResources(ctx, dsPod)
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

	// Step 6: Find the DR-Syncer-Agent running on that node
	log.WithFields(logrus.Fields{
		"node": sourceNode,
	}).Info(logging.LogTagStep6 + " Finding DR-Syncer-Agent on node")

	agentPod, nodeIP, err := p.FindAgentPod(ctx, sourceNode)
	if err != nil {
		log.WithFields(logrus.Fields{
			"node":  sourceNode,
			"error": err,
		}).Error(logging.LogTagError + " Failed to find DR-Syncer-Agent")

		p.cleanupDaemonSetResources(ctx, dsPod)
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

	// Step 7: Find the mount path for the source PVC
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

		p.cleanupDaemonSetResources(ctx, dsPod)
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

	// DaemonSet pods have pre-provisioned SSH keys - skip step 8 (push public key)
	log.WithFields(logrus.Fields{
		"agent_pod": agentPod.Name,
	}).Info(logging.LogTagStep8 + " Skipping public key push - agent already has authorized_keys from cached secret")
	log.Info(logging.LogTagStep8Complete + " Public key already provisioned on agent via cached secret")

	// Step 9: Test SSH connectivity
	log.WithFields(logrus.Fields{
		"dest_pod": dsPod.PodName,
		"node_ip":  nodeIP,
	}).Info(logging.LogTagStep9 + " Testing SSH connectivity")

	// Create a temporary RsyncDeployment wrapper for SSH testing
	// This is needed because TestSSHConnectivity expects an RsyncDeployment
	tempDeployment := &rsyncpod.RsyncDeployment{
		Name:          dsPod.Name,
		Namespace:     dsPod.Namespace,
		PodName:       dsPod.PodName,
		PVCName:       dsPod.PVCName,
		HasCachedKeys: true,
	}

	err = p.TestSSHConnectivity(ctx, tempDeployment, nodeIP, 2222, p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"dest_pod": dsPod.PodName,
			"node_ip":  nodeIP,
			"error":    err,
		}).Error(logging.LogTagError + " Failed to test SSH connectivity")

		p.cleanupDaemonSetResources(ctx, dsPod)
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

	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSSHConnected,
		"SSH connectivity established to source agent on node %s", sourceNode)

	// Step 10: Run rsync command using the DaemonSet pod and kubelet path
	log.WithFields(logrus.Fields{
		"dest_pod":   dsPod.PodName,
		"node_ip":    nodeIP,
		"mount_path": mountPath,
		"dest_path":  dsPod.DestinationPath,
	}).Info(logging.LogTagStep10 + " Running rsync command with kubelet destination path")

	if err := p.performRsyncWithDaemonSet(ctx, dsPod, nodeIP, mountPath); err != nil {
		log.WithFields(logrus.Fields{
			"dest_pod":   dsPod.PodName,
			"node_ip":    nodeIP,
			"mount_path": mountPath,
			"dest_path":  dsPod.DestinationPath,
			"error":      err,
		}).Error(logging.LogTagError + " Failed to perform rsync")

		p.RecordWarningEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncFailed,
			"Rsync operation failed: %v", err)

		p.cleanupDaemonSetResources(ctx, dsPod)
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

		p.cleanupDaemonSetResources(ctx, dsPod)
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

	// Step 12: Clean up temporary resources (not the DaemonSet pod itself)
	log.WithFields(logrus.Fields{
		"dest_pod": dsPod.PodName,
	}).Info(logging.LogTagStep12 + " Cleaning up temporary resources")

	p.cleanupDaemonSetResources(ctx, dsPod)
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
	}
	log.Info(logging.LogTagStep13Complete + " Lock released on source PVC")

	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonLockReleased,
		"Released sync lock for PVC")

	// Calculate duration and emit SyncCompleted event
	duration := time.Since(startTime)
	p.RecordNormalEvent(ctx, sourceNamespace, sourcePVCName, EventReasonSyncCompleted,
		"PVC data sync completed successfully via DaemonSet pool (duration: %s)", duration.Round(time.Second))

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNamespace,
		"source_pvc":       sourcePVCName,
		"dest_namespace":   destNamespace,
		"dest_pvc":         destPVCName,
		"duration":         duration.Round(time.Second),
		"mode":             "daemonset",
	}).Info(logging.LogTagComplete + " Rsync workflow completed successfully (DaemonSet fast path)")

	return nil
}

// performRsyncWithDaemonSet executes rsync using a DaemonSet pod with kubelet path destination
func (p *PVCSyncer) performRsyncWithDaemonSet(ctx context.Context, dsPod *rsyncpod.RsyncDaemonSetPod, nodeIP, sourcePath string) error {
	log.WithFields(logrus.Fields{
		"pod_name":    dsPod.PodName,
		"node_ip":     nodeIP,
		"source_path": sourcePath,
		"dest_path":   dsPod.DestinationPath,
	}).Info(logging.LogTagDetail + " Executing rsync with DaemonSet pod")

	// Create a temporary RsyncDeployment wrapper for performRsync
	tempDeployment := &rsyncpod.RsyncDeployment{
		Name:          dsPod.Name,
		Namespace:     dsPod.Namespace,
		PodName:       dsPod.PodName,
		PVCName:       dsPod.PVCName,
		HasCachedKeys: true,
	}

	// Call the existing performRsync but we need to pass the destination path
	// Store the destination path in context for performRsync to use
	dsCtx := context.WithValue(ctx, daemonSetDestPathKey, dsPod.DestinationPath)

	return p.performRsync(dsCtx, tempDeployment, nodeIP, sourcePath)
}

// daemonSetDestPathKeyType is the type for the DaemonSet destination path context key
type daemonSetDestPathKeyType string

// daemonSetDestPathKey is the context key for storing the DaemonSet destination path
const daemonSetDestPathKey daemonSetDestPathKeyType = "daemonSetDestPath"

// GetDaemonSetDestPath retrieves the DaemonSet destination path from context
func GetDaemonSetDestPath(ctx context.Context) (string, bool) {
	path, ok := ctx.Value(daemonSetDestPathKey).(string)
	return path, ok
}
