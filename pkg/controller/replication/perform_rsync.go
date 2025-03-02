package replication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// performRsync performs the rsync operation between source and destination pods
func (p *PVCSyncer) performRsync(ctx context.Context, destDeployment *rsyncpod.RsyncDeployment, nodeIP, mountPath string) error {
	// Create a context with a timeout for the entire operation
	rsyncCtx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	
	// Source and destination info for logs
	sourceInfo := fmt.Sprintf("root@%s:%s/", nodeIP, mountPath)
	destInfo := "/data/"
	
	log.WithFields(logrus.Fields{
		"deployment": destDeployment.Name,
		"pod_name":   destDeployment.PodName,
		"node_ip":    nodeIP,
		"mount_path": mountPath,
		"source":     sourceInfo,
		"dest":       destInfo,
	}).Info("[DR-SYNC-DETAIL] Starting rsync operation")

	// Initialize sync status
	if err := p.InitSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Warn("[DR-SYNC-WARN] Failed to initialize sync status, continuing anyway")
	}

	// Enhanced rsync options for better performance, robustness, and data integrity
	rsyncOptions := []string{
		"--archive",           // Archive mode (preserves permissions, timestamps, etc.)
		"--verbose",           // Verbose output
		"--delete",            // Delete files on destination that don't exist on source
		"--human-readable",    // Human-readable output
		"--checksum",          // Use checksums to determine if files have changed
		"--partial",           // Keep partially transferred files
		"--progress",          // Show progress during transfer
		"--stats",             // Show file transfer statistics
		"--numeric-ids",       // Don't map uid/gid values by user/group name
		"--compress",          // Compress file data during transfer
		"--info=progress2",    // Fine-grained information
	}
	
	// Apply bandwidth limiting if configured in the NamespaceMapping CRD
	// Get the NamespaceMapping to check for bandwidth limit
	var nm drv1alpha1.NamespaceMapping
	nmKey := client.ObjectKey{Name: fmt.Sprintf("%s-%s", p.SourceNamespace, p.DestinationNamespace)}
	if err := p.SourceClient.Get(ctx, nmKey, &nm); err == nil {
		// Check if bandwidth limit is set
		if nm.Spec.PVCConfig != nil && nm.Spec.PVCConfig.DataSyncConfig != nil && 
		   nm.Spec.PVCConfig.DataSyncConfig.BandwidthLimit != nil {
			bwLimit := *nm.Spec.PVCConfig.DataSyncConfig.BandwidthLimit
			if bwLimit > 0 {
				log.WithFields(logrus.Fields{
					"bandwidth_limit": bwLimit,
				}).Info("[DR-SYNC-DETAIL] Applying bandwidth limit to rsync command")
				rsyncOptions = append(rsyncOptions, fmt.Sprintf("--bwlimit=%d", bwLimit))
			}
		}
	} else {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Debug("[DR-SYNC-DETAIL] Failed to get NamespaceMapping for bandwidth limit, continuing without limit")
	}

	// Test SSH connectivity first with retry logic
	log.Info("[DR-SYNC-DETAIL] Running pre-rsync SSH connectivity check")
	
	err := withRetry(ctx, 3, 5*time.Second, func() error {
		if err := p.TestSSHConnectivity(ctx, destDeployment, nodeIP, 2222); err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Warn("[DR-SYNC-WARN] SSH connectivity check failed, will retry")
			return &RetryableError{Err: fmt.Errorf("SSH connectivity test failed: %v", err)}
		}
		return nil
	})
	
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Pre-rsync SSH connectivity check failed after retries")
		
		// Update status to failed
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)
		
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}
	
	log.Info("[DR-SYNC-DETAIL] Pre-rsync SSH connectivity check passed")
	
	// Update status to show we're starting the actual sync
	status := SyncStatus{
		Phase:            "Syncing",
		StartTime:        time.Now(),
		BytesTransferred: 0,
		FilesTransferred: 0,
		Progress:         5, // Show 5% progress for starting the sync
	}
	
	if err := p.UpdateSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, status); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Warn("[DR-SYNC-WARN] Failed to update sync status, continuing anyway")
	}

	// Combine rsync options
	rsyncOptsStr := strings.Join(rsyncOptions, " ")

	// Build the rsync command with tee to log the output
	rsyncCmd := fmt.Sprintf("rsync %s -e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p 2222' %s %s 2>&1 | tee /var/log/rsync.log",
		rsyncOptsStr, sourceInfo, destInfo)

	log.WithFields(logrus.Fields{
		"rsync_cmd": rsyncCmd,
		"dest_pod":  destDeployment.PodName,
		"source":    sourceInfo,
		"dest":      destInfo,
	}).Info("[DR-SYNC-DETAIL] Executing rsync command")

	// Execute command in rsync pod
	cmd := []string{"sh", "-c", rsyncCmd}
	
	// Execute with retry logic for transient failures
	var stdout, stderr string
	err = withRetry(ctx, 2, 10*time.Second, func() error {
		var execErr error
		stdout, stderr, execErr = rsyncpod.ExecuteCommandInPod(rsyncCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, cmd)
		
		if execErr != nil {
			// Check if the error is retryable
			if strings.Contains(execErr.Error(), "connection refused") || 
			   strings.Contains(execErr.Error(), "connection reset") {
				return &RetryableError{Err: fmt.Errorf("transient error during rsync: %v", execErr)}
			}
			return execErr
		}
		
		// Also check stderr for transient errors that might need retry
		if strings.Contains(stderr, "Connection timed out") ||
		   strings.Contains(stderr, "Connection reset by peer") {
			return &RetryableError{Err: fmt.Errorf("transient connection error in rsync: %s", stderr)}
		}
		
		return nil
	})
	
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Rsync command failed after retries")
		
		// Update status to failed
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)
		
		return fmt.Errorf("rsync command failed: %v", err)
	}

	// Check for rsync errors in output
	if strings.Contains(stderr, "rsync error") || 
	   (strings.Contains(stdout, "rsync error") && !strings.Contains(stdout, "rsync error: some files/attrs were not transferred")) {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"stdout": stdout,
		}).Error("[DR-SYNC-ERROR] Rsync error detected in output")
		
		err := fmt.Errorf("rsync error: %s", stderr)
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)
		return err
	}

	// Parse rsync output to extract transfer stats
	bytesTransferred, filesTransferred, _, _ := ParseRsyncOutput(stdout)
	
	log.WithFields(logrus.Fields{
		"deployment":        destDeployment.Name,
		"pod_name":          destDeployment.PodName,
		"node_ip":           nodeIP,
		"mount_path":        mountPath,
		"bytes_transferred": bytesTransferred,
		"files_transferred": filesTransferred,
	}).Info("[DR-SYNC-DETAIL] Rsync command executed successfully")

	// Output first 100 characters of stdout to help with debugging
	if len(stdout) > 100 {
		log.WithFields(logrus.Fields{
			"stdout_preview": stdout[:100] + "...",
		}).Info("[DR-SYNC-DETAIL] Rsync output preview")

		// Log the full output with multiple log entries for better visibility in logs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				}).Debug("[DR-SYNC-OUTPUT] Rsync output line")
			}
		}
	} else if len(stdout) > 0 {
		log.WithFields(logrus.Fields{
			"stdout": stdout,
		}).Info("[DR-SYNC-DETAIL] Rsync output")

		// Log each line separately for better visibility even for shorter outputs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				}).Debug("[DR-SYNC-OUTPUT] Rsync output line")
			}
		}
	}

	// Verify the transfer by checking rsync exit code and log file
	verifyCmd := []string{"sh", "-c", "if [ -f /var/log/rsync.log ]; then echo 'SUCCESS'; else echo 'FAILED'; fi"}
	verifyOut, _, err := rsyncpod.ExecuteCommandInPod(ctx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, verifyCmd)
	if err != nil || !strings.Contains(verifyOut, "SUCCESS") {
		log.WithFields(logrus.Fields{
			"error": err,
			"verify_output": verifyOut,
		}).Error("[DR-SYNC-ERROR] Rsync verification failed")
		
		err := fmt.Errorf("rsync verification failed: %v", err)
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)
		return err
	}

	// Update status to completed
	if err := p.CompleteSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, bytesTransferred, filesTransferred); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Warn("[DR-SYNC-WARN] Failed to update final sync status, continuing anyway")
	}

	return nil
}

// TestSSHConnectivity tests SSH connectivity from the rsync pod to the agent pod
func (p *PVCSyncer) TestSSHConnectivity(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment, agentIP string, port int) error {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
		"agent_ip":   agentIP,
		"port":       port,
	}).Info("[DR-SYNC-DETAIL] Testing SSH connectivity")

	// Construct SSH command
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d root@%s 'echo SSH connectivity test'", port, agentIP)

	log.WithFields(logrus.Fields{
		"ssh_command": sshCommand,
	}).Info("[DR-SYNC-DETAIL] Executing SSH command")

	cmd := []string{"sh", "-c", sshCommand}

	// Execute command in pod to generate SSH keys
	stdout, stderr, err := rsyncpod.ExecuteCommandInPod(ctx, p.DestinationK8sClient, rsyncDeployment.Namespace, rsyncDeployment.PodName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to execute SSH command")
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}

	log.WithFields(logrus.Fields{
		"stdout": stdout,
	}).Info("[DR-SYNC-DETAIL] SSH connectivity test successful")

	return nil
}
