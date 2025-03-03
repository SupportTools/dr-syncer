package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SyncStatus represents the status of a sync operation
type SyncStatus struct {
	Phase           string    `json:"phase"`
	StartTime       time.Time `json:"startTime"`
	CompletionTime  time.Time `json:"completionTime,omitempty"`
	BytesTransferred int64    `json:"bytesTransferred"`
	FilesTransferred int      `json:"filesTransferred"`
	Progress        int       `json:"progress"` // 0-100
	Error           string    `json:"error,omitempty"`
}

// UpdateSyncStatus updates the sync status on the PVC
func (p *PVCSyncer) UpdateSyncStatus(ctx context.Context, namespace, pvcName string, status SyncStatus) error {
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"phase":              status.Phase,
		"bytes_transferred":  status.BytesTransferred,
		"files_transferred":  status.FilesTransferred,
		"progress":           status.Progress,
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
	
	if status.Error != "" {
		pvc.Annotations["dr-syncer.io/last-error"] = status.Error
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
		if sentMatches := sentPattern.FindStringSubmatch(line); sentMatches != nil && len(sentMatches) > 1 {
			// Parse the sent bytes value
			byteStr := strings.ReplaceAll(sentMatches[1], ",", "")
			if bytes, err := strconv.ParseInt(byteStr, 10, 64); err == nil {
				bytesTransferred += bytes
				log.WithFields(logrus.Fields{
					"bytes_sent": bytes,
				}).Debug(logging.LogTagDetail + " Parsed bytes sent from rsync output")
			}
		}
		
		if receivedMatches := receivedPattern.FindStringSubmatch(line); receivedMatches != nil && len(receivedMatches) > 1 {
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
		if fileMatches := fileCountPattern.FindStringSubmatch(line); fileMatches != nil && len(fileMatches) > 3 {
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

// recordSyncEvent records a Kubernetes event for a sync operation
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
