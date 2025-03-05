package replication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"github.com/supporttools/dr-syncer/pkg/logging"
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

	entry := log.WithFields(logrus.Fields{
		"deployment": destDeployment.Name,
		"pod_name":   destDeployment.PodName,
		"node_ip":    nodeIP,
		"mount_path": mountPath,
		"source":     sourceInfo,
		"dest":       destInfo,
	})
	entry.Info(logging.LogTagInfo + " Starting rsync operation")

	// Initialize sync status
	if err := p.InitSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Warn(logging.LogTagWarn + " Failed to initialize sync status, continuing anyway")
	}

	// Enhanced rsync options for better performance, robustness, and data integrity
	rsyncOptions := []string{
		"--archive",        // Archive mode (preserves permissions, timestamps, etc.)
		"--verbose",        // Verbose output
		"--delete",         // Delete files on destination that don't exist on source
		"--human-readable", // Human-readable output
		"--checksum",       // Use checksums to determine if files have changed
		"--partial",        // Keep partially transferred files
		"--progress",       // Show progress during transfer
		"--stats",          // Show file transfer statistics
		"--numeric-ids",    // Don't map uid/gid values by user/group name
		"--compress",       // Compress file data during transfer
		"--info=progress2", // Fine-grained information
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
				entry := log.WithFields(logrus.Fields{
					"bandwidth_limit": bwLimit,
				})
				entry.Debug(logging.LogTagDetail + " Applying bandwidth limit to rsync command")
				rsyncOptions = append(rsyncOptions, fmt.Sprintf("--bwlimit=%d", bwLimit))
			}
		}
	} else {
		entry := log.WithFields(logrus.Fields{
			"error": err,
		})
		entry.Debug(logging.LogTagDetail + " Failed to get NamespaceMapping for bandwidth limit, continuing without limit")
	}

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
		}).Warn(logging.LogTagWarn + " Failed to update sync status, continuing anyway")
	}

	// Combine rsync options
	rsyncOptsStr := strings.Join(rsyncOptions, " ")

	// Get SSH port from RemoteCluster CRD, default to 2222 if not specified
	sshPort := int32(2222) // Default port

	// Try to get the SSH port from the RemoteCluster CRD
	// First, get the RemoteCluster name from the source cluster
	var remoteClusterName string

	// Look up the RemoteCluster CRD using the source namespace
	// This assumes the NamespaceMapping has a reference to the RemoteCluster
	// or we can derive it from the source/destination namespace pair
	remoteClustersList := &drv1alpha1.RemoteClusterList{}
	if err := p.SourceClient.List(ctx, remoteClustersList); err == nil {
		// Log the number of remote clusters found
		log.WithFields(logrus.Fields{
			"count": len(remoteClustersList.Items),
		}).Debug(logging.LogTagDetail + " Found RemoteClusters")

		// If we found any RemoteClusters, use the first one's SSH port
		// In a production environment, we would need more logic to find the correct one
		if len(remoteClustersList.Items) > 0 {
			remoteCluster := remoteClustersList.Items[0]
			remoteClusterName = remoteCluster.Name

			// Check if PVCSync SSH config is available and has a port
			if remoteCluster.Spec.PVCSync != nil &&
				remoteCluster.Spec.PVCSync.SSH != nil &&
				remoteCluster.Spec.PVCSync.SSH.Port > 0 {
				sshPort = remoteCluster.Spec.PVCSync.SSH.Port
				log.WithFields(logrus.Fields{
					"remote_cluster": remoteClusterName,
					"ssh_port":       sshPort,
				}).Debug(logging.LogTagDetail + " Using SSH port from RemoteCluster CRD")
			}
		}
	} else {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Warn(logging.LogTagWarn + " Failed to list RemoteClusters, using default SSH port 2222")
	}

	// Build the rsync command to display output to pod's console
	// Format for maximum compatibility with the SSH command handler
	// Note we're using double quotes for the ssh command to ensure proper interpretation
	// We're also explicitly using --rsh instead of -e for better compatibility
	// Output is sent to /dev/tty in the pod for visibility in logs
	rsyncCmd := fmt.Sprintf("rsync %s --rsh=\"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d\" %s %s 2>&1 | tee /dev/stdout",
		rsyncOptsStr, sshPort, sourceInfo, destInfo)

	entry = log.WithFields(logrus.Fields{
		"rsync_cmd": rsyncCmd,
		"dest_pod":  destDeployment.PodName,
		"source":    sourceInfo,
		"dest":      destInfo,
	})
	entry.Debug(logging.LogTagDetail + " Executing rsync command")

	// Execute command in rsync pod
	cmd := []string{"sh", "-c", rsyncCmd}

	// Put the PVCSyncer in the context for ExecuteCommandInPod using our exported context key
	pvcSyncCtx := context.WithValue(rsyncCtx, SyncerKey, p)

	// Execute with retry logic for transient failures
	var stdout, stderr string
	err := withRetry(ctx, 2, 10*time.Second, func() error {
		var execErr error
		entry := log.WithFields(logrus.Fields{
			"deployment":       destDeployment.Name,
			"namespace":        destDeployment.Namespace,
			"pod_name":         destDeployment.PodName,
			"dest_client_host": p.DestinationConfig.Host,
		})
		entry.Debug(logging.LogTagDetail + " Executing rsync command with destination config")

		stdout, stderr, execErr = rsyncpod.ExecuteCommandInPod(pvcSyncCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, cmd, p.DestinationConfig)

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
		errorEntry := log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		})
		errorEntry.Error(logging.LogTagError + " Rsync command failed after retries")

		// Update status to failed
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)

		return fmt.Errorf("rsync command failed: %v", err)
	}

	// Check for rsync errors in output
	if strings.Contains(stderr, "rsync error") ||
		(strings.Contains(stdout, "rsync error") && !strings.Contains(stdout, "rsync error: some files/attrs were not transferred")) {
		errorEntry := log.WithFields(logrus.Fields{
			"stderr": stderr,
			"stdout": stdout,
		})
		errorEntry.Error(logging.LogTagError + " Rsync error detected in output")

		err := fmt.Errorf("rsync error: %s", stderr)
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)
		return err
	}

	// Parse rsync output to extract transfer stats
	bytesTransferred, filesTransferred, _, _ := ParseRsyncOutput(stdout)

	entry = log.WithFields(logrus.Fields{
		"deployment":        destDeployment.Name,
		"pod_name":          destDeployment.PodName,
		"node_ip":           nodeIP,
		"mount_path":        mountPath,
		"bytes_transferred": bytesTransferred,
		"files_transferred": filesTransferred,
	})
	entry.Info(logging.LogTagInfo + " Rsync command executed successfully")

	// Output first 100 characters of stdout to help with debugging
	if len(stdout) > 100 {
		entry = log.WithFields(logrus.Fields{
			"stdout_preview": stdout[:100] + "...",
		})
		entry.Debug(logging.LogTagDetail + " Rsync output preview")

		// Log the full output with multiple log entries for better visibility in logs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				logEntry := log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				})
				logEntry.Debug(logging.LogTagOutput + " Rsync output line")
			}
		}
	} else if len(stdout) > 0 {
		entry = log.WithFields(logrus.Fields{
			"stdout": stdout,
		})
		entry.Debug(logging.LogTagDetail + " Rsync output")

		// Log each line separately for better visibility even for shorter outputs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				logEntry := log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				})
				logEntry.Debug(logging.LogTagOutput + " Rsync output line")
			}
		}
	}

	// Verify the transfer by checking if files were actually transferred
	verifyCmd := []string{"sh", "-c", "if [ $(ls -la /data/ | wc -l) -gt 3 ]; then echo 'SUCCESS'; else echo 'FAILED'; fi"}

	// Use the context with PVCSyncer for verification
	pvcVerifyCtx := context.WithValue(ctx, SyncerKey, p)
	entry = log.WithFields(logrus.Fields{
		"deployment": destDeployment.Name,
		"namespace":  destDeployment.Namespace,
		"pod_name":   destDeployment.PodName,
	})
	entry.Debug(logging.LogTagDetail + " Verifying rsync result with destination config")
	verifyOut, _, err := rsyncpod.ExecuteCommandInPod(pvcVerifyCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, verifyCmd, p.DestinationConfig)
	if err != nil || !strings.Contains(verifyOut, "SUCCESS") {
		errorEntry := log.WithFields(logrus.Fields{
			"error":         err,
			"verify_output": verifyOut,
		})
		errorEntry.Error(logging.LogTagError + " Rsync verification failed")

		err := fmt.Errorf("rsync verification failed: %v", err)
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)
		return err
	}

	// Update status to completed
	if err := p.CompleteSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, bytesTransferred, filesTransferred); err != nil {
		warnEntry := log.WithFields(logrus.Fields{
			"error": err,
		})
		warnEntry.Warn(logging.LogTagWarn + " Failed to update final sync status, continuing anyway")
	}

	return nil
}
