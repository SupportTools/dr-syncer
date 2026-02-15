package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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

	opType := string(operation.Spec.OperationType)
	dataAccess := string(operation.Spec.DataAccessStrategy)
	startTime := time.Now()
	RecordBackupStart(opType)

	// Mark operation as started.
	now := metav1.Now()
	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhasePending
		status.StartTime = &now
		status.Message = "Operation starting"
	}); err != nil {
		RecordBackupFailure(opType, dataAccess, time.Since(startTime).Seconds())
		return bw.failOperation(ctx, operation, fmt.Errorf("update start status: %w", err))
	}

	var execErr error
	var bytesTransferred int64
	switch operation.Spec.OperationType {
	case drv1alpha1.OperationTypeBackup:
		log.Info("Starting backup workflow")
		bytesTransferred, execErr = bw.executeBackup(ctx, operation, repo, podConfig)
	case drv1alpha1.OperationTypeRestore:
		log.Info("Starting restore workflow")
		bytesTransferred, execErr = bw.executeRestore(ctx, operation, repo, podConfig)
	default:
		RecordBackupFailure(opType, dataAccess, time.Since(startTime).Seconds())
		return bw.failOperation(ctx, operation, fmt.Errorf("unknown operation type: %s", operation.Spec.OperationType))
	}

	duration := time.Since(startTime).Seconds()
	if execErr != nil {
		RecordBackupFailure(opType, dataAccess, duration)
	} else {
		RecordBackupComplete(opType, dataAccess, duration, bytesTransferred)
	}
	return execErr
}

// executeBackup runs the backup workflow, selecting between snapshot and live paths
// based on the DataAccessStrategy. Returns bytes transferred from the Kopia operation.
func (bw *BackupWorkflow) executeBackup(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation, repo *drv1alpha1.BackupRepository, podConfig KopiaPodConfig) (int64, error) {
	log := bw.Log.WithFields(logrus.Fields{
		"operation": operation.Name,
		"pvc":       operation.Spec.SourcePVC.Name,
		"namespace": operation.Spec.SourcePVC.Namespace,
	})

	strategy, snapshotClassName, err := bw.resolveDataAccessStrategy(ctx, operation)
	if err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("resolve data access strategy: %w", err))
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
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("invalid resolved strategy: %s", strategy))
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
) (int64, error) {
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
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("create snapshot: %w", err))
	}
	snapshotName = snapshot.Name

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseSnapshotCreated
		status.Message = fmt.Sprintf("VolumeSnapshot %s created, waiting for ready", snapshotName)
	}); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("update snapshot created status: %w", err))
	}

	// Step 2: Wait for snapshot to be ready.
	if err := bw.SnapshotManager.WaitForSnapshotReady(ctx, pvcRef.Namespace, snapshotName, snapshotTimeout); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("wait for snapshot ready: %w", err))
	}

	// Step 3: Create temp PVC from snapshot.
	log.Info("Creating temporary PVC from snapshot")
	originalPVC := &corev1.PersistentVolumeClaim{}
	if err := bw.Client.Get(ctx, types.NamespacedName{
		Namespace: pvcRef.Namespace,
		Name:      pvcRef.Name,
	}, originalPVC); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("get source PVC: %w", err))
	}

	tempPVC, err := bw.SnapshotManager.RestoreSnapshotToPVC(ctx, pvcRef.Namespace, snapshotName, originalPVC)
	if err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("restore snapshot to temp PVC: %w", err))
	}
	tempPVCName = tempPVC.Name

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseDataAccessReady
		status.Message = fmt.Sprintf("Temporary PVC %s ready from snapshot", tempPVCName)
	}); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("update data access ready status: %w", err))
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
) (int64, error) {
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
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("update data access ready status: %w", err))
	}

	// Step 2-4: Run Kopia pod and extract snapshot ID.
	return bw.runBackupPod(ctx, operation, operation, repo, podConfig)
}

// runBackupPod creates the Kopia backup pod, waits for completion, extracts the snapshot ID
// and bytes transferred, and updates the operation status. The statusOperation is used for
// status updates, while podOperation is used for building the pod spec (may differ if using temp PVC).
func (bw *BackupWorkflow) runBackupPod(
	ctx context.Context,
	statusOperation *drv1alpha1.VolumeBackupOperation,
	podOperation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	podConfig KopiaPodConfig,
) (int64, error) {
	log := bw.Log.WithField("operation", statusOperation.Name)

	// Build the Kopia backup pod (validates S3 config).
	pod, err := BuildBackupPod(podOperation, repo, podConfig)
	if err != nil {
		return 0, bw.failOperation(ctx, statusOperation, fmt.Errorf("build backup pod: %w", err))
	}

	// Clean up pod on exit.
	defer func() {
		bw.cleanupPod(context.Background(), pod.Namespace, pod.Name)
	}()

	// Create the pod.
	log.WithField("pod", pod.Name).Info("Creating Kopia backup pod")
	if err := bw.Client.Create(ctx, pod); err != nil {
		return 0, bw.failOperation(ctx, statusOperation, fmt.Errorf("create backup pod: %w", err))
	}

	if err := bw.updateStatus(ctx, statusOperation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseBackupInProgress
		status.Message = fmt.Sprintf("Kopia backup pod %s running", pod.Name)
	}); err != nil {
		return 0, bw.failOperation(ctx, statusOperation, fmt.Errorf("update backup in progress status: %w", err))
	}

	// Wait for pod to complete.
	if err := bw.waitForPodCompletion(ctx, pod.Namespace, pod.Name, defaultPodTimeout); err != nil {
		return 0, bw.failOperation(ctx, statusOperation, fmt.Errorf("backup pod failed: %w", err))
	}

	// Extract Kopia snapshot ID and bytes transferred from pod logs.
	snapshotID, bytesTransferred, err := bw.extractSnapshotDataFromPodLogs(ctx, pod.Namespace, pod.Name)
	if err != nil {
		return 0, bw.failOperation(ctx, statusOperation, fmt.Errorf("extract snapshot ID: %w", err))
	}

	log.WithFields(logrus.Fields{
		"snapshot_id":       snapshotID,
		"bytes_transferred": bytesTransferred,
	}).Info("Backup completed, snapshot data extracted")

	// Update status with completion and bytes transferred.
	now := metav1.Now()
	if err := bw.updateStatus(ctx, statusOperation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseBackupComplete
		status.KopiaSnapshotID = snapshotID
		status.BytesTransferred = bytesTransferred
		status.CompletionTime = &now
		status.ProgressPercentage = 100
		status.Message = "Backup completed successfully"
	}); err != nil {
		return bytesTransferred, bw.failOperation(ctx, statusOperation, fmt.Errorf("update backup complete status: %w", err))
	}

	return bytesTransferred, nil
}

// executeRestore runs the restore workflow:
// 1. Build Kopia restore pod with destination PVC mounted
// 2. Wait for pod completion
// 3. Extract bytes transferred from pod output
// 4. Update status with bytes transferred
func (bw *BackupWorkflow) executeRestore(ctx context.Context, operation *drv1alpha1.VolumeBackupOperation, repo *drv1alpha1.BackupRepository, podConfig KopiaPodConfig) (int64, error) {
	log := bw.Log.WithField("operation", operation.Name)

	if operation.Spec.SnapshotID == "" {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("snapshotID is required for restore operations"))
	}

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseDataAccessReady
		status.Message = "Destination PVC ready for restore"
	}); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("update data access ready status: %w", err))
	}

	// Build the Kopia restore pod (validates snapshot ID and S3 config).
	pod, err := BuildRestorePod(operation, repo, podConfig)
	if err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("build restore pod: %w", err))
	}

	// Clean up pod on exit.
	defer func() {
		bw.cleanupPod(context.Background(), pod.Namespace, pod.Name)
	}()

	log.WithFields(logrus.Fields{
		"pod":         pod.Name,
		"snapshot_id": operation.Spec.SnapshotID,
	}).Info("Creating Kopia restore pod")
	if err := bw.Client.Create(ctx, pod); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("create restore pod: %w", err))
	}

	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseRestoreInProgress
		status.Message = fmt.Sprintf("Kopia restore pod %s running", pod.Name)
	}); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("update restore in progress status: %w", err))
	}

	// Wait for pod to complete.
	if err := bw.waitForPodCompletion(ctx, pod.Namespace, pod.Name, defaultPodTimeout); err != nil {
		return 0, bw.failOperation(ctx, operation, fmt.Errorf("restore pod failed: %w", err))
	}

	// Extract bytes transferred from restore pod output.
	bytesTransferred := bw.extractRestoreBytesFromPodLogs(ctx, pod.Namespace, pod.Name)

	log.WithField("bytes_transferred", bytesTransferred).Info("Restore completed, bytes extracted")

	// Update status with completion and bytes transferred.
	now := metav1.Now()
	if err := bw.updateStatus(ctx, operation, func(status *drv1alpha1.VolumeBackupOperationStatus) {
		status.Phase = drv1alpha1.VolumeBackupPhaseRestoreComplete
		status.BytesTransferred = bytesTransferred
		status.CompletionTime = &now
		status.ProgressPercentage = 100
		status.Message = "Restore completed successfully"
	}); err != nil {
		return bytesTransferred, bw.failOperation(ctx, operation, fmt.Errorf("update restore complete status: %w", err))
	}

	log.Info("Restore completed successfully")
	return bytesTransferred, nil
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
		Summ  *struct {
			Size int64 `json:"size"`
		} `json:"summ"`
	} `json:"rootEntry"`
	ID          string `json:"id"`
	Description string `json:"description"`
	Stats       *struct {
		Content *struct {
			HashedBytes int64 `json:"hashedBytes"`
			ReadBytes   int64 `json:"readBytes"`
		} `json:"content"`
		TotalSize int64 `json:"totalSize"`
	} `json:"stats"`
}

// extractSnapshotIDFromPodLogs reads the Kopia pod logs and extracts the snapshot ID
// from the JSON output of `kopia snapshot create --json`.
func (bw *BackupWorkflow) extractSnapshotIDFromPodLogs(ctx context.Context, namespace, podName string) (string, error) {
	id, _, err := bw.extractSnapshotDataFromPodLogs(ctx, namespace, podName)
	return id, err
}

// extractSnapshotDataFromPodLogs reads the Kopia pod logs and extracts the snapshot ID
// and bytes transferred from the JSON output of `kopia snapshot create --json`.
func (bw *BackupWorkflow) extractSnapshotDataFromPodLogs(ctx context.Context, namespace, podName string) (string, int64, error) {
	pod := &corev1.Pod{}
	if err := bw.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
		return "", 0, fmt.Errorf("get pod for log extraction: %w", err)
	}

	if len(pod.Status.ContainerStatuses) > 0 {
		cs := pod.Status.ContainerStatuses[0]
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			snapshotID, bytes, err := parseKopiaSnapshotOutput(cs.State.Terminated.Message)
			if err == nil && snapshotID != "" {
				return snapshotID, bytes, nil
			}
		}
	}

	if bw.podLogReader != nil {
		logs, err := bw.podLogReader.GetPodLogs(ctx, namespace, podName, "kopia")
		if err != nil {
			return "", 0, fmt.Errorf("get pod logs: %w", err)
		}

		snapshotID, bytes, err := parseKopiaSnapshotOutput(logs)
		if err != nil {
			return "", 0, fmt.Errorf("parse snapshot data from logs: %w", err)
		}
		if snapshotID != "" {
			return snapshotID, bytes, nil
		}
	}

	return "", 0, fmt.Errorf("no snapshot ID found in pod %s/%s output", namespace, podName)
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
// This is a backward-compatible wrapper around parseKopiaSnapshotOutput.
func parseKopiaSnapshotID(output string) (string, error) {
	id, _, err := parseKopiaSnapshotOutput(output)
	return id, err
}

// parseKopiaSnapshotOutput extracts the snapshot ID and bytes transferred from Kopia's JSON output.
// Kopia's `snapshot create --json` outputs a JSON object with the snapshot manifest.
// Bytes are extracted from stats.content.hashedBytes, falling back to stats.totalSize,
// then rootEntry.summ.size. Returns 0 bytes if no stats are present.
func parseKopiaSnapshotOutput(output string) (string, int64, error) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var snapshotOut kopiaSnapshotOutput
		if err := json.Unmarshal([]byte(line), &snapshotOut); err != nil {
			continue
		}

		// Extract snapshot ID.
		id := snapshotOut.ID
		if id == "" && snapshotOut.RootEntry != nil {
			id = snapshotOut.RootEntry.ObjID
		}
		if id == "" {
			continue
		}

		// Extract bytes transferred — prefer stats.content.hashedBytes (actual data processed),
		// then stats.totalSize, then rootEntry.summ.size.
		var bytes int64
		if snapshotOut.Stats != nil && snapshotOut.Stats.Content != nil && snapshotOut.Stats.Content.HashedBytes > 0 {
			bytes = snapshotOut.Stats.Content.HashedBytes
		} else if snapshotOut.Stats != nil && snapshotOut.Stats.TotalSize > 0 {
			bytes = snapshotOut.Stats.TotalSize
		} else if snapshotOut.RootEntry != nil && snapshotOut.RootEntry.Summ != nil && snapshotOut.RootEntry.Summ.Size > 0 {
			bytes = snapshotOut.RootEntry.Summ.Size
		}

		return id, bytes, nil
	}

	return "", 0, fmt.Errorf("no valid Kopia snapshot JSON found in output")
}

// extractRestoreBytesFromPodLogs attempts to extract bytes transferred from the Kopia
// restore pod output. Unlike backup pods which emit structured JSON, restore pods output
// a text summary line like: "Restored 2 files, 1 directories and 0 symbolic links (7.8 MB)"
// This is best-effort — returns 0 if parsing fails (restore still succeeds).
func (bw *BackupWorkflow) extractRestoreBytesFromPodLogs(ctx context.Context, namespace, podName string) int64 {
	pod := &corev1.Pod{}
	if err := bw.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
		bw.Log.WithError(err).Debug("Could not get pod for restore bytes extraction")
		return 0
	}

	// Try termination message first.
	if len(pod.Status.ContainerStatuses) > 0 {
		cs := pod.Status.ContainerStatuses[0]
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			if bytes, err := parseKopiaRestoreOutput(cs.State.Terminated.Message); err == nil && bytes > 0 {
				return bytes
			}
		}
	}

	// Try pod logs via log reader.
	if bw.podLogReader != nil {
		logs, err := bw.podLogReader.GetPodLogs(ctx, namespace, podName, "kopia")
		if err != nil {
			bw.Log.WithError(err).Debug("Could not read pod logs for restore bytes extraction")
			return 0
		}
		if bytes, err := parseKopiaRestoreOutput(logs); err == nil {
			return bytes
		}
	}

	return 0
}

// kopiaRestorePattern matches Kopia restore summary output.
// Example: "Restored 2 files, 1 directories and 0 symbolic links (7.8 MB)"
var kopiaRestorePattern = regexp.MustCompile(`Restored \d+ files.*\(([^)]+)\)`)

// parseKopiaRestoreOutput extracts bytes transferred from Kopia's restore text output.
// Kopia's `snapshot restore` outputs a summary line like:
//
//	"Restored 2 files, 1 directories and 0 symbolic links (7.8 MB)"
//
// Returns the byte count parsed from the human-readable size in parentheses.
func parseKopiaRestoreOutput(output string) (int64, error) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if matches := kopiaRestorePattern.FindStringSubmatch(line); len(matches) == 2 {
			return parseHumanReadableBytes(matches[1])
		}
	}
	return 0, fmt.Errorf("no Kopia restore summary found in output")
}

// parseHumanReadableBytes converts a human-readable byte string to int64.
// Supports formats like "53 B", "7.8 KB", "1.2 MB", "3.5 GB", "1.1 TB".
func parseHumanReadableBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Split into number and unit parts.
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid size format: %q", s)
	}

	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", parts[0], err)
	}

	unit := strings.ToUpper(parts[1])
	var multiplier float64
	switch unit {
	case "B":
		multiplier = 1
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown unit %q", unit)
	}

	return int64(value * multiplier), nil
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
