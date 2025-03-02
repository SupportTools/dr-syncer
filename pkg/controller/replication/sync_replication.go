package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
)

// SyncReplication orchestrates the PVC replication process between source and destination clusters
func (p *PVCSyncer) SyncReplication(ctx context.Context, sourceNS, destNS, pvcName string, syncID string) error {
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] Starting PVC replication process")

	// Update context with PVCSyncer for executeCommandInPod to use
	ctx = context.WithValue(ctx, "pvcsync", p)
	
	// Step 1: Check if source PVC is currently mounted
	log.Info("[DR-SYNC] Step 1: Checking if source PVC is mounted")
	hasAttachments, err := p.HasVolumeAttachments(ctx, sourceNS, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to check if source PVC has attachments")
		return fmt.Errorf("failed to check if source PVC has attachments: %v", err)
	}

	if !hasAttachments {
		log.Info("[DR-SYNC] Source PVC is not mounted, skipping rsync")
		return nil
	}

	// Step 2: Find the node where the PVC is mounted
	log.Info("[DR-SYNC] Step 2: Finding node where source PVC is mounted")
	nodeName, err := p.FindPVCNode(ctx, p.SourceClient, sourceNS, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find node where source PVC is mounted")
		return fmt.Errorf("failed to find node where source PVC is mounted: %v", err)
	}

	// Step 3: Find the DR-Syncer-Agent running on that node
	log.Info("[DR-SYNC] Step 3: Finding DR-Syncer-Agent on node")
	agentPod, agentIP, err := p.FindAgentPod(ctx, nodeName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find DR-Syncer-Agent on node")
		return fmt.Errorf("failed to find DR-Syncer-Agent on node: %v", err)
	}

	// Step 4: Find the mount path for the PVC
	log.Info("[DR-SYNC] Step 4: Finding mount path for source PVC")
	mountPath, err := p.FindPVCMountPath(ctx, sourceNS, pvcName, agentPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find mount path for source PVC")
		return fmt.Errorf("failed to find mount path for source PVC: %v", err)
	}

	// Step 5: Create rsync pod on the destination cluster and namespace
	log.Info("[DR-SYNC] Step 5: Deploying rsync pod on destination cluster")
	rsyncMgr, err := rsyncpod.NewManager(p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to create rsync pod manager")
		return fmt.Errorf("failed to create rsync pod manager: %v", err)
	}

	opts := rsyncpod.RsyncPodOptions{
		Namespace:       destNS,
		PVCName:         pvcName,
		Type:            rsyncpod.DestinationPodType,
		SyncID:          syncID,
		ReplicationName: pvcName,
	}

	// Create the rsync pod that mounts the destination PVC
	rsyncPod, err := rsyncMgr.CreateRsyncPod(ctx, opts)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to create rsync pod on destination cluster")
		return fmt.Errorf("failed to create rsync pod on destination cluster: %v", err)
	}

	// Ensure we clean up the rsync pod at the end
	defer func() {
		log.Info("[DR-SYNC] Cleaning up rsync pod")
		cleanupErr := rsyncPod.Cleanup(ctx, 30)
		if cleanupErr != nil {
			log.WithFields(logrus.Fields{
				"error": cleanupErr,
			}).Error("[DR-SYNC-ERROR] Failed to cleanup rsync pod")
		}
	}()

	// Step 6: Wait for the rsync pod to be ready
	log.Info("[DR-SYNC] Step 6: Waiting for rsync pod to become ready")
	err = rsyncPod.WaitForPodReady(ctx, 5*time.Minute)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Rsync pod failed to become ready")
		return fmt.Errorf("rsync pod failed to become ready: %v", err)
	}

	// Step 7: Generate SSH keys in the rsync pod
	log.Info("[DR-SYNC] Step 7: Generating SSH keys in rsync pod")
	err = rsyncPod.GenerateSSHKeys(ctx)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys in rsync pod")
		return fmt.Errorf("failed to generate SSH keys in rsync pod: %v", err)
	}

	// Step 8: Get the public SSH key from the rsync pod
	log.Info("[DR-SYNC] Step 8: Getting public SSH key from rsync pod")
	publicKey, err := rsyncPod.GetPublicKey(ctx)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to get public SSH key from rsync pod")
		return fmt.Errorf("failed to get public SSH key from rsync pod: %v", err)
	}

	// Step 9: Push the public SSH key to the agent pod
	log.Info("[DR-SYNC] Step 9: Pushing public SSH key to agent pod")
	trackingInfo := fmt.Sprintf("dr-syncer-%s", syncID)
	err = p.PushPublicKeyToAgent(ctx, agentPod, publicKey, trackingInfo)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to push public SSH key to agent pod")
		return fmt.Errorf("failed to push public SSH key to agent pod: %v", err)
	}

	// Step 10: Test SSH connectivity from rsync pod to agent pod
	log.Info("[DR-SYNC] Step 10: Testing SSH connectivity")
	// We need to adapt our TestSSHConnectivity method to work with the RsyncPod type
	// Create a temporary RsyncDeployment struct with needed fields
	tempDeployment := &rsyncpod.RsyncDeployment{
		Name:      rsyncPod.Name,
		Namespace: rsyncPod.Namespace,
		PodName:   rsyncPod.Name,
	}
	
	err = p.TestSSHConnectivity(ctx, tempDeployment, agentIP, 2222)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] SSH connectivity test failed")
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}

	// Step 11: Run the rsync command and monitor status
	log.Info("[DR-SYNC] Step 11: Running rsync command")
	err = p.performRsync(ctx, tempDeployment, agentIP, mountPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Rsync command failed")
		return fmt.Errorf("rsync command failed: %v", err)
	}

	// Step 12: Update source PVC annotations
	log.Info("[DR-SYNC] Step 12: Updating source PVC annotations")
	// Store current values to be restored after sync
	p.SourceNamespace = sourceNS
	p.DestinationNamespace = destNS
	err = p.UpdateSourcePVCAnnotations(ctx, sourceNS, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to update source PVC annotations")
		return fmt.Errorf("failed to update source PVC annotations: %v", err)
	}

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] PVC replication process completed successfully")

	return nil
}

// These functions are already defined in create_temp_pod.go
