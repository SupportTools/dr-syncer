package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// CompleteReplicationPVCSyncLegacy completes a PVC sync operation for a namespace mapping (legacy implementation)
func (p *PVCSyncer) CompleteReplicationPVCSyncLegacy(ctx context.Context, repl *drv1alpha1.NamespaceMapping, syncID string) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"sync_id":          syncID,
	}).Info("Completing PVC sync operation (legacy)")

	// Update the replication status with the sync completion
	now := metav1.NewTime(time.Now())

	// Create a patch to update the status
	patch := []byte(fmt.Sprintf(`{"status":{"pvcSyncStatus":{"phase":"Completed","message":"PVC sync completed successfully","lastSyncTime":"%s"}}}`, now.Format(time.RFC3339)))

	// Apply the patch
	if err := p.DestinationClient.Status().Patch(ctx, repl, client.RawPatch(types.MergePatchType, patch)); err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"error":            err,
		}).Error("Failed to update namespace mapping status")
		return fmt.Errorf("failed to update namespace mapping status: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"sync_id":          syncID,
	}).Info("Successfully completed PVC sync operation (legacy)")

	return nil
}

// ScheduleNextPVCSyncLegacy schedules the next PVC sync for a namespace mapping (legacy implementation)
func (p *PVCSyncer) ScheduleNextPVCSyncLegacy(ctx context.Context, repl *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
	}).Info("Scheduling next PVC sync (legacy)")

	// Calculate the next sync time based on the replication schedule
	var nextSyncTime metav1.Time

	// For now, we'll just schedule it for 24 hours from now
	nextSyncTime = metav1.NewTime(time.Now().Add(24 * time.Hour))

	// Create a patch to update the status
	patch := []byte(fmt.Sprintf(`{"status":{"pvcSyncStatus":{"nextSyncTime":"%s"}}}`, nextSyncTime.Format(time.RFC3339)))

	// Apply the patch
	if err := p.DestinationClient.Status().Patch(ctx, repl, client.RawPatch(types.MergePatchType, patch)); err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"error":            err,
		}).Error("Failed to update namespace mapping status with next sync time")
		return fmt.Errorf("failed to update namespace mapping status: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"next_sync_at":     nextSyncTime,
	}).Info("Successfully scheduled next PVC sync (legacy)")

	return nil
}
