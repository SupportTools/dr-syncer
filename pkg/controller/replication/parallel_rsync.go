/*
Copyright 2024 Support Tools.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package replication

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"github.com/supporttools/dr-syncer/pkg/logging"
)

// StreamResult contains the result of a single rsync stream
type StreamResult struct {
	StreamID         int
	Directories      []string
	BytesTransferred int64
	FilesTransferred int
	Duration         time.Duration
	Error            error
}

// AggregatedProgress combines progress from multiple streams
type AggregatedProgress struct {
	mu              sync.RWMutex
	TotalBytes      int64
	TotalFiles      int
	StreamsComplete int
	StreamsTotal    int
	StartTime       time.Time
}

// partitionDirectories lists top-level directories and distributes them across streams
// using round-robin assignment. Returns a slice of directory lists, one for each stream.
func (p *PVCSyncer) partitionDirectories(ctx context.Context, destDeployment *rsyncpod.RsyncDeployment,
	nodeIP, mountPath string, sshPort int32, numStreams int) ([][]string, error) {

	logger := log.WithFields(logrus.Fields{
		"node_ip":     nodeIP,
		"mount_path":  mountPath,
		"num_streams": numStreams,
	})
	logger.Debug(logging.LogTagDetail + " Partitioning directories for parallel rsync")

	// List top-level directories via SSH from the destination pod
	listCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d root@%s 'ls -d %s/*/ 2>/dev/null || echo \"\"'",
		sshPort, nodeIP, mountPath)

	cmd := []string{"sh", "-c", listCmd}
	pvcCtx := context.WithValue(ctx, SyncerKey, p)
	stdout, _, err := rsyncpod.ExecuteCommandInPod(pvcCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, cmd, p.DestinationConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to list directories: %w", err)
	}

	// Parse directory list
	var directories []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract just the directory name from the full path
		// e.g., /mnt/pvc/data/subdir1/ -> subdir1
		dirName := filepath.Base(strings.TrimSuffix(line, "/"))
		if dirName != "" && dirName != "." && dirName != ".." {
			directories = append(directories, dirName)
		}
	}

	logger.WithField("directories", len(directories)).Debug(logging.LogTagDetail + " Found top-level directories")

	// If no directories found, return single empty partition (will sync root files only)
	if len(directories) == 0 {
		logger.Info(logging.LogTagInfo + " No subdirectories found, using single stream for root-level sync")
		return [][]string{{}}, nil
	}

	// Adjust stream count if fewer directories than streams
	actualStreams := numStreams
	if len(directories) < numStreams {
		actualStreams = len(directories)
		logger.WithFields(logrus.Fields{
			"requested_streams": numStreams,
			"actual_streams":    actualStreams,
		}).Info(logging.LogTagInfo + " Reducing stream count to match directory count")
	}

	// Round-robin distribution
	partitions := make([][]string, actualStreams)
	for i := range partitions {
		partitions[i] = []string{}
	}

	for i, dir := range directories {
		streamIdx := i % actualStreams
		partitions[streamIdx] = append(partitions[streamIdx], dir)
	}

	// Log partition assignments
	for i, partition := range partitions {
		logger.WithFields(logrus.Fields{
			"stream":      i,
			"directories": len(partition),
		}).Debug(logging.LogTagDetail + " Stream directory assignment")
	}

	return partitions, nil
}

// executeParallelRsyncWorkflow runs multiple rsync streams concurrently
func (p *PVCSyncer) executeParallelRsyncWorkflow(ctx context.Context, destDeployment *rsyncpod.RsyncDeployment,
	nodeIP, mountPath string, numStreams int32, failureMode string, rsyncOptions []string,
	sshPort int32, syncStartTime time.Time, bandwidthLimit *int32) error {

	logger := log.WithFields(logrus.Fields{
		"pvc":          destDeployment.PVCName,
		"streams":      numStreams,
		"failure_mode": failureMode,
	})
	logger.Info(logging.LogTagInfo + " Starting parallel rsync workflow")

	// Partition directories
	partitions, err := p.partitionDirectories(ctx, destDeployment, nodeIP, mountPath, sshPort, int(numStreams))
	if err != nil {
		return fmt.Errorf("failed to partition directories: %w", err)
	}

	actualStreams := len(partitions)
	logger.WithField("actual_streams", actualStreams).Info(logging.LogTagInfo + " Partitioned directories for parallel sync")

	// Calculate per-stream bandwidth limit
	var streamBandwidth *int32
	if bandwidthLimit != nil && *bandwidthLimit > 0 {
		perStream := *bandwidthLimit / int32(actualStreams)
		if perStream < 1 {
			perStream = 1
		}
		streamBandwidth = &perStream
		logger.WithFields(logrus.Fields{
			"total_bandwidth":      *bandwidthLimit,
			"per_stream_bandwidth": perStream,
		}).Debug(logging.LogTagDetail + " Divided bandwidth across streams")
	}

	// Create cancellable context for fail-all mode
	streamCtx, cancelStreams := context.WithCancel(ctx)
	defer cancelStreams()

	// Result collection
	resultChan := make(chan StreamResult, actualStreams)
	var wg sync.WaitGroup

	// Track aggregated progress
	progress := &AggregatedProgress{
		StreamsTotal: actualStreams,
		StartTime:    syncStartTime,
	}

	// Determine destination path
	destPath := "/data/"
	if dsPath, ok := GetDaemonSetDestPath(ctx); ok && dsPath != "" {
		destPath = dsPath
		if !strings.HasSuffix(destPath, "/") {
			destPath += "/"
		}
	}

	// Launch goroutines for each stream
	for i := 0; i < actualStreams; i++ {
		wg.Add(1)
		go func(streamID int, dirs []string) {
			defer wg.Done()
			result := p.runStreamRsync(streamCtx, streamID, dirs, destDeployment, nodeIP, mountPath,
				destPath, rsyncOptions, sshPort, streamBandwidth, actualStreams)
			resultChan <- result

			// In fail-all mode, cancel all streams on first error
			if result.Error != nil && failureMode == "fail-all" {
				logger.WithFields(logrus.Fields{
					"stream": streamID,
					"error":  result.Error,
				}).Warn(logging.LogTagWarn + " Stream failed, cancelling remaining streams")
				cancelStreams()
			}
		}(i, partitions[i])
	}

	// Wait for all streams to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var results []StreamResult
	for result := range resultChan {
		results = append(results, result)

		// Update aggregated progress
		progress.mu.Lock()
		progress.StreamsComplete++
		progress.TotalBytes += result.BytesTransferred
		progress.TotalFiles += result.FilesTransferred
		progress.mu.Unlock()

		logger.WithFields(logrus.Fields{
			"stream":            result.StreamID,
			"bytes_transferred": result.BytesTransferred,
			"files_transferred": result.FilesTransferred,
			"duration":          result.Duration,
			"error":             result.Error,
		}).Info(logging.LogTagInfo + " Stream completed")
	}

	// Merge results
	totalBytes, totalFiles, errors := mergeStreamResults(results)

	// Handle errors based on failure mode
	if len(errors) > 0 {
		if failureMode == "fail-all" {
			// Return first error (all streams were cancelled)
			return fmt.Errorf("parallel rsync failed: %w", errors[0])
		}
		// In continue mode, log errors but continue if any stream succeeded
		for _, err := range errors {
			logger.WithError(err).Warn(logging.LogTagWarn + " Stream error in continue mode")
		}
		if len(errors) == actualStreams {
			return fmt.Errorf("all %d parallel rsync streams failed", actualStreams)
		}
		logger.WithFields(logrus.Fields{
			"failed_streams":     len(errors),
			"successful_streams": actualStreams - len(errors),
		}).Warn(logging.LogTagWarn + " Parallel rsync completed with partial failures")
	}

	// Record success metrics
	syncDuration := time.Since(syncStartTime).Seconds()
	RecordSyncComplete(
		p.SourceNamespace,
		destDeployment.PVCName,
		p.DestinationNamespace,
		totalBytes,
		totalFiles,
		syncDuration,
		len(errors) == 0,
	)

	// Update status to completed
	if err := p.CompleteSyncStatus(ctx, p.SourceNamespace, destDeployment.PVCName, totalBytes, totalFiles); err != nil {
		logger.WithError(err).Warn(logging.LogTagWarn + " Failed to update final sync status")
	}

	logger.WithFields(logrus.Fields{
		"total_bytes":    totalBytes,
		"total_files":    totalFiles,
		"duration":       syncDuration,
		"streams_used":   actualStreams,
		"streams_failed": len(errors),
	}).Info(logging.LogTagInfo + " Parallel rsync completed")

	return nil
}

// runStreamRsync executes rsync for a single stream's directories
func (p *PVCSyncer) runStreamRsync(ctx context.Context, streamID int, dirs []string,
	destDeployment *rsyncpod.RsyncDeployment, nodeIP, mountPath, destPath string,
	rsyncOptions []string, sshPort int32, bandwidthLimit *int32, totalStreams int) StreamResult {

	startTime := time.Now()
	result := StreamResult{
		StreamID:    streamID,
		Directories: dirs,
	}

	logger := log.WithFields(logrus.Fields{
		"stream":      streamID,
		"directories": len(dirs),
		"pvc":         destDeployment.PVCName,
	})
	logger.Debug(logging.LogTagDetail + " Starting stream rsync")

	// Build rsync options for this stream
	streamOptions := make([]string, len(rsyncOptions))
	copy(streamOptions, rsyncOptions)

	// Add bandwidth limit if specified
	if bandwidthLimit != nil && *bandwidthLimit > 0 {
		streamOptions = append(streamOptions, fmt.Sprintf("--bwlimit=%d", *bandwidthLimit))
	}

	// Build include/exclude filters for this stream's directories
	var filters []string

	if len(dirs) > 0 {
		// Include assigned directories
		for _, dir := range dirs {
			filters = append(filters, fmt.Sprintf("--include='%s/***'", dir))
		}
		// Stream 0 also handles root-level files
		if streamID == 0 {
			filters = append(filters, "--include='*'")
		}
		// Exclude all other directories
		filters = append(filters, "--exclude='*/'")
	} else {
		// No directories assigned - just sync root-level files (stream 0 only)
		if streamID == 0 {
			filters = append(filters, "--include='*'")
			filters = append(filters, "--exclude='*/'")
		} else {
			// Other streams have nothing to do
			result.Duration = time.Since(startTime)
			return result
		}
	}

	// Build the complete rsync command
	sourceInfo := fmt.Sprintf("root@%s:%s/", nodeIP, mountPath)
	optionsStr := strings.Join(streamOptions, " ")
	filtersStr := strings.Join(filters, " ")

	rsyncCmd := fmt.Sprintf("rsync %s %s --rsh=\"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d\" %s %s",
		optionsStr, filtersStr, sshPort, sourceInfo, destPath)

	logger.WithField("rsync_cmd", rsyncCmd).Debug(logging.LogTagDetail + " Executing stream rsync command")

	cmd := []string{"sh", "-c", rsyncCmd}
	pvcCtx := context.WithValue(ctx, SyncerKey, p)

	stdout, stderr, err := rsyncpod.ExecuteCommandInPod(pvcCtx, p.DestinationK8sClient, destDeployment.Namespace, destDeployment.PodName, cmd, p.DestinationConfig)

	result.Duration = time.Since(startTime)

	if err != nil {
		// Check if context was cancelled (from fail-all mode)
		if ctx.Err() != nil {
			result.Error = fmt.Errorf("stream cancelled: %w", ctx.Err())
		} else if isTransientError(err, stderr) {
			result.Error = fmt.Errorf("transient error in stream %d: %w", streamID, err)
		} else {
			result.Error = fmt.Errorf("rsync failed in stream %d: %w", streamID, err)
		}
		logger.WithError(result.Error).Warn(logging.LogTagWarn + " Stream rsync failed")
		return result
	}

	// Parse rsync output for statistics
	bytesTransferred, filesTransferred, _, parseErr := ParseRsyncOutput(stdout)
	if parseErr != nil {
		logger.WithError(parseErr).Debug(logging.LogTagDetail + " Failed to parse rsync output")
	}

	result.BytesTransferred = bytesTransferred
	result.FilesTransferred = filesTransferred

	logger.WithFields(logrus.Fields{
		"bytes_transferred": bytesTransferred,
		"files_transferred": filesTransferred,
		"duration":          result.Duration,
	}).Debug(logging.LogTagDetail + " Stream rsync completed successfully")

	return result
}

// mergeStreamResults combines results from all streams
func mergeStreamResults(results []StreamResult) (totalBytes int64, totalFiles int, errors []error) {
	for _, r := range results {
		totalBytes += r.BytesTransferred
		totalFiles += r.FilesTransferred
		if r.Error != nil {
			errors = append(errors, r.Error)
		}
	}
	return
}
