package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controller/replication"
)

const (
	// defaultPodTimeout is the maximum time to wait for a Kopia pod to complete.
	defaultPodTimeout = 30 * time.Minute

	// DefaultPodPollInterval is how often to check pod status.
	DefaultPodPollInterval = 5 * time.Second

	// snapshotTimeout is how long to wait for a VolumeSnapshot to become ready.
	snapshotTimeout = 5 * time.Minute
)

// BackupWorkflow executes the backup or restore workflow for a single VolumeBackupOperation.
// It manages the full lifecycle: data access setup (snapshot or live), Kopia pod execution,
// snapshot ID extraction, status updates, and cleanup.
type BackupWorkflow struct {
	Client          client.Client
	SnapshotManager *replication.SnapshotManager
	Log             *logrus.Entry
	PodPollInterval time.Duration
	podLogReader    PodLogReader
}

// NewBackupWorkflow creates a new BackupWorkflow.
func NewBackupWorkflow(k8sClient client.Client) *BackupWorkflow {
	return &BackupWorkflow{
		Client:          k8sClient,
		SnapshotManager: replication.NewSnapshotManager(k8sClient),
		Log:             logrus.WithField("component", "backup-workflow"),
		PodPollInterval: DefaultPodPollInterval,
	}
}

// Execute runs the appropriate workflow (backup or restore) for the given operation.
func (bw *BackupWorkflow) Execute(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation, repo *drv1alpha1.BackupRepository, podConfig KopiaPodConfig) error {
	log := bw.Log.WithFields(logrus.Fields{
		"operation": operation.Name,
		"namespace": operation.Namespace,
		"type":      operation.Spec.OperationType,
	})

	// Mark operation as started.
	now := metav1.Now()
	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhasePending
		status.StartTime = &now
		status.Message = "Operation starting"
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update start status: %w", err))
	}

	switch operation.Spec.OperationType {
	case drv1alpha1.OperationTypeBackup:
		log.Info("Starting backup workflow")
		return bw.executeBackup(ctx, operation, repo, podConfig)
	case drv1alpha1.OperationTypeRestore:
		log.Info("Starting restore workflow")
		return bw.executeRestore(ctx, operation, repo, podConfig)
	default:
		return bw.failOperation(ctx, operation, fmt.Errorf("unknown operation type: %s", operation.Spec.OperationType))
	}
}

// executeBackup runs the backup workflow, selecting between snapshot and live paths
// based on the DataAccessStrategy.
func (bw *BackupWorkflow) executeBackup(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation, repo *drv1alpha1.BackupRepository, podConfig KopiaPodConfig) error {
	log := bw.Log.WithFields(logrus.Fields{
		"operation": operation.Name,
		"pvc":       operation.Spec.SourcePVC.Name,
		"namespace": operation.Spec.SourcePVC.Namespace,
	})

	strategy, snapshotClassName, err := bw.resolveDataAccessStrategy(ctx, operation)
	if err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("resolve data access strategy: %w", err))
	}

	log.WithFields(logrus.Fields{
		"strategy":      strategy,
		"snapshotClass": snapshotClassName,
	}).Info("Resolved data access strategy")

	switch strategy {
	case drv1alpha1.DataAccessStrategySnapshot:
		return bw.executeSnapshotBackup(ctx, operation, repo, podConfig, snapshotClassName)
	case drv1alpha1.DataAccessStrategyLive:
		return bw.executeLiveBackup(ctx, operation, repo, podConfig)
	default:
		return bw.failOperation(ctx, operation, fmt.Errorf("invalid resolved strategy: %s", strategy))
	}
}

// resolveDataAccessStrategy determines the actual strategy to use.
// For "Auto", it checks CSI snapshot support and falls back to Live.
// For "Snapshot", it verifies CSI support and fails if unavailable.
// For "Live", it returns Live directly.
func (bw *BackupWorkflow) resolveDataAccessStrategy(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation) (drv1alpha1.DataAccessStrategy, string, error) {
	requested := operation.Spec.DataAccessStrategy
	if requested == "" {
		requested = drv1alpha1.DataAccessStrategyAuto
	}

	pvcRef := operation.Spec.SourcePVC

	switch requested {
	case drv1alpha1.DataAccessStrategyLive:
		return drv1alpha1.DataAccessStrategyLive, "", nil

	case drv1alpha1.DataAccessStrategySnapshot:
		canSnap, className, err := bw.SnapshotManager.CanSnapshot(ctx, pvcRef.Namespace, pvcRef.Name)
		if err != nil {
			return "", "", fmt.Errorf("check snapshot support: %w", err)
		}
		if !canSnap {
			return "", "", fmt.Errorf("snapshot strategy requested but PVC %s/%s does not support CSI snapshots", pvcRef.Namespace, pvcRef.Name)
		}
		return drv1alpha1.DataAccessStrategySnapshot, className, nil

	case drv1alpha1.DataAccessStrategyAuto:
		canSnap, className, err := bw.SnapshotManager.CanSnapshot(ctx, pvcRef.Namespace, pvcRef.Name)
		if err != nil {
			bw.Log.WithError(err).Warn("CSI snapshot check failed, falling back to live access")
			return drv1alpha1.DataAccessStrategyLive, "", nil
		}
		if canSnap {
			return drv1alpha1.DataAccessStrategySnapshot, className, nil
		}
		return drv1alpha1.DataAccessStrategyLive, "", nil

	default:
		return "", "", fmt.Errorf("unknown data access strategy: %s", requested)
	}
}

// executeSnapshotBackup implements the CSI snapshot backup path:
// 1. Create VolumeSnapshot of the source PVC
// 2. Wait for snapshot to be ready
// 3. Create temporary PVC from the snapshot
// 4. Build and create Kopia backup pod mounting the temp PVC
// 5. Wait for pod completion and extract snapshot ID
// 6. Cleanup snapshot + temp PVC
// 7. Update status with Kopia snapshot ID
func (bw *BackupWorkflow) executeSnapshotBackup(
	ctx context.Context,
	operation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	podConfig KopiaPodConfig,
	snapshotClassName string,
) error {
	log := bw.Log.WithField("operation", operation.Name)
	pvcRef := operation.Spec.SourcePVC

	// Track resources for deferred cleanup.
	var snapshotName string
	var tempPVCName string
	defer func() {
		bw.SnapshotManager.CleanupSnapshot(context.Background(), pvcRef.Namespace, snapshotName, tempPVCName)
	}()

	// Step 1: Create VolumeSnapshot.
	log.Info("Creating VolumeSnapshot for snapshot-based backup")
	snapshot, err := bw.SnapshotManager.CreateSnapshot(ctx, pvcRef.Namespace, pvcRef.Name, snapshotClassName)
	if err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("create snapshot: %w", err))
	}
	snapshotName = snapshot.Name

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseSnapshotCreated
		status.Message = fmt.Sprintf("VolumeSnapshot %s created, waiting for ready", snapshotName)
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update snapshot created status: %w", err))
	}

	// Step 2: Wait for snapshot to be ready.
	if err := bw.SnapshotManager.WaitForSnapshotReady(ctx, pvcRef.Namespace, snapshotName, snapshotTimeout); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("wait for snapshot ready: %w", err))
	}

	// Step 3: Create temp PVC from snapshot.
	log.Info("Creating temporary PVC from snapshot")
	originalPVC := &corev1.PersistentVolumeClaim{}
	if err := bw.Client.Get(ctx, types.NamespacedName{
		Namespace: pvcRef.Namespace,
		Name:      pvcRef.Name,
	}, originalPVC); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("get source PVC: %w", err))
	}

	tempPVC, err := bw.SnapshotManager.RestoreSnapshotToPVC(ctx, pvcRef.Namespace, snapshotName, originalPVC)
	if err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("restore snapshot to temp PVC: %w", err))
	}
	tempPVCName = tempPVC.Name

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseDataAccessReady
		status.Message = fmt.Sprintf("Temporary PVC %s ready from snapshot", tempPVCName)
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update data access ready status: %w", err))
	}

	// Step 4: Create a modified operation that points to the temp PVC for the backup pod.
	tempOperation := operation.DeepCopy()
	tempOperation.Spec.SourcePVC = drv1alpha1.PVCReference{
		Namespace: pvcRef.Namespace,
		Name:      tempPVCName,
	}

	// Step 5-7: Run Kopia pod and extract snapshot ID.
	return bw.runBackupPod(ctx, operation, tempOperation, repo, podConfig)
}

// executeLiveBackup implements the live-mount backup path:
// 1. Find the node where the PVC is currently mounted
// 2. Build Kopia backup pod pinned to that node
// 3. Wait for pod completion and extract snapshot ID
// 4. Update status with Kopia snapshot ID
func (bw *BackupWorkflow) executeLiveBackup(
	ctx context.Context,
	operation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	podConfig KopiaPodConfig,
) error {
	log := bw.Log.WithField("operation", operation.Name)
	pvcRef := operation.Spec.SourcePVC

	// Step 1: Find the node where the PVC is mounted.
	nodeName, err := bw.findPVCNode(ctx, pvcRef.Namespace, pvcRef.Name)
	if err != nil {
		// If we can't find the node, the PVC might not be mounted by any pod.
		// Proceed without node pinning — Kubernetes scheduling will handle it.
		log.WithError(err).Warn("Could not determine PVC node, proceeding without node affinity")
	} else {
		log.WithField("node", nodeName).Info("PVC mounted on node, pinning backup pod")
		podConfig.NodeName = nodeName
	}

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseDataAccessReady
		status.Message = "Live PVC access ready"
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update data access ready status: %w", err))
	}

	// Step 2-4: Run Kopia pod and extract snapshot ID.
	return bw.runBackupPod(ctx, operation, operation, repo, podConfig)
}

// runBackupPod creates the Kopia backup pod, waits for completion, extracts the snapshot ID,
// and updates the operation status. The statusOperation is used for status updates,
// while podOperation is used for building the pod spec (may differ if using temp PVC).
func (bw *BackupWorkflow) runBackupPod(
	ctx context.Context,
	statusOperation *drv1alpha1.VolumeBackupOperation,
	podOperation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	podConfig KopiaPodConfig,
) error {
	log := bw.Log.WithField("operation", statusOperation.Name)

	// Build the Kopia backup pod.
	pod := BuildBackupPod(podOperation, repo, podConfig)

	// Clean up pod on exit.
	defer func() {
		bw.cleanupPod(context.Background(), pod.Namespace, pod.Name)
	}()

	// Create the pod.
	log.WithField("pod", pod.Name).Info("Creating Kopia backup pod")
	if err := bw.Client.Create(ctx, pod); err != nil {
		return bw.failOperation(ctx, statusOperation, fmt.Errorf("create backup pod: %w", err))
	}

	if err := bw.updateStatus(ctx, statusOperation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseBackupInProgress
		status.Message = fmt.Sprintf("Kopia backup pod %s running", pod.Name)
	}); err != nil {
		return bw.failOperation(ctx, statusOperation, fmt.Errorf("update backup in progress status: %w", err))
	}

	// Wait for pod to complete.
	if err := bw.waitForPodCompletion(ctx, pod.Namespace, pod.Name, defaultPodTimeout); err != nil {
		return bw.failOperation(ctx, statusOperation, fmt.Errorf("backup pod failed: %w", err))
	}

	// Extract Kopia snapshot ID from pod logs.
	snapshotID, err := bw.extractSnapshotIDFromPodLogs(ctx, pod.Namespace, pod.Name)
	if err != nil {
		return bw.failOperation(ctx, statusOperation, fmt.Errorf("extract snapshot ID: %w", err))
	}

	log.WithField("snapshot_id", snapshotID).Info("Backup completed, snapshot ID extracted")

	// Update status with completion.
	now := metav1.Now()
	if err := bw.updateStatus(ctx, statusOperation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseBackupComplete
		status.KopiaSnapshotID = snapshotID
		status.CompletionTime = &now
		status.ProgressPercentage = 100
		status.Message = "Backup completed successfully"
	}); err != nil {
		return bw.failOperation(ctx, statusOperation, fmt.Errorf("update backup complete status: %w", err))
	}

	return nil
}

// executeRestore runs the restore workflow:
// 1. Build Kopia restore pod with destination PVC mounted
// 2. Wait for pod completion
// 3. Update status
func (bw *BackupWorkflow) executeRestore(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation, repo *drv1alpha1.BackupRepository, podConfig KopiaPodConfig) error {
	log := bw.Log.WithField("operation", operation.Name)

	if operation.Spec.SnapshotID == "" {
		return bw.failOperation(ctx, operation, fmt.Errorf("snapshotID is required for restore operations"))
	}

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseDataAccessReady
		status.Message = "Destination PVC ready for restore"
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update data access ready status: %w", err))
	}

	// Build the Kopia restore pod.
	pod := BuildRestorePod(operation, repo, podConfig)

	// Clean up pod on exit.
	defer func() {
		bw.cleanupPod(context.Background(), pod.Namespace, pod.Name)
	}()

	log.WithFields(logrus.Fields{
		"pod":         pod.Name,
		"snapshot_id": operation.Spec.SnapshotID,
	}).Info("Creating Kopia restore pod")
	if err := bw.Client.Create(ctx, pod); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("create restore pod: %w", err))
	}

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseRestoreInProgress
		status.Message = fmt.Sprintf("Kopia restore pod %s running", pod.Name)
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update restore in progress status: %w", err))
	}

	// Wait for pod to complete.
	if err := bw.waitForPodCompletion(ctx, pod.Namespace, pod.Name, defaultPodTimeout); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("restore pod failed: %w", err))
	}

	// Update status with completion.
	now := metav1.Now()
	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseRestoreComplete
		status.CompletionTime = &now
		status.ProgressPercentage = 100
		status.Message = "Restore completed successfully"
	}); err != nil {
		return bw.failOperation(ctx, operation, fmt.Errorf("update restore complete status: %w", err))
	}

	log.Info("Restore completed successfully")
	return nil
}

// findPVCNode finds which node a PVC is currently mounted on by checking pods
// that reference the PVC in the same namespace.
func (bw *BackupWorkflow) findPVCNode(ctx context.Context, namespace, pvcName string) (string, error) {
	podList := &corev1.PodList{}
	if err := bw.Client.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		return "", fmt.Errorf("list pods: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" {
			continue
		}
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvcName {
				return pod.Spec.NodeName, nil
			}
		}
	}

	return "", fmt.Errorf("no running pod found mounting PVC %s/%s", namespace, pvcName)
}

// waitForPodCompletion polls a pod until it reaches Succeeded, Failed, or the timeout expires.
func (bw *BackupWorkflow) waitForPodCompletion(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	pollInterval := bw.PodPollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPodPollInterval
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("pod %s/%s timed out after %v", namespace, podName, timeout)
		case <-ticker.C:
			pod := &corev1.Pod{}
			if err := bw.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
				if apierrors.IsNotFound(err) {
					return fmt.Errorf("pod %s/%s was deleted", namespace, podName)
				}
				bw.Log.WithError(err).Warn("Failed to poll pod status, will retry")
				continue
			}

			switch pod.Status.Phase {
			case corev1.PodSucceeded:
				return nil
			case corev1.PodFailed:
				msg := "pod failed"
				if len(pod.Status.ContainerStatuses) > 0 {
					cs := pod.Status.ContainerStatuses[0]
					if cs.State.Terminated != nil {
						msg = fmt.Sprintf("pod failed with exit code %d: %s", cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
					}
				}
				return fmt.Errorf("%s", msg)
			}
		}
	}
}

// kopiaSnapshotOutput represents the JSON output from `kopia snapshot create --json`.
type kopiaSnapshotOutput struct {
	RootEntry *struct {
		ObjID string `json:"obj"`
	} `json:"rootEntry"`
	ID          string `json:"id"`
	Description string `json:"description"`
}

// extractSnapshotIDFromPodLogs reads the Kopia pod logs and extracts the snapshot ID
// from the JSON output of `kopia snapshot create --json`.
func (bw *BackupWorkflow) extractSnapshotIDFromPodLogs(ctx context.Context, namespace, podName string) (string, error) {
	// First, try the termination message from the container status.
	pod := &corev1.Pod{}
	if err := bw.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
		return "", fmt.Errorf("get pod for log extraction: %w", err)
	}

	if len(pod.Status.ContainerStatuses) > 0 {
		cs := pod.Status.ContainerStatuses[0]
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			snapshotID, err := parseKopiaSnapshotID(cs.State.Terminated.Message)
			if err == nil && snapshotID != "" {
				return snapshotID, nil
			}
		}
	}

	// Fall back to the external pod log reader if available.
	if bw.podLogReader != nil {
		logs, err := bw.podLogReader.GetPodLogs(ctx, namespace, podName, "kopia")
		if err != nil {
			return "", fmt.Errorf("get pod logs: %w", err)
		}

		snapshotID, err := parseKopiaSnapshotID(logs)
		if err != nil {
			return "", fmt.Errorf("parse snapshot ID from logs: %w", err)
		}
		if snapshotID != "" {
			return snapshotID, nil
		}
	}

	return "", fmt.Errorf("no snapshot ID found in pod %s/%s output", namespace, podName)
}

// PodLogReader is an interface for reading pod logs. This allows tests to inject
// a mock log reader, and production code to provide a real Kubernetes client.
type PodLogReader interface {
	GetPodLogs(ctx context.Context, namespace, podName, containerName string) (string, error)
}

// SetPodLogReader sets an external log reader for extracting Kopia output.
// This is used when a clientset is available for reading pod logs directly.
func (bw *BackupWorkflow) SetPodLogReader(reader PodLogReader) {
	bw.podLogReader = reader
}

// parseKopiaSnapshotID extracts the snapshot ID from Kopia's JSON output.
// Kopia's `snapshot create --json` outputs a JSON object with the snapshot manifest.
func parseKopiaSnapshotID(output string) (string, error) {
	// Try to find the JSON object in the output.
	// Kopia may output progress lines before the final JSON.
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Try parsing as Kopia snapshot JSON.
		var snapshotOut kopiaSnapshotOutput
		if err := json.Unmarshal([]byte(line), &snapshotOut); err != nil {
			continue
		}

		// The snapshot ID is in the "id" field.
		if snapshotOut.ID != "" {
			return snapshotOut.ID, nil
		}

		// Fallback: check rootEntry.obj for the root object ID.
		if snapshotOut.RootEntry != nil && snapshotOut.RootEntry.ObjID != "" {
			return snapshotOut.RootEntry.ObjID, nil
		}
	}

	return "", fmt.Errorf("no valid Kopia snapshot JSON found in output")
}

// updateStatus performs a get-then-update on the VolumeBackupOperation status subresource.
// The mutate function is called to apply changes to the status before updating.
func (bw *BackupWorkflow) updateStatus(
	ctx context.Context,
	operation *drv1alpha1.VolumeBackupOperation,
	mutate func(*drv1alpha1.VolumeBackupOperationStatus),
) error {
	latest := &drv1alpha1.VolumeBackupOperation{}
	if err := bw.Client.Get(ctx, client.ObjectKeyFromObject(operation), latest); err != nil {
		return fmt.Errorf("get latest operation: %w", err)
	}

	mutate(&latest.Status)

	if err := bw.Client.Status().Update(ctx, latest); err != nil {
		return fmt.Errorf("update operation status: %w", err)
	}

	return nil
}

// failOperation marks the operation as Failed with the given error.
func (bw *BackupWorkflow) failOperation(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation, opErr error) error {
	bw.Log.WithFields(logrus.Fields{
		"operation": operation.Name,
		"error":     opErr,
	}).Error("Operation failed")

	now := metav1.Now()
	updateErr := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseFailed
		status.ErrorMessage = opErr.Error()
		status.CompletionTime = &now
		status.Message = "Operation failed"
	})
	if updateErr != nil {
		bw.Log.WithError(updateErr).Error("Failed to update operation status to Failed")
	}

	return opErr
}

// cleanupPod deletes a pod, logging errors but not returning them.
func (bw *BackupWorkflow) cleanupPod(ctx context.Context, namespace, podName string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
	}
	if err := bw.Client.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		bw.Log.WithFields(logrus.Fields{
			"pod":       podName,
			"namespace": namespace,
		}).WithError(err).Warn("Failed to cleanup Kopia pod")
	}
}
