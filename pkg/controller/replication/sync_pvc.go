package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// SyncPVC synchronizes a PVC from the source to the destination cluster
func (p *PVCSyncer) SyncPVC(ctx context.Context, namespace, name string, destNamespace string, mapping *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"source_namespace": namespace,
		"source_pvc":       name,
		"dest_namespace":   destNamespace,
	}).Info("Syncing PVC")

	// Check if both source and destination PVCs exist
	if err := p.ValidatePVCSync(ctx, name, namespace, name, destNamespace); err != nil {
		return fmt.Errorf("validation failed: %v", err)
	}

	// Log sync progress
	p.LogSyncProgress(ctx, name, namespace, name, destNamespace, "Started", "PVC sync started")

	// Perform the rsync workflow
	err := p.RsyncWorkflow(ctx, namespace, name, destNamespace, name)
	if err != nil {
		p.LogSyncProgress(ctx, name, namespace, name, destNamespace, "Failed", fmt.Sprintf("PVC sync failed: %v", err))
		return fmt.Errorf("rsync workflow failed: %v", err)
	}

	// Update namespace mapping status if provided
	if mapping != nil {
		// Generate a unique ID for this sync operation
		syncID := fmt.Sprintf("%s-%s-%s", namespace, name, time.Now().Format("20060102150405"))

		// Update namespace mapping status
		if err := p.CompleteNamespaceMappingPVCSync(ctx, mapping, syncID); err != nil {
			log.WithFields(logrus.Fields{
				"namespacemapping": mapping.Name,
				"error":            err,
			}).Warning("Failed to update namespace mapping status")
		}

		// Schedule next sync if using scheduled mode
		if string(mapping.Spec.ReplicationMode) == string(ScheduledMode) {
			if err := p.ScheduleNextPVCSync(ctx, mapping); err != nil {
				log.WithFields(logrus.Fields{
					"namespacemapping": mapping.Name,
					"error":            err,
				}).Warning("Failed to schedule next PVC sync")
			}
		}
	}

	// Log sync progress
	p.LogSyncProgress(ctx, name, namespace, name, destNamespace, "Completed", "PVC sync completed successfully")

	log.WithFields(logrus.Fields{
		"source_namespace": namespace,
		"source_pvc":       name,
		"dest_namespace":   destNamespace,
	}).Info("PVC sync completed successfully")

	return nil
}

// SyncPVCs synchronizes all PVCs matching the provided selector from the source to the destination namespace
func (p *PVCSyncer) SyncPVCs(ctx context.Context, sourceNS, destNS string, selector map[string]string, mapping *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
	}).Info("Syncing PVCs between namespaces")

	// Set the namespaces in the PVCSyncer
	p.SourceNamespace = sourceNS
	p.DestinationNamespace = destNS

	// Get list of PVCs to sync
	pvcNames, err := p.GetPVCsToSync(ctx, sourceNS, destNS, selector)
	if err != nil {
		return fmt.Errorf("failed to get PVCs to sync: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pvc_count": len(pvcNames),
	}).Info("Found PVCs to sync")

	// Sync each PVC
	for _, pvcName := range pvcNames {
		if err := p.SyncPVC(ctx, sourceNS, pvcName, destNS, mapping); err != nil {
			log.WithFields(logrus.Fields{
				"pvc_name": pvcName,
				"error":    err,
			}).Error("Failed to sync PVC")
			// Continue with other PVCs even if one fails
			continue
		}
	}

	return nil
}

// SchedulePVCSync schedules a PVC sync according to the mapping's schedule
func (p *PVCSyncer) SchedulePVCSync(ctx context.Context, mapping *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": mapping.Name,
	}).Info("Scheduling PVC sync")

	// Skip if not in scheduled mode
	if string(mapping.Spec.ReplicationMode) != string(ScheduledMode) {
		log.WithFields(logrus.Fields{
			"namespacemapping": mapping.Name,
			"mode":             mapping.Spec.ReplicationMode,
		}).Info("NamespaceMapping is not in scheduled mode, skipping")
		return nil
	}

	// Parse schedule
	schedule := mapping.Spec.Schedule
	if schedule == "" {
		schedule = "0 * * * *" // Default to hourly if not specified
	}

	// Calculate next run time
	// This is a simplified implementation - in a real controller you would use a cron parser
	nextRun := metav1.Now().Add(1 * time.Hour) // Simplified - just add one hour

	// Update the mapping status with the next sync time
	mapping.Status.NextSyncTime = &metav1.Time{Time: nextRun}

	log.WithFields(logrus.Fields{
		"namespacemapping": mapping.Name,
		"next_sync_time":   nextRun.Format(time.RFC3339),
	}).Info("Scheduled next PVC sync")

	return nil
}
