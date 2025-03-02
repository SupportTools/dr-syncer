package replication

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// RsyncReplicationController is responsible for PVC replication operations
type RsyncReplicationController struct {
	// PVCSyncer handles the PVC sync operations
	syncer *PVCSyncer
	
	// RsyncController handles the rsync deployment and replication process
	rsyncController *RsyncController
}

// NewRsyncReplicationController creates a new rsync replication controller
func NewRsyncReplicationController(sourceClient, destClient client.Client, sourceConfig, destConfig *rest.Config) (*RsyncReplicationController, error) {
	// Initialize the PVC syncer
	syncer, err := NewPVCSyncer(sourceClient, destClient, sourceConfig, destConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize PVC syncer: %v", err)
	}
	
	// Initialize the rsync controller
	rsyncController := NewRsyncController(syncer)
	
	return &RsyncReplicationController{
		syncer:          syncer,
		rsyncController: rsyncController,
	}, nil
}

// ReplicatePVC initiates PVC replication from source to destination cluster
func (c *RsyncReplicationController) ReplicatePVC(ctx context.Context, sourceNS, destNS, pvcName string) error {
	// Generate a unique sync ID for this replication
	syncID := generateSyncID()
	
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] Starting PVC replication")
	
	// Call the rsync controller to handle the replication
	err := c.rsyncController.SyncReplication(ctx, sourceNS, destNS, pvcName, syncID)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] PVC replication failed")
		return err
	}
	
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] PVC replication completed successfully")
	
	return nil
}

// ProcessNamespaceMapping processes a namespace mapping and replicates PVCs
func (c *RsyncReplicationController) ProcessNamespaceMapping(ctx context.Context, namespacemapping *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": namespacemapping.Name,
		"source_namespace": namespacemapping.Spec.SourceNamespace,
		"dest_namespace":   namespacemapping.Spec.DestinationNamespace,
	}).Info("[DR-SYNC] Processing namespace mapping")
	
	// Set the source and destination namespaces on the syncer
	c.syncer.SourceNamespace = namespacemapping.Spec.SourceNamespace
	c.syncer.DestinationNamespace = namespacemapping.Spec.DestinationNamespace
	
	// Set up empty label selector for PVCs - will match all PVCs in the namespace
	pvcSelector := client.MatchingLabels{}
	
	// Check if we have PVC filtering configuration
	if namespacemapping.Spec.PVCConfig != nil {
		// For now, we're not implementing any filtering based on PVCConfig
		// This will match all PVCs in the source namespace
		log.WithFields(logrus.Fields{
			"namespacemapping": namespacemapping.Name,
		}).Info("[DR-SYNC] PVC configuration detected")
	}
	
	// Get PVCs matching the selector
	pvcNames, err := c.syncer.GetPVCsToSync(ctx, c.syncer.SourceNamespace, c.syncer.DestinationNamespace, pvcSelector)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to get PVCs for namespace mapping")
		return err
	}
	
	log.WithFields(logrus.Fields{
		"namespacemapping": namespacemapping.Name,
		"pvc_count":        len(pvcNames),
	}).Info("[DR-SYNC] Found PVCs for replication")
	
	// Process each PVC
	for _, pvcName := range pvcNames {
		err := c.ReplicatePVC(ctx, c.syncer.SourceNamespace, c.syncer.DestinationNamespace, pvcName)
		if err != nil {
			log.WithFields(logrus.Fields{
				"pvc_name": pvcName,
				"error":    err,
			}).Error("[DR-SYNC-ERROR] Failed to replicate PVC")
			// Continue to next PVC, don't abort the whole process
			continue
		}
	}
	
	// Update namespace mapping status
	err = c.syncer.CompleteNamespaceMappingPVCSync(ctx, namespacemapping, generateSyncID())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to update namespace mapping status")
		return err
	}
	
	// Schedule next sync if using scheduled mode
	if namespacemapping.Spec.ReplicationMode == ScheduledMode {
		err = c.syncer.ScheduleNextPVCSync(ctx, namespacemapping)
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Error("[DR-SYNC-ERROR] Failed to schedule next PVC sync")
			return err
		}
	}
	
	log.WithFields(logrus.Fields{
		"namespacemapping": namespacemapping.Name,
		"source_namespace": namespacemapping.Spec.SourceNamespace,
		"dest_namespace":   namespacemapping.Spec.DestinationNamespace,
	}).Info("[DR-SYNC] Namespace mapping processing completed")
	
	return nil
}

// generateSyncID generates a unique sync ID
func generateSyncID() string {
	timestamp := metav1.Now().Format("20060102-150405")
	return fmt.Sprintf("%s", timestamp)
}

// GetRuntimeObject returns the wrapped runtime object
func (c *RsyncReplicationController) GetRuntimeObject(obj runtime.Object) runtime.Object {
	return obj
}
