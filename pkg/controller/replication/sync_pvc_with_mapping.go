package replication

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// SyncPVCWithNamespaceMapping synchronizes a PVC from source to destination using the specified options and namespace mapping
func (p *PVCSyncer) SyncPVCWithNamespaceMapping(ctx context.Context, mapping *drv1alpha1.NamespaceMapping, opts PVCSyncOptions) error {
	log.WithFields(map[string]interface{}{
		"source_namespace":      opts.SourceNamespace,
		"source_pvc":            opts.SourcePVC.Name,
		"destination_namespace": opts.DestinationNamespace,
		"destination_pvc":       opts.DestinationPVC.Name,
		"mapping":               mapping.Name,
	}).Info("Syncing PVC with namespace mapping")

	// Set the source and destination namespaces in the PVCSyncer
	p.SourceNamespace = opts.SourceNamespace
	p.DestinationNamespace = opts.DestinationNamespace

	// Check if source PVC is mounted
	hasMounts, err := p.HasVolumeAttachments(ctx, opts.SourceNamespace, opts.SourcePVC.Name)
	if err != nil {
		return fmt.Errorf("failed to check if source PVC is mounted: %v", err)
	}

	if !hasMounts {
		log.WithFields(map[string]interface{}{
			"source_namespace": opts.SourceNamespace,
			"source_pvc":       opts.SourcePVC.Name,
		}).Info("Source PVC is not mounted, skipping rsync")
		return nil
	}

	// Validate the PVC sync operation
	if err := p.ValidatePVCSync(ctx,
		opts.SourcePVC.Name, opts.SourceNamespace,
		opts.DestinationPVC.Name, opts.DestinationNamespace); err != nil {
		return fmt.Errorf("validation failed: %v", err)
	}

	// Log sync progress
	p.LogSyncProgress(ctx,
		opts.SourcePVC.Name, opts.SourceNamespace,
		opts.DestinationPVC.Name, opts.DestinationNamespace,
		"Started", "PVC sync started")

	// Perform the rsync workflow
	err = p.RsyncWorkflow(ctx,
		opts.SourceNamespace, opts.SourcePVC.Name,
		opts.DestinationNamespace, opts.DestinationPVC.Name)
	if err != nil {
		p.LogSyncProgress(ctx,
			opts.SourcePVC.Name, opts.SourceNamespace,
			opts.DestinationPVC.Name, opts.DestinationNamespace,
			"Failed", fmt.Sprintf("PVC sync failed: %v", err))
		return fmt.Errorf("rsync workflow failed: %v", err)
	}

	// Update namespace mapping status
	syncID := fmt.Sprintf("%s-%s-%s",
		opts.SourceNamespace, opts.SourcePVC.Name,
		opts.DestinationNamespace)

	if err := p.CompleteNamespaceMappingPVCSync(ctx, mapping, syncID); err != nil {
		log.WithFields(map[string]interface{}{
			"namespacemapping": mapping.Name,
			"error":            err,
		}).Warning("Failed to update namespace mapping status")
	}

	// Schedule next sync if using scheduled mode
	if string(mapping.Spec.ReplicationMode) == string(ScheduledMode) {
		if err := p.ScheduleNextPVCSync(ctx, mapping); err != nil {
			log.WithFields(map[string]interface{}{
				"namespacemapping": mapping.Name,
				"error":            err,
			}).Warning("Failed to schedule next PVC sync")
		}
	}

	// Log sync progress
	p.LogSyncProgress(ctx,
		opts.SourcePVC.Name, opts.SourceNamespace,
		opts.DestinationPVC.Name, opts.DestinationNamespace,
		"Completed", "PVC sync completed successfully")

	log.WithFields(map[string]interface{}{
		"source_namespace":      opts.SourceNamespace,
		"source_pvc":            opts.SourcePVC.Name,
		"destination_namespace": opts.DestinationNamespace,
		"destination_pvc":       opts.DestinationPVC.Name,
	}).Info("PVC sync completed successfully")

	return nil
}
