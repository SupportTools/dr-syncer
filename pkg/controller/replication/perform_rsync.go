package replication

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"github.com/supporttools/dr-syncer/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VerificationConfig holds the resolved verification configuration
type VerificationConfig struct {
	Mode          drv1alpha1.VerificationMode
	SamplePercent int32
}

// getVerificationConfig resolves the verification configuration with 3-level hierarchy:
// 1. Per-PVC annotation (highest priority)
// 2. NamespaceMapping config
// 3. RemoteCluster default (lowest priority)
func (p *PVCSyncer) getVerificationConfig(ctx context.Context, pvcName string, nm *drv1alpha1.NamespaceMapping) VerificationConfig {
	config := VerificationConfig{
		Mode:          drv1alpha1.VerificationModeNone,
		SamplePercent: 10, // Default sample percentage
	}

	// Priority 3: Get RemoteCluster defaults (lowest priority)
	remoteClustersList := &drv1alpha1.RemoteClusterList{}
	if err := p.SourceClient.List(ctx, remoteClustersList); err == nil {
		if len(remoteClustersList.Items) > 0 {
			rc := remoteClustersList.Items[0]
			if rc.Spec.PVCSync != nil {
				if rc.Spec.PVCSync.DefaultVerificationMode != "" {
					config.Mode = rc.Spec.PVCSync.DefaultVerificationMode
				}
				if rc.Spec.PVCSync.DefaultSamplePercent != nil {
					config.SamplePercent = *rc.Spec.PVCSync.DefaultSamplePercent
				}
			}
		}
	}

	// Priority 2: NamespaceMapping config overrides RemoteCluster
	if nm != nil && nm.Spec.PVCConfig != nil && nm.Spec.PVCConfig.DataSyncConfig != nil {
		dsc := nm.Spec.PVCConfig.DataSyncConfig
		if dsc.VerificationMode != "" {
			config.Mode = dsc.VerificationMode
		}
		if dsc.SamplePercent != nil {
			config.SamplePercent = *dsc.SamplePercent
		}
	}

	// Priority 1: Per-PVC annotation (highest priority)
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(p.SourceNamespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err == nil && pvc.Annotations != nil {
		if mode, ok := pvc.Annotations["dr-syncer.io/verification-mode"]; ok {
			switch drv1alpha1.VerificationMode(mode) {
			case drv1alpha1.VerificationModeNone, drv1alpha1.VerificationModeSample, drv1alpha1.VerificationModeFull:
				config.Mode = drv1alpha1.VerificationMode(mode)
			default:
				log.WithFields(logrus.Fields{
					"pvc":  pvcName,
					"mode": mode,
				}).Warn(logging.LogTagWarn + " Invalid verification mode in PVC annotation, using inherited value")
			}
		}
		if sampleStr, ok := pvc.Annotations["dr-syncer.io/sample-percent"]; ok {
			var samplePercent int32
			if _, err := fmt.Sscanf(sampleStr, "%d", &samplePercent); err == nil {
				if samplePercent >= 1 && samplePercent <= 100 {
					config.SamplePercent = samplePercent
				}
			}
		}
	}

	log.WithFields(logrus.Fields{
		"pvc":            pvcName,
		"mode":           config.Mode,
		"sample_percent": config.SamplePercent,
	}).Debug(logging.LogTagDetail + " Resolved verification configuration")

	return config
}

// performSampleVerification performs checksum verification on a random sample of files
func (p *PVCSyncer) performSampleVerification(ctx context.Context, destDeployment *rsyncpod.RsyncDeployment,
	nodeIP, mountPath string, sshPort int32, samplePercent int32) (*VerificationResult, error) {

	result := &VerificationResult{
		Mode:          drv1alpha1.VerificationModeSample,
		ChecksumMatch: true,
		VerifiedAt:    time.Now(),
	}

	// Get list of files in destination
	listCmd := []string{"sh", "-c", "find /data -type f 2>/dev/null | head -1000"}
	pvcCtx := context.WithValue(ctx, SyncerKey, p)
	stdout, _, err := rsyncpod.ExecuteCommandInPod(pvcCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, listCmd, p.DestinationConfig)
	if err != nil {
		result.Error = fmt.Sprintf("failed to list files: %v", err)
		result.ChecksumMatch = false
		return result, err
	}

	files := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(files) == 0 || (len(files) == 1 && files[0] == "") {
		log.Debug(logging.LogTagDetail + " No files to verify in sample mode")
		result.FilesVerified = 0
		result.FilesTotal = 0
		return result, nil
	}

	result.FilesTotal = len(files)

	// Calculate number of files to sample
	numSamples := int(float64(len(files)) * float64(samplePercent) / 100.0)
	if numSamples < 1 {
		numSamples = 1
	}
	if numSamples > len(files) {
		numSamples = len(files)
	}

	// Randomly select files to verify
	rand.Shuffle(len(files), func(i, j int) {
		files[i], files[j] = files[j], files[i]
	})
	samplesToVerify := files[:numSamples]

	log.WithFields(logrus.Fields{
		"total_files":    len(files),
		"sample_percent": samplePercent,
		"num_samples":    numSamples,
	}).Debug(logging.LogTagDetail + " Starting sample verification")

	// Verify each sampled file
	verified := 0
	for _, file := range samplesToVerify {
		if file == "" {
			continue
		}

		// Convert destination path to source path
		relPath := strings.TrimPrefix(file, "/data")
		if relPath == "" {
			relPath = "/"
		}
		sourcePath := mountPath + relPath

		// Get checksums from both source and destination
		destChecksumCmd := []string{"sh", "-c", fmt.Sprintf("md5sum '%s' 2>/dev/null | awk '{print $1}'", file)}
		destChecksum, _, err := rsyncpod.ExecuteCommandInPod(pvcCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, destChecksumCmd, p.DestinationConfig)
		if err != nil {
			log.WithFields(logrus.Fields{
				"file":  file,
				"error": err,
			}).Warn(logging.LogTagWarn + " Failed to get destination checksum")
			continue
		}

		// Get source checksum via SSH
		sourceChecksumCmd := []string{"sh", "-c", fmt.Sprintf(
			"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d root@%s \"md5sum '%s' 2>/dev/null | awk '{print \\$1}'\"",
			sshPort, nodeIP, sourcePath)}
		sourceChecksum, _, err := rsyncpod.ExecuteCommandInPod(pvcCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, sourceChecksumCmd, p.DestinationConfig)
		if err != nil {
			log.WithFields(logrus.Fields{
				"file":  sourcePath,
				"error": err,
			}).Warn(logging.LogTagWarn + " Failed to get source checksum")
			continue
		}

		destChecksum = strings.TrimSpace(destChecksum)
		sourceChecksum = strings.TrimSpace(sourceChecksum)

		if destChecksum != sourceChecksum {
			log.WithFields(logrus.Fields{
				"file":         file,
				"src_checksum": sourceChecksum,
				"dst_checksum": destChecksum,
			}).Warn(logging.LogTagWarn + " Checksum mismatch detected")
			result.ChecksumMatch = false
			result.Error = fmt.Sprintf("checksum mismatch for file: %s", file)
		}
		verified++
	}

	result.FilesVerified = verified

	log.WithFields(logrus.Fields{
		"files_verified": verified,
		"files_total":    result.FilesTotal,
		"checksum_match": result.ChecksumMatch,
	}).Info(logging.LogTagInfo + " Sample verification completed")

	return result, nil
}

// isTransientError checks if an error is transient and should be retried
// It checks both the error message and stderr output for transient patterns
func isTransientError(err error, stderr string) bool {
	// Check error message for transient patterns
	if err != nil {
		errStr := strings.ToLower(err.Error())
		transientErrorPatterns := []string{
			"connection refused",
			"connection reset",
			"connection timed out",
			"no route to host",
			"network is unreachable",
			"i/o timeout",
			"broken pipe",
			"eof",
			"temporary failure",
			"resource temporarily unavailable",
		}
		for _, pattern := range transientErrorPatterns {
			if strings.Contains(errStr, pattern) {
				return true
			}
		}
	}

	// Check stderr for SSH/rsync specific transient errors
	if stderr != "" {
		stderrPatterns := []string{
			"Connection timed out",
			"Connection reset by peer",
			"Connection closed",
			"Host key verification failed",
			"Permission denied (publickey)",
			"ssh_exchange_identification",
			"Read from socket failed",
			"Write failed",
			"rsync error: error in rsync protocol data stream",
			"rsync: connection unexpectedly closed",
		}
		for _, pattern := range stderrPatterns {
			if strings.Contains(stderr, pattern) {
				return true
			}
		}
	}

	return false
}

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

	// Simple default rsync options
	rsyncOptions := []string{
		"-avz",       // Archive mode, verbose, compress
		"--progress", // Show progress during transfer
		"--delete",   // Delete files on destination that don't exist on source
	}

	// By default we won't use checksums for faster performance
	useChecksum := false

	// RetryConfig for configurable retry behavior (nil uses defaults)
	var retryConfig *drv1alpha1.RetryConfig

	// Get NamespaceMapping to check for bandwidth limit and custom options
	var nm drv1alpha1.NamespaceMapping
	var nmPtr *drv1alpha1.NamespaceMapping
	nmKey := client.ObjectKey{Name: fmt.Sprintf("%s-%s", p.SourceNamespace, p.DestinationNamespace)}
	if err := p.SourceClient.Get(ctx, nmKey, &nm); err == nil {
		nmPtr = &nm
		// Get RetryConfig from NamespaceMapping if available
		retryConfig = nm.Spec.RetryConfig

		// Check if PVCConfig and DataSyncConfig are defined
		if nm.Spec.PVCConfig != nil && nm.Spec.PVCConfig.DataSyncConfig != nil {
			// Check if custom RsyncOptions are provided
			if len(nm.Spec.PVCConfig.DataSyncConfig.RsyncOptions) > 0 {
				customOptions := nm.Spec.PVCConfig.DataSyncConfig.RsyncOptions
				entry := log.WithFields(logrus.Fields{
					"custom_options": customOptions,
				})
				entry.Debug(logging.LogTagDetail + " Adding custom rsync options from NamespaceMapping")
				rsyncOptions = append(rsyncOptions, customOptions...)

				// Check if any custom option includes checksum mode
				for _, opt := range customOptions {
					if opt == "--checksum" {
						useChecksum = true
						entry.Debug(logging.LogTagDetail + " Detected checksum option, enabling checksum mode")
						break
					}
				}
			}

			// Check for bandwidth limit
			if nm.Spec.PVCConfig.DataSyncConfig.BandwidthLimit != nil {
				bwLimit := *nm.Spec.PVCConfig.DataSyncConfig.BandwidthLimit
				if bwLimit > 0 {
					entry := log.WithFields(logrus.Fields{
						"bandwidth_limit": bwLimit,
					})
					entry.Debug(logging.LogTagDetail + " Applying bandwidth limit to rsync command")
					rsyncOptions = append(rsyncOptions, fmt.Sprintf("--bwlimit=%d", bwLimit))
				}
			}
		}
	} else {
		entry := log.WithFields(logrus.Fields{
			"error": err,
		})
		entry.Debug(logging.LogTagDetail + " Failed to get NamespaceMapping for custom options, continuing with defaults")
	}

	// Get verification configuration with 3-level hierarchy
	verifyConfig := p.getVerificationConfig(ctx, destDeployment.PVCName, nmPtr)

	// Apply verification mode to rsync options
	if verifyConfig.Mode == drv1alpha1.VerificationModeFull {
		useChecksum = true
		log.WithFields(logrus.Fields{
			"pvc":  destDeployment.PVCName,
			"mode": verifyConfig.Mode,
		}).Info(logging.LogTagInfo + " Using full checksum verification mode")
	}

	// Add checksum option if thorough verification needed
	if useChecksum {
		rsyncOptions = append(rsyncOptions, "--checksum")
		entry := log.WithFields(logrus.Fields{})
		entry.Debug(logging.LogTagDetail + " Using checksum verification for thorough data integrity check")
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
	// Output goes directly to the pod's stdout/stderr without capturing
	// This will show in the pod logs but not be returned to the controller
	rsyncCmd := fmt.Sprintf("rsync %s --rsh=\"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d\" %s %s",
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

	// Variable to store rsync output for parsing after successful execution
	var rsyncOutput string

	// Execute with configurable retry logic for transient failures
	// Uses RetryConfig from NamespaceMapping if available, otherwise uses defaults
	err := withRetryConfig(ctx, retryConfig, func() error {
		entry := log.WithFields(logrus.Fields{
			"deployment":       destDeployment.Name,
			"namespace":        destDeployment.Namespace,
			"pod_name":         destDeployment.PodName,
			"dest_client_host": p.DestinationConfig.Host,
		})
		entry.Debug(logging.LogTagDetail + " Executing rsync command with destination config")

		// Execute command but don't capture detailed stdout/stderr
		// Set a minimal timeout since we're not capturing the full output
		execCtx, cancel := context.WithTimeout(pvcSyncCtx, 30*time.Second)
		defer cancel()

		stdout, stderr, execErr := rsyncpod.ExecuteCommandInPod(execCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, cmd, p.DestinationConfig)

		if execErr != nil {
			// Use expanded error classification for transient detection
			if isTransientError(execErr, "") {
				return &RetryableError{Err: fmt.Errorf("transient error during rsync: %v", execErr)}
			}
			return execErr
		}

		// Check stderr for SSH/rsync specific transient errors
		if isTransientError(nil, stderr) {
			return &RetryableError{Err: fmt.Errorf("transient connection error in rsync: %s", stderr)}
		}

		// Store stdout for parsing after successful execution
		rsyncOutput = stdout
		return nil
	})

	if err != nil {
		errorEntry := log.WithFields(logrus.Fields{
			"error": err,
		})
		errorEntry.Error(logging.LogTagError + " Rsync command failed after retries")

		// Update status to failed
		p.FailedSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, err)

		return fmt.Errorf("rsync command failed: %v", err)
	}

	// Parse rsync output to get actual transfer statistics
	bytesTransferred, filesTransferred, _, parseErr := ParseRsyncOutput(rsyncOutput)
	if parseErr != nil {
		log.WithField("error", parseErr).Warn(logging.LogTagWarn + " Failed to parse rsync output, using defaults")
		bytesTransferred = 0
		filesTransferred = 0
	}

	entry = log.WithFields(logrus.Fields{
		"deployment": destDeployment.Name,
		"pod_name":   destDeployment.PodName,
		"node_ip":    nodeIP,
		"mount_path": mountPath,
	})
	entry.Info(logging.LogTagInfo + " Rsync command executed successfully. See pod logs for details.")

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

	// Perform sample verification if configured
	var verificationResult *VerificationResult
	if verifyConfig.Mode == drv1alpha1.VerificationModeSample {
		log.WithFields(logrus.Fields{
			"pvc":            destDeployment.PVCName,
			"sample_percent": verifyConfig.SamplePercent,
		}).Info(logging.LogTagInfo + " Performing sample checksum verification")

		verificationResult, err = p.performSampleVerification(ctx, destDeployment, nodeIP, mountPath, sshPort, verifyConfig.SamplePercent)
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Warn(logging.LogTagWarn + " Sample verification failed, but rsync completed")
		} else if !verificationResult.ChecksumMatch {
			log.WithFields(logrus.Fields{
				"files_verified": verificationResult.FilesVerified,
				"files_total":    verificationResult.FilesTotal,
				"error":          verificationResult.Error,
			}).Warn(logging.LogTagWarn + " Sample verification detected checksum mismatch")
		}
	} else if verifyConfig.Mode == drv1alpha1.VerificationModeFull {
		// For full mode, rsync --checksum already verified everything
		verificationResult = &VerificationResult{
			Mode:          drv1alpha1.VerificationModeFull,
			ChecksumMatch: true,
			VerifiedAt:    time.Now(),
		}
	}

	// Update status to completed with verification result
	if err := p.CompleteSyncStatusWithVerification(ctx, p.SourceNamespace, destDeployment.PVCName, bytesTransferred, filesTransferred, verificationResult); err != nil {
		warnEntry := log.WithFields(logrus.Fields{
			"error": err,
		})
		warnEntry.Warn(logging.LogTagWarn + " Failed to update final sync status, continuing anyway")
	}

	return nil
}
