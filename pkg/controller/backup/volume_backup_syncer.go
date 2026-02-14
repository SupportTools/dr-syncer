package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controller/replication"
)

const (
	// Default timeout for a single backup or restore operation.
	defaultOperationTimeout = 30 * time.Minute

	// Polling interval when waiting for operation completion.
	defaultPollInterval = 10 * time.Second

	// Annotation keys for backup sync status on NamespaceMapping.
	annotationLastBackupSyncTime   = "dr-syncer.io/last-backup-sync-time"
	annotationLastBackupSyncStatus = "dr-syncer.io/last-backup-sync-status"
	annotationLastBackupSyncError  = "dr-syncer.io/last-backup-sync-error"

	// Label keys applied to VolumeBackupOperation CRs.
	labelMapping   = "dr-syncer.io/mapping"
	labelPVC       = "dr-syncer.io/pvc"
	labelOperation = "dr-syncer.io/operation"
)

// PVCSyncResult holds the outcome of syncing a single PVC via backup.
type PVCSyncResult struct {
	PVCName    string
	SnapshotID string
	Err        error
}

// VolumeBackupSyncer orchestrates backup-based PVC data synchronization.
// It creates VolumeBackupOperation CRs on source and destination clusters
// and polls their status to track progress.
type VolumeBackupSyncer struct {
	SourceClient      client.Client
	DestinationClient client.Client

	// PollInterval controls how frequently operation status is checked.
	// Defaults to defaultPollInterval if zero.
	PollInterval time.Duration

	Log *logrus.Entry
}

// NewVolumeBackupSyncer creates a new VolumeBackupSyncer.
func NewVolumeBackupSyncer(sourceClient, destinationClient client.Client) *VolumeBackupSyncer {
	return &VolumeBackupSyncer{
		SourceClient:      sourceClient,
		DestinationClient: destinationClient,
		Log:               logrus.WithField("component", "volume-backup-syncer"),
	}
}

// SyncPVCsWithBackup orchestrates backup-based sync for all PVCs in a NamespaceMapping.
// It creates VolumeBackupOperation CRs for each PVC that needs backup, waits for
// completion, then creates restore operations on the destination cluster.
// Partial failures are tolerated — individual PVC failures don't block others.
func (vbs *VolumeBackupSyncer) SyncPVCsWithBackup(
	ctx context.Context,
	mapping *drv1alpha1.NamespaceMapping,
	pvcs []corev1.PersistentVolumeClaim,
	backupConfig *drv1alpha1.BackupConfig,
) []PVCSyncResult {
	log := vbs.Log.WithFields(logrus.Fields{
		"mapping":   mapping.Name,
		"namespace": mapping.Namespace,
		"pvc_count": len(pvcs),
	})

	if backupConfig == nil || !backupConfig.Enabled {
		log.Warn("SyncPVCsWithBackup called but backup config is not enabled")
		return nil
	}

	if backupConfig.BackupRepositoryRef == nil {
		log.Error("BackupConfig.BackupRepositoryRef is nil, cannot perform backup sync")
		vbs.updateMappingAnnotations(ctx, mapping, "Failed", fmt.Errorf("backupRepositoryRef is required"))
		return nil
	}

	// Resolve the BackupRepository to verify it's ready.
	repo, err := vbs.getBackupRepository(ctx, backupConfig.BackupRepositoryRef, mapping.Namespace)
	if err != nil {
		log.WithError(err).Error("Failed to resolve BackupRepository")
		vbs.updateMappingAnnotations(ctx, mapping, "Failed", err)
		return nil
	}

	if repo.Status.State != drv1alpha1.BackupRepositoryStateReady {
		err := fmt.Errorf("BackupRepository %s/%s is not ready (state: %s)", repo.Namespace, repo.Name, repo.Status.State)
		log.WithError(err).Error("Repository not ready")
		vbs.updateMappingAnnotations(ctx, mapping, "Failed", err)
		return nil
	}

	log.Info("Starting backup-based PVC sync")
	vbs.updateMappingAnnotations(ctx, mapping, "Running", nil)

	results := make([]PVCSyncResult, 0, len(pvcs))
	var successCount, failCount int

	for i := range pvcs {
		pvc := &pvcs[i]
		result := vbs.syncSinglePVC(ctx, mapping, pvc, backupConfig, repo)
		results = append(results, result)

		if result.Err != nil {
			failCount++
			log.WithFields(logrus.Fields{
				"pvc":   pvc.Name,
				"error": result.Err,
			}).Error("PVC backup sync failed")
		} else {
			successCount++
			log.WithFields(logrus.Fields{
				"pvc":         pvc.Name,
				"snapshot_id": result.SnapshotID,
			}).Info("PVC backup sync completed")
		}
	}

	// Update mapping status based on overall results.
	if failCount == 0 {
		vbs.updateMappingAnnotations(ctx, mapping, "Completed", nil)
	} else if successCount > 0 {
		vbs.updateMappingAnnotations(ctx, mapping, "PartialFailure",
			fmt.Errorf("%d of %d PVCs failed", failCount, len(pvcs)))
	} else {
		vbs.updateMappingAnnotations(ctx, mapping, "Failed",
			fmt.Errorf("all %d PVCs failed", failCount))
	}

	log.WithFields(logrus.Fields{
		"success": successCount,
		"failed":  failCount,
		"total":   len(pvcs),
	}).Info("Backup-based PVC sync batch complete")

	return results
}

// syncSinglePVC performs the backup→restore cycle for one PVC, respecting concurrency limits.
func (vbs *VolumeBackupSyncer) syncSinglePVC(
	ctx context.Context,
	mapping *drv1alpha1.NamespaceMapping,
	pvc *corev1.PersistentVolumeClaim,
	backupConfig *drv1alpha1.BackupConfig,
	repo *drv1alpha1.BackupRepository,
) PVCSyncResult {
	log := vbs.Log.WithFields(logrus.Fields{
		"mapping": mapping.Name,
		"pvc":     pvc.Name,
	})

	// Acquire global concurrency slot.
	gcm := replication.GetGlobalConcurrencyManager()
	if gcm != nil {
		if err := gcm.Acquire(ctx, pvc.Namespace, pvc.Name); err != nil {
			return PVCSyncResult{PVCName: pvc.Name, Err: fmt.Errorf("concurrency acquire: %w", err)}
		}
		defer gcm.Release(pvc.Namespace, pvc.Name)
	}

	// Determine operation timeout.
	timeout := defaultOperationTimeout
	if mapping.Spec.PVCConfig != nil &&
		mapping.Spec.PVCConfig.DataSyncConfig != nil &&
		mapping.Spec.PVCConfig.DataSyncConfig.Timeout != nil {
		timeout = mapping.Spec.PVCConfig.DataSyncConfig.Timeout.Duration
	}

	// Step 1: Create backup operation on source cluster.
	backupOp := vbs.buildBackupOperation(mapping, pvc, repo, backupConfig)
	log.Info("Creating backup operation on source cluster")

	if err := vbs.createOrUpdateOperation(ctx, vbs.SourceClient, backupOp); err != nil {
		return PVCSyncResult{PVCName: pvc.Name, Err: fmt.Errorf("create backup operation: %w", err)}
	}

	// Step 2: Wait for backup to complete.
	snapshotID, err := vbs.waitForOperationPhase(
		ctx, vbs.SourceClient, backupOp,
		drv1alpha1.VolumeBackupPhaseBackupComplete, timeout,
	)
	if err != nil {
		return PVCSyncResult{PVCName: pvc.Name, Err: fmt.Errorf("backup operation: %w", err)}
	}

	log.WithField("snapshot_id", snapshotID).Info("Backup operation completed")

	// Step 3: Create restore operation on destination cluster.
	destPVCRef := &drv1alpha1.PVCReference{
		Namespace: mapping.Spec.DestinationNamespace,
		Name:      pvc.Name,
	}
	restoreOp := vbs.buildRestoreOperation(mapping, destPVCRef, repo, backupConfig, snapshotID)
	log.Info("Creating restore operation on destination cluster")

	if err := vbs.createOrUpdateOperation(ctx, vbs.DestinationClient, restoreOp); err != nil {
		return PVCSyncResult{PVCName: pvc.Name, SnapshotID: snapshotID, Err: fmt.Errorf("create restore operation: %w", err)}
	}

	// Step 4: Wait for restore to complete.
	_, err = vbs.waitForOperationPhase(
		ctx, vbs.DestinationClient, restoreOp,
		drv1alpha1.VolumeBackupPhaseRestoreComplete, timeout,
	)
	if err != nil {
		return PVCSyncResult{PVCName: pvc.Name, SnapshotID: snapshotID, Err: fmt.Errorf("restore operation: %w", err)}
	}

	return PVCSyncResult{PVCName: pvc.Name, SnapshotID: snapshotID}
}

// buildBackupOperation creates a VolumeBackupOperation spec for a backup.
func (vbs *VolumeBackupSyncer) buildBackupOperation(
	mapping *drv1alpha1.NamespaceMapping,
	pvc *corev1.PersistentVolumeClaim,
	repo *drv1alpha1.BackupRepository,
	backupConfig *drv1alpha1.BackupConfig,
) *drv1alpha1.VolumeBackupOperation {
	return &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-backup", mapping.Name, pvc.Name),
			Namespace: pvc.Namespace,
			Labels: map[string]string{
				labelMapping:   mapping.Name,
				labelPVC:       pvc.Name,
				labelOperation: "backup",
			},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC: drv1alpha1.PVCReference{
				Namespace: pvc.Namespace,
				Name:      pvc.Name,
			},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      repo.Name,
				Namespace: repo.Namespace,
			},
			DataAccessStrategy: backupConfig.DataAccessStrategy,
		},
	}
}

// buildRestoreOperation creates a VolumeBackupOperation spec for a restore.
func (vbs *VolumeBackupSyncer) buildRestoreOperation(
	mapping *drv1alpha1.NamespaceMapping,
	destPVC *drv1alpha1.PVCReference,
	repo *drv1alpha1.BackupRepository,
	backupConfig *drv1alpha1.BackupConfig,
	snapshotID string,
) *drv1alpha1.VolumeBackupOperation {
	return &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-restore", mapping.Name, destPVC.Name),
			Namespace: destPVC.Namespace,
			Labels: map[string]string{
				labelMapping:   mapping.Name,
				labelPVC:       destPVC.Name,
				labelOperation: "restore",
			},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SourcePVC: drv1alpha1.PVCReference{
				Namespace: destPVC.Namespace,
				Name:      destPVC.Name,
			},
			DestinationPVC: destPVC,
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      repo.Name,
				Namespace: repo.Namespace,
			},
			SnapshotID:         snapshotID,
			DataAccessStrategy: backupConfig.DataAccessStrategy,
		},
	}
}

// createOrUpdateOperation creates a VolumeBackupOperation CR or updates it if it already exists.
func (vbs *VolumeBackupSyncer) createOrUpdateOperation(
	ctx context.Context,
	k8sClient client.Client,
	op *drv1alpha1.VolumeBackupOperation,
) error {
	existing := &drv1alpha1.VolumeBackupOperation{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(op), existing)
	if apierrors.IsNotFound(err) {
		return k8sClient.Create(ctx, op)
	}
	if err != nil {
		return err
	}

	// Update spec of existing operation (resets it for a new run).
	existing.Spec = op.Spec
	existing.Labels = op.Labels
	return k8sClient.Update(ctx, existing)
}

// waitForOperationPhase polls a VolumeBackupOperation until it reaches the target phase,
// fails, or times out. Returns the KopiaSnapshotID on success.
func (vbs *VolumeBackupSyncer) waitForOperationPhase(
	ctx context.Context,
	k8sClient client.Client,
	op *drv1alpha1.VolumeBackupOperation,
	targetPhase drv1alpha1.VolumeBackupOperationPhase,
	timeout time.Duration,
) (string, error) {
	log := vbs.Log.WithFields(logrus.Fields{
		"operation":    op.Name,
		"namespace":    op.Namespace,
		"target_phase": targetPhase,
	})

	pollInterval := vbs.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	deadline := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", fmt.Errorf("operation %s/%s timed out after %v waiting for phase %s",
				op.Namespace, op.Name, timeout, targetPhase)
		case <-ticker.C:
			latest := &drv1alpha1.VolumeBackupOperation{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(op), latest); err != nil {
				if apierrors.IsNotFound(err) {
					return "", fmt.Errorf("operation %s/%s was deleted", op.Namespace, op.Name)
				}
				log.WithError(err).Warn("Failed to poll operation status, will retry")
				continue
			}

			switch latest.Status.Phase {
			case targetPhase:
				return latest.Status.KopiaSnapshotID, nil
			case drv1alpha1.VolumeBackupPhaseFailed:
				return "", fmt.Errorf("operation failed: %s", latest.Status.ErrorMessage)
			default:
				log.WithFields(logrus.Fields{
					"phase":    latest.Status.Phase,
					"progress": latest.Status.ProgressPercentage,
				}).Debug("Operation in progress")
			}
		}
	}
}

// getBackupRepository fetches the BackupRepository referenced by the config.
func (vbs *VolumeBackupSyncer) getBackupRepository(
	ctx context.Context,
	ref *drv1alpha1.SecretReference,
	defaultNamespace string,
) (*drv1alpha1.BackupRepository, error) {
	ns := ref.Namespace
	if ns == "" {
		ns = defaultNamespace
	}

	repo := &drv1alpha1.BackupRepository{}
	key := client.ObjectKey{Name: ref.Name, Namespace: ns}
	if err := vbs.SourceClient.Get(ctx, key, repo); err != nil {
		return nil, fmt.Errorf("failed to get BackupRepository %s/%s: %w", ns, ref.Name, err)
	}
	return repo, nil
}

// updateMappingAnnotations updates the NamespaceMapping with backup sync progress.
// Follows the Get-then-Update pattern to avoid etcd conflicts.
func (vbs *VolumeBackupSyncer) updateMappingAnnotations(
	ctx context.Context,
	mapping *drv1alpha1.NamespaceMapping,
	status string,
	syncErr error,
) {
	log := vbs.Log.WithFields(logrus.Fields{
		"mapping": mapping.Name,
		"status":  status,
	})

	// Get the latest version to avoid conflicts.
	var latest drv1alpha1.NamespaceMapping
	if err := vbs.DestinationClient.Get(ctx, client.ObjectKey{
		Name:      mapping.Name,
		Namespace: mapping.Namespace,
	}, &latest); err != nil {
		log.WithError(err).Warn("Failed to get latest NamespaceMapping for annotation update")
		return
	}

	if latest.Annotations == nil {
		latest.Annotations = make(map[string]string)
	}

	now := metav1.Now()
	latest.Annotations[annotationLastBackupSyncTime] = now.Format(time.RFC3339)
	latest.Annotations[annotationLastBackupSyncStatus] = status

	if syncErr != nil {
		latest.Annotations[annotationLastBackupSyncError] = syncErr.Error()
	} else {
		delete(latest.Annotations, annotationLastBackupSyncError)
	}

	if err := vbs.DestinationClient.Update(ctx, &latest); err != nil {
		log.WithError(err).Warn("Failed to update NamespaceMapping annotations")
	}
}
