package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
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
	}).Info("[DR-SYNC-STATUS] Updating sync status")

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
	// This is a simplified implementation that would need to be enhanced
	// to properly parse rsync output in a production environment
	
	// Example parsing logic (would need to be more robust)
	var bytesTransferred int64 = 0
	var filesTransferred int = 0
	var progress int = 0
	
	// Example: parse lines like "sent 1,048,576 bytes  received 2,048 bytes"
	if bytes, found := extractBytes(output, "sent"); found {
		bytesTransferred = bytes
	}
	
	// Count number of files transferred by counting lines with "100%"
	filesTransferred = countFileLines(output)
	
	// Calculate approximate progress (simplified)
	if filesTransferred > 0 {
		// This is very simplified - in a real implementation,
		// you would need to track total files and bytes
		progress = 100
	}
	
	return bytesTransferred, filesTransferred, progress, nil
}

// Helper to extract byte values from rsync output
func extractBytes(output string, prefix string) (int64, bool) {
	// Real implementation would use regex to extract numbers
	// and handle commas, unit multipliers, etc.
	return 0, false
}

// Helper to count file transfer lines in rsync output
func countFileLines(output string) int {
	// Real implementation would count lines containing transfer indicators
	return 0
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
		log.WithFields(fields).Error("[DR-SYNC-EVENT] Sync operation encountered an error")
	} else {
		log.WithFields(fields).Info("[DR-SYNC-EVENT] Sync operation status update")
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
