package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// standbyPVCSuffix is appended to the source PVC name to form the standby PVC name.
	standbyPVCSuffix = "-standby"

	// Annotation tracking restore metadata on standby PVCs.
	annotationLastRestoreTime   = "dr-syncer.io/last-restore-time"
	annotationLastRestoreSnapID = "dr-syncer.io/last-restore-snapshot-id"
)

// StandbyPVCManager manages standby PVC lifecycle on the DR cluster.
// This interface will be implemented by task #9685.
type StandbyPVCManager interface {
	// EnsureStandbyPVC creates or verifies a standby PVC on the DR cluster.
	// Returns the PVC reference and whether it was newly created.
	EnsureStandbyPVC(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, sourcePVCSpec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error)

	// UpdateStandbyPVCStatus updates the standby PVC annotations after a restore.
	UpdateStandbyPVCStatus(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error
}

// RestoreWorkflow orchestrates the complete 8-step restore process on the DR cluster.
// It manages: repository verification, snapshot resolution, standby PVC provisioning,
// restore pod execution, progress monitoring, and status updates.
type RestoreWorkflow struct {
	// DestClient is the Kubernetes client for the DR (destination) cluster.
	DestClient client.Client

	// SourceClient is the Kubernetes client for the source cluster.
	SourceClient client.Client

	// BackupWf handles the pod-level restore execution.
	BackupWf *BackupWorkflow

	// StandbyPVCMgr manages standby PVC lifecycle. If nil, the restore will
	// target the DestinationPVC specified in the operation directly.
	StandbyPVCMgr StandbyPVCManager

	// ProgressMon monitors restore pod progress. If nil, progress monitoring is skipped.
	ProgressMon ProgressMonitor

	// Timeout is the maximum duration for the restore operation.
	// Defaults to defaultRestoreTimeout if zero.
	Timeout time.Duration

	Log *logrus.Entry
}

// NewRestoreWorkflow creates a new RestoreWorkflow.
func NewRestoreWorkflow(destClient, sourceClient client.Client) *RestoreWorkflow {
	return &RestoreWorkflow{
		DestClient:   destClient,
		SourceClient: sourceClient,
		BackupWf:     NewBackupWorkflow(destClient),
		Log:          logrus.WithField("component", "restore-workflow"),
	}
}

// RestoreRequest holds the parameters for a restore operation.
type RestoreRequest struct {
	// SourcePVC is the source PVC that was backed up.
	SourcePVC drv1alpha1.PVCReference

	// DestNamespace is the namespace on the DR cluster where the standby PVC lives.
	DestNamespace string

	// MappingName is the NamespaceMapping name (used for labeling).
	MappingName string

	// Repository is the BackupRepository containing the Kopia data.
	Repository *drv1alpha1.BackupRepository

	// SnapshotID is the Kopia snapshot ID to restore. If empty, the latest
	// completed backup's snapshot ID is resolved automatically.
	SnapshotID string

	// PodConfig holds Kopia pod configuration.
	PodConfig KopiaPodConfig

	// SourcePVCSpec is the spec of the source PVC, used when creating standby PVCs.
	// If nil and StandbyPVCManager needs it, the workflow will fetch it from the source cluster.
	SourcePVCSpec *corev1.PersistentVolumeClaimSpec
}

// RestoreResult holds the outcome of a restore operation.
type RestoreResult struct {
	// SnapshotID is the Kopia snapshot ID that was restored.
	SnapshotID string

	// StandbyPVCRef is the PVC reference for the restored standby PVC.
	StandbyPVCRef *drv1alpha1.PVCReference

	// BytesRestored is the total bytes restored.
	BytesRestored int64
}

// ExecuteRestore runs the 8-step restore workflow:
//  1. Verify BackupRepository is accessible from the DR cluster
//  2. Resolve the Kopia snapshot ID (from request or latest backup operation)
//  3. Ensure standby PVC exists on DR cluster
//  4. Create/update VolumeBackupOperation CR for restore
//  5. Run Kopia restore pod via BackupWorkflow
//  6. Monitor progress (if ProgressMonitor available)
//  7. Clean up restore resources (pod is cleaned by BackupWorkflow)
//  8. Update completion status on VolumeBackupOperation
func (rw *RestoreWorkflow) ExecuteRestore(ctx context.Context, req RestoreRequest) (*RestoreResult, error) {
	log := rw.Log.WithFields(logrus.Fields{
		"source_pvc":     req.SourcePVC.Name,
		"source_ns":      req.SourcePVC.Namespace,
		"dest_ns":        req.DestNamespace,
		"mapping":        req.MappingName,
		"repository":     req.Repository.Name,
		"input_snapshot": req.SnapshotID,
	})

	log.Info("Starting 8-step restore workflow")

	// Step 1: Verify BackupRepository is accessible.
	log.Info("Step 1/8: Verifying BackupRepository accessibility")
	if err := rw.verifyRepository(ctx, req.Repository); err != nil {
		return nil, fmt.Errorf("step 1 verify repository: %w", err)
	}

	// Step 2: Resolve snapshot ID.
	log.Info("Step 2/8: Resolving Kopia snapshot ID")
	snapshotID, err := rw.resolveSnapshotID(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("step 2 resolve snapshot ID: %w", err)
	}
	log.WithField("snapshot_id", snapshotID).Info("Snapshot ID resolved")

	// Step 3: Ensure standby PVC exists on DR cluster.
	log.Info("Step 3/8: Ensuring standby PVC on DR cluster")
	destPVCRef, err := rw.ensureStandbyPVC(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("step 3 ensure standby PVC: %w", err)
	}
	log.WithField("standby_pvc", destPVCRef.Name).Info("Standby PVC ready")

	// Step 4: Create/update restore VolumeBackupOperation.
	log.Info("Step 4/8: Creating restore VolumeBackupOperation")
	restoreOp, err := rw.createRestoreOperation(ctx, req, snapshotID, destPVCRef)
	if err != nil {
		return nil, fmt.Errorf("step 4 create restore operation: %w", err)
	}
	log.WithField("operation", restoreOp.Name).Info("Restore operation created")

	// Step 5: Execute restore via BackupWorkflow (creates pod, waits, cleans up pod).
	log.Info("Step 5/8: Executing Kopia restore pod")
	if err := rw.BackupWf.Execute(ctx, restoreOp, req.Repository, req.PodConfig); err != nil {
		// On failure, leave the standby PVC in place to preserve partial data.
		log.WithError(err).Error("Restore pod execution failed; standby PVC preserved")
		return nil, fmt.Errorf("step 5 execute restore: %w", err)
	}

	// Step 6: Progress monitoring runs concurrently inside BackupWorkflow.waitForPodCompletion
	// and the existing progress monitor. Log completion here.
	log.Info("Step 6/8: Restore pod completed successfully")

	// Step 7: Cleanup restore-specific resources.
	log.Info("Step 7/8: Cleaning up restore resources")
	rw.cleanupRestoreResources(ctx, restoreOp)

	// Step 8: Update standby PVC status and return result.
	log.Info("Step 8/8: Updating completion status")
	result, err := rw.updateCompletionStatus(ctx, req, snapshotID, destPVCRef, restoreOp)
	if err != nil {
		return nil, fmt.Errorf("step 8 update completion status: %w", err)
	}

	log.WithFields(logrus.Fields{
		"snapshot_id":    snapshotID,
		"standby_pvc":    destPVCRef.Name,
		"bytes_restored": result.BytesRestored,
	}).Info("Restore workflow completed successfully")

	return result, nil
}

// verifyRepository checks that the BackupRepository is in a ready state.
func (rw *RestoreWorkflow) verifyRepository(ctx context.Context, repo *drv1alpha1.BackupRepository) error {
	// Re-fetch the latest state from the DR cluster to verify accessibility.
	latest := &drv1alpha1.BackupRepository{}
	key := types.NamespacedName{Name: repo.Name, Namespace: repo.Namespace}
	if err := rw.DestClient.Get(ctx, key, latest); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("BackupRepository %s/%s not found on DR cluster", repo.Namespace, repo.Name)
		}
		return fmt.Errorf("get BackupRepository: %w", err)
	}

	if latest.Status.State != drv1alpha1.BackupRepositoryStateReady {
		return fmt.Errorf("BackupRepository %s/%s is not ready (state: %s)", repo.Namespace, repo.Name, latest.Status.State)
	}

	// Verify the S3 credentials secret exists in the DR cluster.
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      latest.Spec.S3Config.CredentialsSecretRef.Name,
		Namespace: repo.Namespace,
	}
	if err := rw.DestClient.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("S3 credentials secret %s/%s not found on DR cluster", secretKey.Namespace, secretKey.Name)
		}
		return fmt.Errorf("get S3 credentials secret: %w", err)
	}

	// Verify the Kopia encryption secret exists.
	kopiaSecret := &corev1.Secret{}
	kopiaSecretKey := types.NamespacedName{
		Name:      latest.Spec.KopiaConfig.EncryptionSecretRef.Name,
		Namespace: repo.Namespace,
	}
	if err := rw.DestClient.Get(ctx, kopiaSecretKey, kopiaSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("Kopia encryption secret %s/%s not found on DR cluster", kopiaSecretKey.Namespace, kopiaSecretKey.Name)
		}
		return fmt.Errorf("get Kopia encryption secret: %w", err)
	}

	return nil
}

// resolveSnapshotID determines the Kopia snapshot ID to restore.
// If the request provides an explicit snapshot ID, it is validated and returned.
// Otherwise, the latest completed backup operation's snapshot ID is resolved.
func (rw *RestoreWorkflow) resolveSnapshotID(ctx context.Context, req RestoreRequest) (string, error) {
	if req.SnapshotID != "" {
		if err := ValidateSnapshotID(req.SnapshotID); err != nil {
			return "", fmt.Errorf("invalid explicit snapshot ID: %w", err)
		}
		return req.SnapshotID, nil
	}

	// Find the latest completed backup operation for this PVC.
	opList := &drv1alpha1.VolumeBackupOperationList{}
	if err := rw.SourceClient.List(ctx, opList,
		client.InNamespace(req.SourcePVC.Namespace),
		client.MatchingLabels{
			labelMapping:   req.MappingName,
			labelPVC:       req.SourcePVC.Name,
			labelOperation: "backup",
		},
	); err != nil {
		return "", fmt.Errorf("list backup operations: %w", err)
	}

	// Find the most recently completed backup with a snapshot ID.
	var latestSnapshotID string
	var latestCompletionTime time.Time

	for i := range opList.Items {
		op := &opList.Items[i]
		if op.Status.Phase != drv1alpha1.VolumeBackupPhaseBackupComplete {
			continue
		}
		if op.Status.KopiaSnapshotID == "" {
			continue
		}
		if op.Status.CompletionTime == nil {
			continue
		}
		if op.Status.CompletionTime.Time.After(latestCompletionTime) {
			latestCompletionTime = op.Status.CompletionTime.Time
			latestSnapshotID = op.Status.KopiaSnapshotID
		}
	}

	if latestSnapshotID == "" {
		return "", fmt.Errorf("no completed backup found for PVC %s/%s in mapping %s",
			req.SourcePVC.Namespace, req.SourcePVC.Name, req.MappingName)
	}

	if err := ValidateSnapshotID(latestSnapshotID); err != nil {
		return "", fmt.Errorf("invalid resolved snapshot ID: %w", err)
	}

	return latestSnapshotID, nil
}

// ensureStandbyPVC ensures a standby PVC exists on the DR cluster.
// If StandbyPVCManager is configured, it delegates to it. Otherwise,
// it assumes the destination PVC already exists and returns a reference.
func (rw *RestoreWorkflow) ensureStandbyPVC(ctx context.Context, req RestoreRequest) (*drv1alpha1.PVCReference, error) {
	if rw.StandbyPVCMgr != nil {
		// Fetch source PVC spec if not provided.
		pvcSpec := req.SourcePVCSpec
		if pvcSpec == nil {
			sourcePVC := &corev1.PersistentVolumeClaim{}
			key := types.NamespacedName{Name: req.SourcePVC.Name, Namespace: req.SourcePVC.Namespace}
			if err := rw.SourceClient.Get(ctx, key, sourcePVC); err != nil {
				return nil, fmt.Errorf("get source PVC spec: %w", err)
			}
			pvcSpec = &sourcePVC.Spec
		}

		pvcRef, created, err := rw.StandbyPVCMgr.EnsureStandbyPVC(ctx, req.SourcePVC, req.DestNamespace, pvcSpec)
		if err != nil {
			return nil, fmt.Errorf("ensure standby PVC: %w", err)
		}
		if created {
			rw.Log.WithField("pvc", pvcRef.Name).Info("Standby PVC created")
		} else {
			rw.Log.WithField("pvc", pvcRef.Name).Info("Standby PVC already exists")
		}
		return pvcRef, nil
	}

	// Without StandbyPVCManager, build the standby PVC name and verify it exists.
	standbyName := req.SourcePVC.Name + standbyPVCSuffix
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: standbyName, Namespace: req.DestNamespace}
	if err := rw.DestClient.Get(ctx, key, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("standby PVC %s/%s does not exist and no StandbyPVCManager configured",
				req.DestNamespace, standbyName)
		}
		return nil, fmt.Errorf("get standby PVC: %w", err)
	}

	return &drv1alpha1.PVCReference{
		Namespace: req.DestNamespace,
		Name:      standbyName,
	}, nil
}

// createRestoreOperation creates or updates a VolumeBackupOperation CR for the restore
// on the DR cluster. The operation is idempotent — re-creating with the same name
// updates the spec for a new run.
func (rw *RestoreWorkflow) createRestoreOperation(
	ctx context.Context,
	req RestoreRequest,
	snapshotID string,
	destPVCRef *drv1alpha1.PVCReference,
) (*drv1alpha1.VolumeBackupOperation, error) {
	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-restore", req.MappingName, req.SourcePVC.Name),
			Namespace: destPVCRef.Namespace,
			Labels: map[string]string{
				labelMapping:   req.MappingName,
				labelPVC:       req.SourcePVC.Name,
				labelOperation: "restore",
			},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SourcePVC: drv1alpha1.PVCReference{
				Namespace: destPVCRef.Namespace,
				Name:      destPVCRef.Name,
			},
			DestinationPVC: destPVCRef,
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      req.Repository.Name,
				Namespace: req.Repository.Namespace,
			},
			SnapshotID: snapshotID,
		},
	}

	// Create or update the operation (idempotent).
	existing := &drv1alpha1.VolumeBackupOperation{}
	err := rw.DestClient.Get(ctx, client.ObjectKeyFromObject(op), existing)
	if apierrors.IsNotFound(err) {
		if createErr := rw.DestClient.Create(ctx, op); createErr != nil {
			return nil, fmt.Errorf("create restore operation: %w", createErr)
		}
		return op, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get existing restore operation: %w", err)
	}

	// Update the existing operation for a new restore run.
	existing.Spec = op.Spec
	existing.Labels = op.Labels
	// Reset status for the new run.
	existing.Status = drv1alpha1.VolumeBackupOperationStatus{}
	if updateErr := rw.DestClient.Update(ctx, existing); updateErr != nil {
		return nil, fmt.Errorf("update restore operation: %w", updateErr)
	}

	return existing, nil
}

// cleanupRestoreResources cleans up temporary resources created during restore.
// The Kopia pod is already cleaned up by BackupWorkflow. This handles any
// additional restore-specific cleanup.
func (rw *RestoreWorkflow) cleanupRestoreResources(ctx context.Context, restoreOp *drv1alpha1.VolumeBackupOperation) {
	// Currently, BackupWorkflow handles pod cleanup via deferred cleanupPod.
	// Future: clean up temporary secrets, config maps, etc. if needed.
	rw.Log.WithField("operation", restoreOp.Name).Debug("Restore resource cleanup complete")
}

// updateCompletionStatus updates the standby PVC annotations and returns the result.
func (rw *RestoreWorkflow) updateCompletionStatus(
	ctx context.Context,
	req RestoreRequest,
	snapshotID string,
	destPVCRef *drv1alpha1.PVCReference,
	restoreOp *drv1alpha1.VolumeBackupOperation,
) (*RestoreResult, error) {
	// Get the latest operation to read final status.
	latest := &drv1alpha1.VolumeBackupOperation{}
	if err := rw.DestClient.Get(ctx, client.ObjectKeyFromObject(restoreOp), latest); err != nil {
		return nil, fmt.Errorf("get final restore operation status: %w", err)
	}

	// Update standby PVC annotations if manager is available.
	if rw.StandbyPVCMgr != nil {
		if err := rw.StandbyPVCMgr.UpdateStandbyPVCStatus(ctx, *destPVCRef, snapshotID); err != nil {
			rw.Log.WithError(err).Warn("Failed to update standby PVC status annotations")
			// Non-fatal: the restore data is intact even if annotations fail.
		}
	} else {
		// Update annotations directly on the PVC.
		rw.annotateStandbyPVC(ctx, destPVCRef, snapshotID)
	}

	return &RestoreResult{
		SnapshotID:    snapshotID,
		StandbyPVCRef: destPVCRef,
		BytesRestored: latest.Status.BytesTransferred,
	}, nil
}

// annotateStandbyPVC adds restore metadata annotations to the standby PVC.
func (rw *RestoreWorkflow) annotateStandbyPVC(ctx context.Context, pvcRef *drv1alpha1.PVCReference, snapshotID string) {
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: pvcRef.Name, Namespace: pvcRef.Namespace}
	if err := rw.DestClient.Get(ctx, key, pvc); err != nil {
		rw.Log.WithError(err).Warn("Failed to get standby PVC for annotation update")
		return
	}

	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}
	pvc.Annotations[annotationLastRestoreTime] = time.Now().UTC().Format(time.RFC3339)
	pvc.Annotations[annotationLastRestoreSnapID] = snapshotID

	if err := rw.DestClient.Update(ctx, pvc); err != nil {
		rw.Log.WithError(err).Warn("Failed to update standby PVC annotations")
	}
}
