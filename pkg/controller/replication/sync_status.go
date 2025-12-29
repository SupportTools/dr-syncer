package replication

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Event reason constants for PVC sync workflow observability
const (
	// EventReasonSyncStarted indicates the sync workflow has begun
	EventReasonSyncStarted = "SyncStarted"

	// EventReasonLockAcquired indicates the PVC lock was successfully acquired
	EventReasonLockAcquired = "LockAcquired"

	// EventReasonRsyncPodDeployed indicates the rsync pod was deployed in the destination cluster
	EventReasonRsyncPodDeployed = "RsyncPodDeployed"

	// EventReasonSSHConnected indicates SSH connectivity was established
	EventReasonSSHConnected = "SSHConnected"

	// EventReasonSyncCompleted indicates the sync completed successfully
	EventReasonSyncCompleted = "SyncCompleted"

	// EventReasonSyncFailed indicates the sync operation failed
	EventReasonSyncFailed = "SyncFailed"

	// EventReasonLockReleased indicates the PVC lock was released
	EventReasonLockReleased = "LockReleased"

	// EventReasonSyncSkipped indicates the sync was skipped (e.g., locked by another, PVC not mounted)
	EventReasonSyncSkipped = "SyncSkipped"
)

// SyncStatus represents the status of a sync operation
type SyncStatus struct {
	Phase              string              `json:"phase"`
	StartTime          time.Time           `json:"startTime"`
	CompletionTime     time.Time           `json:"completionTime,omitempty"`
	BytesTransferred   int64               `json:"bytesTransferred"`
	FilesTransferred   int                 `json:"filesTransferred"`
	TotalBytes         int64               `json:"totalBytes,omitempty"`         // Total bytes to transfer (if known)
	TotalFiles         int                 `json:"totalFiles,omitempty"`         // Total files to transfer (if known)
	Progress           int                 `json:"progress"`                     // 0-100
	SpeedBytesPerSec   float64             `json:"speedBytesPerSec,omitempty"`   // Current transfer speed
	EstimatedRemaining string              `json:"estimatedRemaining,omitempty"` // Estimated time remaining (e.g., "5m30s")
	Error              string              `json:"error,omitempty"`
	Verification       *VerificationResult `json:"verification,omitempty"`
}

// VerificationResult holds the result of data verification after sync
type VerificationResult struct {
	// Mode is the verification mode used
	Mode drv1alpha1.VerificationMode `json:"mode"`
	// FilesVerified is the number of files that were verified
	FilesVerified int `json:"filesVerified"`
	// FilesTotal is the total number of files in the destination
	FilesTotal int `json:"filesTotal"`
	// ChecksumMatch indicates whether all verified checksums matched
	ChecksumMatch bool `json:"checksumMatch"`
	// VerifiedAt is when the verification was performed
	VerifiedAt time.Time `json:"verifiedAt,omitempty"`
	// Error contains any error message from verification
	Error string `json:"error,omitempty"`
}

// UpdateSyncStatus updates the sync status on the PVC
func (p *PVCSyncer) UpdateSyncStatus(ctx context.Context, namespace, pvcName string, status SyncStatus) error {
	log.WithFields(logrus.Fields{
		"namespace":         namespace,
		"pvc_name":          pvcName,
		"phase":             status.Phase,
		"bytes_transferred": status.BytesTransferred,
		"files_transferred": status.FilesTransferred,
		"progress":          status.Progress,
	}).Info(logging.LogTagInfo + " Updating sync status")

	// Get the PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get PVC: %v", err)
	}

	// Initialize annotations if needed
	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}

	// Convert status to JSON
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %v", err)
	}

	// Update annotations
	pvc.Annotations["dr-syncer.io/sync-status"] = string(statusJSON)
	pvc.Annotations["dr-syncer.io/last-updated"] = time.Now().UTC().Format(time.RFC3339)
	pvc.Annotations["dr-syncer.io/phase"] = status.Phase

	if status.Progress > 0 {
		pvc.Annotations["dr-syncer.io/progress"] = fmt.Sprintf("%d", status.Progress)
	}

	// Update the PVC
	_, err = p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvc, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update PVC annotations: %v", err)
	}

	// Record event if we have a client
	p.recordSyncEvent(namespace, pvcName, status.Phase, status.Error)

	return nil
}

// ParseRsyncOutput parses the output of the rsync command to extract progress information
func ParseRsyncOutput(output string) (int64, int, int, error) {
	// Initialize values
	var bytesTransferred int64 = 0
	var filesTransferred int = 0
	var progress int = 0

	// Split the output into lines for processing
	lines := strings.Split(output, "\n")

	// Look for the summary line that has the format: "sent X bytes  received Y bytes"
	sentPattern := regexp.MustCompile(`sent ([0-9,]+) bytes`)
	receivedPattern := regexp.MustCompile(`received ([0-9,]+) bytes`)
	fileCountPattern := regexp.MustCompile(`Number of (regular )?files (transferred|created): ([0-9,]+)`)

	// Also count individual file transfer lines (lines containing progress indicators)
	fileTransferCount := 0
	for _, line := range lines {
		// Look for summary statistics
		if sentMatches := sentPattern.FindStringSubmatch(line); len(sentMatches) > 1 {
			// Parse the sent bytes value
			byteStr := strings.ReplaceAll(sentMatches[1], ",", "")
			if bytes, err := strconv.ParseInt(byteStr, 10, 64); err == nil {
				bytesTransferred += bytes
				log.WithFields(logrus.Fields{
					"bytes_sent": bytes,
				}).Debug(logging.LogTagDetail + " Parsed bytes sent from rsync output")
			}
		}

		if receivedMatches := receivedPattern.FindStringSubmatch(line); len(receivedMatches) > 1 {
			// Add received bytes to the total (typically much smaller than sent)
			byteStr := strings.ReplaceAll(receivedMatches[1], ",", "")
			if bytes, err := strconv.ParseInt(byteStr, 10, 64); err == nil {
				// We don't add this to bytesTransferred as that should represent data sent from source to destination
				log.WithFields(logrus.Fields{
					"bytes_received": bytes,
				}).Debug(logging.LogTagDetail + " Parsed bytes received from rsync output")
			}
		}

		// Look for file count information
		if fileMatches := fileCountPattern.FindStringSubmatch(line); len(fileMatches) > 3 {
			countStr := strings.ReplaceAll(fileMatches[3], ",", "")
			if count, err := strconv.Atoi(countStr); err == nil {
				filesTransferred = count
				log.WithFields(logrus.Fields{
					"files_transferred": count,
				}).Debug(logging.LogTagDetail + " Parsed files transferred from rsync output")
			}
		}

		// Count lines that look like file transfers
		if strings.Contains(line, "%") && (strings.Contains(line, "to-chk=") || strings.Contains(line, "to-check=")) {
			fileTransferCount++
		}
	}

	// If we didn't find an explicit file count but counted transfer lines, use that
	if filesTransferred == 0 && fileTransferCount > 0 {
		filesTransferred = fileTransferCount
		log.WithFields(logrus.Fields{
			"files_transferred": filesTransferred,
		}).Debug(logging.LogTagDetail + " Using file transfer line count as files transferred")
	}

	// Calculate approximate progress
	if filesTransferred > 0 {
		// If the transfer completed, set to 100%
		if strings.Contains(output, "speedup is") || strings.Contains(output, "total size is") {
			progress = 100
			log.Debug(logging.LogTagDetail + " Rsync completed, setting progress to 100%")
		} else {
			// Otherwise estimate based on what we know
			progress = min(95, fileTransferCount) // Cap at 95% until we see "total size is"
			log.WithFields(logrus.Fields{
				"progress": progress,
			}).Debug(logging.LogTagDetail + " Setting estimated progress value")
		}
	}

	return bytesTransferred, filesTransferred, progress, nil
}

// min is a helper function to return the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Progress2Info holds parsed information from rsync --info=progress2 output
type Progress2Info struct {
	BytesTransferred int64
	TotalBytes       int64
	FilesTransferred int
	TotalFiles       int
	Progress         int     // 0-100
	SpeedBytesPerSec float64 // Current transfer speed
	ETASeconds       int     // Estimated time remaining in seconds
}

// ParseProgress2Output parses rsync --info=progress2 streaming output
// This format provides overall transfer progress rather than per-file progress
// Example output: "  1,234,567  12%  100.00kB/s    0:05:30  (xfr#5, to-chk=95/100)"
func ParseProgress2Output(output string) *Progress2Info {
	info := &Progress2Info{}

	// Get the most recent progress line (last non-empty line with progress info)
	lines := strings.Split(output, "\n")
	var progressLine string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" && strings.Contains(line, "%") {
			progressLine = line
			break
		}
	}

	if progressLine == "" {
		return info
	}

	// Parse bytes transferred (first number in the line)
	// Format: "  1,234,567  12%  100.00kB/s    0:05:30"
	bytesPattern := regexp.MustCompile(`^\s*([0-9,]+)`)
	if matches := bytesPattern.FindStringSubmatch(progressLine); len(matches) > 1 {
		byteStr := strings.ReplaceAll(matches[1], ",", "")
		if bytes, err := strconv.ParseInt(byteStr, 10, 64); err == nil {
			info.BytesTransferred = bytes
		}
	}

	// Parse progress percentage
	percentPattern := regexp.MustCompile(`(\d+)%`)
	if matches := percentPattern.FindStringSubmatch(progressLine); len(matches) > 1 {
		if pct, err := strconv.Atoi(matches[1]); err == nil {
			info.Progress = pct
			// Estimate total bytes from percentage
			if pct > 0 && info.BytesTransferred > 0 {
				info.TotalBytes = (info.BytesTransferred * 100) / int64(pct)
			}
		}
	}

	// Parse speed (e.g., "100.00kB/s", "1.23MB/s", "500.00B/s")
	speedPattern := regexp.MustCompile(`([0-9.]+)([kKMGTB]+)/s`)
	if matches := speedPattern.FindStringSubmatch(progressLine); len(matches) > 2 {
		if speed, err := strconv.ParseFloat(matches[1], 64); err == nil {
			unit := strings.ToUpper(matches[2])
			switch {
			case strings.HasPrefix(unit, "K"):
				info.SpeedBytesPerSec = speed * 1024
			case strings.HasPrefix(unit, "M"):
				info.SpeedBytesPerSec = speed * 1024 * 1024
			case strings.HasPrefix(unit, "G"):
				info.SpeedBytesPerSec = speed * 1024 * 1024 * 1024
			case strings.HasPrefix(unit, "T"):
				info.SpeedBytesPerSec = speed * 1024 * 1024 * 1024 * 1024
			default:
				info.SpeedBytesPerSec = speed
			}
		}
	}

	// Parse ETA (e.g., "0:05:30" or "1:23:45")
	etaPattern := regexp.MustCompile(`(\d+):(\d+):(\d+)`)
	if matches := etaPattern.FindStringSubmatch(progressLine); len(matches) > 3 {
		hours, _ := strconv.Atoi(matches[1])
		mins, _ := strconv.Atoi(matches[2])
		secs, _ := strconv.Atoi(matches[3])
		info.ETASeconds = hours*3600 + mins*60 + secs
	}

	// Parse file transfer info (e.g., "xfr#5, to-chk=95/100")
	xfrPattern := regexp.MustCompile(`xfr#(\d+)`)
	if matches := xfrPattern.FindStringSubmatch(progressLine); len(matches) > 1 {
		if xfr, err := strconv.Atoi(matches[1]); err == nil {
			info.FilesTransferred = xfr
		}
	}

	toChkPattern := regexp.MustCompile(`to-ch[ek]+=(\d+)/(\d+)`)
	if matches := toChkPattern.FindStringSubmatch(progressLine); len(matches) > 2 {
		remaining, _ := strconv.Atoi(matches[1])
		total, _ := strconv.Atoi(matches[2])
		info.TotalFiles = total
		if info.FilesTransferred == 0 {
			info.FilesTransferred = total - remaining
		}
	}

	return info
}

// FormatDuration formats seconds into a human-readable duration string
func FormatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	d := time.Duration(seconds) * time.Second
	if d >= time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	if d >= time.Minute {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	return fmt.Sprintf("%ds", seconds)
}

// recordSyncEvent records a Kubernetes event for a sync operation (legacy stub - kept for compatibility)
func (p *PVCSyncer) recordSyncEvent(namespace, pvcName, phase, errorMsg string) {
	fields := logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"phase":     phase,
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
		log.WithFields(fields).Error(logging.LogTagError + " Sync operation encountered an error")
	} else {
		log.WithFields(fields).Info(logging.LogTagInfo + " Sync operation status update")
	}
}

// recordEvent emits a Kubernetes event on the source PVC for observability
// This enables users to view sync progress via `kubectl describe pvc <name>`
func (p *PVCSyncer) recordEvent(ctx context.Context, namespace, pvcName string,
	eventType, reason, messageFmt string, args ...interface{}) {

	message := fmt.Sprintf(messageFmt, args...)

	// Always log the event for debugging
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"reason":    reason,
		"message":   message,
	}).Info(logging.LogTagInfo + " [EVENT] " + message)

	// If no event recorder is available, gracefully skip event emission
	if p.SourceEventRecorder == nil {
		return
	}

	// Get the PVC object to attach the event to
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(
		ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Warn(logging.LogTagWarn + " Failed to get PVC for event emission")
		return
	}

	// Emit the Kubernetes event on the PVC
	p.SourceEventRecorder.Eventf(pvc, eventType, reason, messageFmt, args...)
}

// RecordNormalEvent emits a Normal-type Kubernetes event on the source PVC
func (p *PVCSyncer) RecordNormalEvent(ctx context.Context, namespace, pvcName, reason string, messageFmt string, args ...interface{}) {
	p.recordEvent(ctx, namespace, pvcName, corev1.EventTypeNormal, reason, messageFmt, args...)
}

// RecordWarningEvent emits a Warning-type Kubernetes event on the source PVC
func (p *PVCSyncer) RecordWarningEvent(ctx context.Context, namespace, pvcName, reason string, messageFmt string, args ...interface{}) {
	p.recordEvent(ctx, namespace, pvcName, corev1.EventTypeWarning, reason, messageFmt, args...)
}

// InitSyncStatus initializes a new sync status
func (p *PVCSyncer) InitSyncStatus(ctx context.Context, namespace, pvcName string) error {
	status := SyncStatus{
		Phase:            "Initializing",
		StartTime:        time.Now(),
		BytesTransferred: 0,
		FilesTransferred: 0,
		Progress:         0,
	}

	return p.UpdateSyncStatus(ctx, namespace, pvcName, status)
}

// CompleteSyncStatus updates the sync status to completed
func (p *PVCSyncer) CompleteSyncStatus(ctx context.Context, namespace, pvcName string,
	bytesTransferred int64, filesTransferred int) error {

	status := SyncStatus{
		Phase:            "Completed",
		StartTime:        time.Now().Add(-1 * time.Minute), // Approximate
		CompletionTime:   time.Now(),
		BytesTransferred: bytesTransferred,
		FilesTransferred: filesTransferred,
		Progress:         100,
	}

	return p.UpdateSyncStatus(ctx, namespace, pvcName, status)
}

// CompleteSyncStatusWithVerification updates the sync status to completed with verification results
func (p *PVCSyncer) CompleteSyncStatusWithVerification(ctx context.Context, namespace, pvcName string,
	bytesTransferred int64, filesTransferred int, verification *VerificationResult) error {

	status := SyncStatus{
		Phase:            "Completed",
		StartTime:        time.Now().Add(-1 * time.Minute), // Approximate
		CompletionTime:   time.Now(),
		BytesTransferred: bytesTransferred,
		FilesTransferred: filesTransferred,
		Progress:         100,
		Verification:     verification,
	}

	// Log verification results if available
	if verification != nil {
		log.WithFields(logrus.Fields{
			"namespace":      namespace,
			"pvc_name":       pvcName,
			"mode":           verification.Mode,
			"files_verified": verification.FilesVerified,
			"files_total":    verification.FilesTotal,
			"checksum_match": verification.ChecksumMatch,
		}).Info(logging.LogTagInfo + " Sync completed with verification")
	}

	return p.UpdateSyncStatus(ctx, namespace, pvcName, status)
}

// FailedSyncStatus updates the sync status to failed
func (p *PVCSyncer) FailedSyncStatus(ctx context.Context, namespace, pvcName string, err error) error {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	status := SyncStatus{
		Phase:            "Failed",
		CompletionTime:   time.Now(),
		BytesTransferred: 0,
		FilesTransferred: 0,
		Progress:         0,
		Error:            errMsg,
	}

	return p.UpdateSyncStatus(ctx, namespace, pvcName, status)
}

// =============================================================================
// PVCSyncOperation CRD Management
// =============================================================================

const (
	// MaxPVCSyncHistoryEntries is the maximum number of history entries to keep
	MaxPVCSyncHistoryEntries = 5
)

// generatePVCSyncOperationName generates a deterministic name for a PVCSyncOperation resource
// Format: {sourceNS}-{pvcName}-{destNS} truncated to 63 chars max with hash suffix if needed
func generatePVCSyncOperationName(sourceNS, pvcName, destNS string) string {
	baseName := fmt.Sprintf("%s-%s-%s", sourceNS, pvcName, destNS)

	// Kubernetes names must be <= 63 characters
	if len(baseName) <= 63 {
		return baseName
	}

	// Use SHA256 hash to create a unique suffix
	hash := sha256.Sum256([]byte(baseName))
	hashSuffix := fmt.Sprintf("%x", hash)[:8]

	// Truncate base name to fit with hash suffix (63 - 1 dash - 8 hash = 54)
	maxBaseLen := 63 - 1 - 8
	if len(baseName) > maxBaseLen {
		baseName = baseName[:maxBaseLen]
	}

	return fmt.Sprintf("%s-%s", baseName, hashSuffix)
}

// CreateOrGetPVCSyncOperation creates or retrieves an existing PVCSyncOperation resource
// for tracking the current PVC sync operation
func (p *PVCSyncer) CreateOrGetPVCSyncOperation(ctx context.Context,
	sourceNS, pvcName, destNS, destPVCName string,
	namespaceMappingRef *drv1alpha1.PVCSyncNamespaceMappingRef) (*drv1alpha1.PVCSyncOperation, error) {

	if p.DestinationClient == nil {
		log.Warn("[PVCSyncOperation] DestinationClient is nil, skipping CRD management")
		return nil, nil
	}

	name := generatePVCSyncOperationName(sourceNS, pvcName, destNS)

	// Try to get existing PVCSyncOperation
	existing := &drv1alpha1.PVCSyncOperation{}
	err := p.DestinationClient.Get(ctx, client.ObjectKey{
		Namespace: destNS,
		Name:      name,
	}, existing)

	if err == nil {
		// Found existing resource
		log.WithFields(logrus.Fields{
			"name":      name,
			"namespace": destNS,
		}).Debug("[PVCSyncOperation] Found existing PVCSyncOperation resource")
		return existing, nil
	}

	if !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get PVCSyncOperation: %w", err)
	}

	// Create new PVCSyncOperation
	pvcSync := &drv1alpha1.PVCSyncOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: destNS,
			Labels: map[string]string{
				"dr-syncer.io/source-namespace": sourceNS,
				"dr-syncer.io/pvc-name":         pvcName,
				"dr-syncer.io/dest-namespace":   destNS,
			},
		},
		Spec: drv1alpha1.PVCSyncOperationSpec{
			SourceNamespace:      sourceNS,
			PVCName:              pvcName,
			DestinationNamespace: destNS,
			DestinationPVCName:   destPVCName,
			NamespaceMappingRef:  namespaceMappingRef,
		},
	}

	if err := p.DestinationClient.Create(ctx, pvcSync); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			// Race condition - another sync created it, fetch and return
			if getErr := p.DestinationClient.Get(ctx, client.ObjectKey{
				Namespace: destNS,
				Name:      name,
			}, existing); getErr == nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("failed to create PVCSyncOperation: %w", err)
	}

	log.WithFields(logrus.Fields{
		"name":      name,
		"namespace": destNS,
	}).Info("[PVCSyncOperation] Created new PVCSyncOperation resource")

	return pvcSync, nil
}

// InitPVCSyncOperation initializes a PVCSyncOperation at the start of a sync
func (p *PVCSyncer) InitPVCSyncOperation(ctx context.Context,
	sourceNS, pvcName, destNS, destPVCName string,
	namespaceMappingRef *drv1alpha1.PVCSyncNamespaceMappingRef) (*drv1alpha1.PVCSyncOperation, error) {

	pvcSync, err := p.CreateOrGetPVCSyncOperation(ctx, sourceNS, pvcName, destNS, destPVCName, namespaceMappingRef)
	if err != nil {
		return nil, err
	}
	if pvcSync == nil {
		return nil, nil // CRD management disabled
	}

	// Archive current status to history if we're starting a new sync on an existing resource
	if pvcSync.Status.Phase != "" && pvcSync.Status.Phase != drv1alpha1.PVCSyncOperationPhasePending {
		p.archiveToHistory(pvcSync)
	}

	// Reset status for new sync
	now := metav1.Now()
	pvcSync.Status = drv1alpha1.PVCSyncOperationStatus{
		Phase:     drv1alpha1.PVCSyncOperationPhaseInitializing,
		Progress:  "0%",
		StartTime: &now,
		History:   pvcSync.Status.History, // Preserve history
	}

	if err := p.DestinationClient.Status().Update(ctx, pvcSync); err != nil {
		return nil, fmt.Errorf("failed to initialize PVCSyncOperation status: %w", err)
	}

	log.WithFields(logrus.Fields{
		"name":      pvcSync.Name,
		"namespace": pvcSync.Namespace,
		"phase":     pvcSync.Status.Phase,
	}).Info("[PVCSyncOperation] Initialized sync operation")

	return pvcSync, nil
}

// UpdatePVCSyncOperationProgress updates the PVCSyncOperation with current progress
func (p *PVCSyncer) UpdatePVCSyncOperationProgress(ctx context.Context, pvcSync *drv1alpha1.PVCSyncOperation,
	progress int, bytesTransferred, totalBytes int64, filesTransferred, totalFiles int,
	speedBytesPerSec float64, etaSeconds int) error {

	if pvcSync == nil || p.DestinationClient == nil {
		return nil
	}

	// Fetch latest version to avoid conflicts
	latest := &drv1alpha1.PVCSyncOperation{}
	if err := p.DestinationClient.Get(ctx, client.ObjectKey{
		Namespace: pvcSync.Namespace,
		Name:      pvcSync.Name,
	}, latest); err != nil {
		return fmt.Errorf("failed to get latest PVCSyncOperation: %w", err)
	}

	// Update progress fields
	latest.Status.Phase = drv1alpha1.PVCSyncOperationPhaseSyncing
	latest.Status.Progress = fmt.Sprintf("%d%%", progress)
	latest.Status.BytesTransferred = bytesTransferred
	latest.Status.TotalBytes = totalBytes
	latest.Status.FilesTransferred = filesTransferred
	latest.Status.TotalFiles = totalFiles
	latest.Status.Speed = FormatSpeed(speedBytesPerSec)
	latest.Status.EstimatedRemaining = FormatDuration(etaSeconds)

	// Calculate duration since start
	if latest.Status.StartTime != nil {
		duration := time.Since(latest.Status.StartTime.Time)
		latest.Status.Duration = formatDurationShort(duration)
	}

	if err := p.DestinationClient.Status().Update(ctx, latest); err != nil {
		log.WithFields(logrus.Fields{
			"name":      latest.Name,
			"namespace": latest.Namespace,
			"error":     err,
		}).Warn("[PVCSyncOperation] Failed to update progress (will retry)")
		return err
	}

	return nil
}

// CompletePVCSyncOperation marks the PVCSyncOperation as completed
func (p *PVCSyncer) CompletePVCSyncOperation(ctx context.Context, pvcSync *drv1alpha1.PVCSyncOperation,
	bytesTransferred int64, filesTransferred int, verification *drv1alpha1.PVCSyncVerificationResult) error {

	if pvcSync == nil || p.DestinationClient == nil {
		return nil
	}

	// Fetch latest version
	latest := &drv1alpha1.PVCSyncOperation{}
	if err := p.DestinationClient.Get(ctx, client.ObjectKey{
		Namespace: pvcSync.Namespace,
		Name:      pvcSync.Name,
	}, latest); err != nil {
		return fmt.Errorf("failed to get latest PVCSyncOperation: %w", err)
	}

	now := metav1.Now()
	latest.Status.Phase = drv1alpha1.PVCSyncOperationPhaseCompleted
	latest.Status.Progress = "100%"
	latest.Status.CompletionTime = &now
	latest.Status.BytesTransferred = bytesTransferred
	latest.Status.FilesTransferred = filesTransferred
	latest.Status.Verification = verification
	latest.Status.Error = ""

	// Calculate final duration
	if latest.Status.StartTime != nil {
		duration := now.Time.Sub(latest.Status.StartTime.Time)
		latest.Status.Duration = formatDurationShort(duration)
	}

	if err := p.DestinationClient.Status().Update(ctx, latest); err != nil {
		return fmt.Errorf("failed to complete PVCSyncOperation: %w", err)
	}

	log.WithFields(logrus.Fields{
		"name":              latest.Name,
		"namespace":         latest.Namespace,
		"bytes_transferred": bytesTransferred,
		"files_transferred": filesTransferred,
		"duration":          latest.Status.Duration,
		"has_verification":  verification != nil,
	}).Info("[PVCSyncOperation] Sync operation completed")

	return nil
}

// FailPVCSyncOperation marks the PVCSyncOperation as failed
func (p *PVCSyncer) FailPVCSyncOperation(ctx context.Context, pvcSync *drv1alpha1.PVCSyncOperation, syncErr error) error {
	if pvcSync == nil || p.DestinationClient == nil {
		return nil
	}

	// Fetch latest version
	latest := &drv1alpha1.PVCSyncOperation{}
	if err := p.DestinationClient.Get(ctx, client.ObjectKey{
		Namespace: pvcSync.Namespace,
		Name:      pvcSync.Name,
	}, latest); err != nil {
		return fmt.Errorf("failed to get latest PVCSyncOperation: %w", err)
	}

	now := metav1.Now()
	latest.Status.Phase = drv1alpha1.PVCSyncOperationPhaseFailed
	latest.Status.CompletionTime = &now

	if syncErr != nil {
		latest.Status.Error = syncErr.Error()
	}

	// Calculate duration
	if latest.Status.StartTime != nil {
		duration := now.Time.Sub(latest.Status.StartTime.Time)
		latest.Status.Duration = formatDurationShort(duration)
	}

	if err := p.DestinationClient.Status().Update(ctx, latest); err != nil {
		return fmt.Errorf("failed to update PVCSyncOperation as failed: %w", err)
	}

	log.WithFields(logrus.Fields{
		"name":      latest.Name,
		"namespace": latest.Namespace,
		"error":     latest.Status.Error,
		"duration":  latest.Status.Duration,
	}).Warn("[PVCSyncOperation] Sync operation failed")

	return nil
}

// archiveToHistory archives the current status to history and trims to max entries
func (p *PVCSyncer) archiveToHistory(pvcSync *drv1alpha1.PVCSyncOperation) {
	if pvcSync.Status.StartTime == nil {
		return // No valid sync to archive
	}

	entry := drv1alpha1.PVCSyncHistoryEntry{
		StartTime:        *pvcSync.Status.StartTime,
		CompletionTime:   pvcSync.Status.CompletionTime,
		Phase:            pvcSync.Status.Phase,
		BytesTransferred: pvcSync.Status.BytesTransferred,
		FilesTransferred: pvcSync.Status.FilesTransferred,
		Duration:         pvcSync.Status.Duration,
		Error:            pvcSync.Status.Error,
	}

	// Prepend to history (newest first)
	pvcSync.Status.History = append([]drv1alpha1.PVCSyncHistoryEntry{entry}, pvcSync.Status.History...)

	// Trim to max entries
	if len(pvcSync.Status.History) > MaxPVCSyncHistoryEntries {
		pvcSync.Status.History = pvcSync.Status.History[:MaxPVCSyncHistoryEntries]
	}
}

// formatDurationShort formats a duration into a short human-readable string
func formatDurationShort(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}

	d = d.Round(time.Second)

	if d >= time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", hours, mins)
	}

	if d >= time.Minute {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}

	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// FormatSpeed formats bytes per second into a human-readable string (e.g., "1.5 MB/s")
func FormatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return ""
	}

	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytesPerSec >= GB:
		return fmt.Sprintf("%.2f GB/s", bytesPerSec/GB)
	case bytesPerSec >= MB:
		return fmt.Sprintf("%.2f MB/s", bytesPerSec/MB)
	case bytesPerSec >= KB:
		return fmt.Sprintf("%.2f KB/s", bytesPerSec/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}
