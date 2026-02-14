package backup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// defaultStallTimeout is how long to wait with no progress before declaring stalled.
	defaultStallTimeout = 5 * time.Minute

	// defaultUpdateInterval is the minimum interval between status update callbacks.
	defaultUpdateInterval = 10 * time.Second

	// containerNameKopia is the container name used in Kopia pods.
	containerNameKopia = "kopia"
)

// ProgressUpdate contains parsed progress information from a Kopia operation.
type ProgressUpdate struct {
	// BytesProcessed is the total bytes processed so far.
	BytesProcessed int64

	// TotalBytes is the estimated total bytes (available during restore operations).
	// This is 0 if not available.
	TotalBytes int64

	// FilesProcessed is the number of files processed so far.
	FilesProcessed int64

	// TotalFiles is the estimated total file count (available during restore operations).
	// This is 0 if not available.
	TotalFiles int64

	// PercentComplete is the estimated completion percentage (0-100).
	// This is -1 if not yet estimable.
	PercentComplete int32

	// CurrentFile is the file currently being processed, if available.
	CurrentFile string

	// Speed is the human-readable transfer speed, if available.
	Speed string

	// RawLine is the original log line that produced this update.
	RawLine string
}

// ProgressCallback is called when a new progress update is available.
// Return an error to stop monitoring.
type ProgressCallback func(ctx context.Context, update ProgressUpdate) error

// ProgressMonitor watches a Kopia pod's log output and reports progress.
type ProgressMonitor interface {
	// MonitorPodProgress streams logs from a running Kopia pod and calls the
	// callback with parsed progress updates. Blocks until the context is
	// cancelled, the pod finishes, or a stall timeout is reached.
	// Returns ErrStalled if the operation stalls.
	MonitorPodProgress(ctx context.Context, namespace, podName string, callback ProgressCallback) error
}

// ErrStalled indicates the Kopia operation produced no progress within the stall timeout.
var ErrStalled = errors.New("kopia operation stalled: no progress output within timeout")

// KopiaPodProgressMonitor implements ProgressMonitor by streaming pod logs
// via the Kubernetes typed clientset.
type KopiaPodProgressMonitor struct {
	clientset      kubernetes.Interface
	stallTimeout   time.Duration
	updateInterval time.Duration
	log            *logrus.Entry
}

// ProgressMonitorOption configures a KopiaPodProgressMonitor.
type ProgressMonitorOption func(*KopiaPodProgressMonitor)

// WithStallTimeout sets the stall detection timeout.
func WithStallTimeout(d time.Duration) ProgressMonitorOption {
	return func(m *KopiaPodProgressMonitor) {
		m.stallTimeout = d
	}
}

// WithUpdateInterval sets the minimum interval between callback invocations.
func WithUpdateInterval(d time.Duration) ProgressMonitorOption {
	return func(m *KopiaPodProgressMonitor) {
		m.updateInterval = d
	}
}

// NewKopiaPodProgressMonitor creates a new progress monitor.
func NewKopiaPodProgressMonitor(clientset kubernetes.Interface, opts ...ProgressMonitorOption) *KopiaPodProgressMonitor {
	m := &KopiaPodProgressMonitor{
		clientset:      clientset,
		stallTimeout:   defaultStallTimeout,
		updateInterval: defaultUpdateInterval,
		log:            logrus.WithField("component", "progress-monitor"),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MonitorPodProgress streams logs from the Kopia container and parses progress.
func (m *KopiaPodProgressMonitor) MonitorPodProgress(ctx context.Context, namespace, podName string, callback ProgressCallback) error {
	log := m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod":       podName,
	})

	log.Info("Starting progress monitoring for Kopia pod")

	logStream, err := m.openLogStream(ctx, namespace, podName)
	if err != nil {
		log.WithError(err).Warn("Failed to open log stream, progress monitoring unavailable")
		return fmt.Errorf("open log stream: %w", err)
	}
	defer logStream.Close()

	return m.processLogStream(ctx, log, logStream, callback)
}

// openLogStream opens a following log stream for the Kopia container.
func (m *KopiaPodProgressMonitor) openLogStream(ctx context.Context, namespace, podName string) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{
		Container: containerNameKopia,
		Follow:    true,
	}

	req := m.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	return req.Stream(ctx)
}

// scanResult is sent from the scanner goroutine to the main loop.
type scanResult struct {
	line string
	err  error
	done bool
}

// processLogStream reads lines from the log stream, parses progress, and invokes the callback.
// It runs the scanner in a separate goroutine so that context cancellation and stall
// detection work even when the underlying Read blocks.
func (m *KopiaPodProgressMonitor) processLogStream(ctx context.Context, log *logrus.Entry, stream io.Reader, callback ProgressCallback) error {
	lines := make(chan scanResult, 1)

	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		for scanner.Scan() {
			select {
			case lines <- scanResult{line: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- scanResult{err: err, done: true}:
			case <-ctx.Done():
			}
		} else {
			select {
			case lines <- scanResult{done: true}:
			case <-ctx.Done():
			}
		}
	}()

	stallTimer := time.NewTimer(m.stallTimeout)
	defer stallTimer.Stop()

	var lastCallbackTime time.Time

	for {
		select {
		case <-ctx.Done():
			log.Debug("Progress monitoring cancelled")
			return nil

		case <-stallTimer.C:
			log.Warn("Kopia operation stalled — no progress output within timeout")
			return ErrStalled

		case result, ok := <-lines:
			if !ok || result.done {
				if result.err != nil {
					log.WithError(result.err).Debug("Log stream ended with error")
				} else {
					log.Debug("Log stream ended (pod likely finished)")
				}
				return nil
			}

			update, parsed := parseKopiaProgressLine(result.line)
			if !parsed {
				continue
			}

			// Reset stall timer on any progress.
			if !stallTimer.Stop() {
				select {
				case <-stallTimer.C:
				default:
				}
			}
			stallTimer.Reset(m.stallTimeout)

			// Throttle callback invocations to avoid excessive API updates.
			now := time.Now()
			if now.Sub(lastCallbackTime) < m.updateInterval {
				continue
			}
			lastCallbackTime = now

			if callback != nil {
				if err := callback(ctx, update); err != nil {
					log.WithError(err).Warn("Progress callback failed")
					return fmt.Errorf("progress callback: %w", err)
				}
			}
		}
	}
}

// Kopia progress line patterns.
//
// Backup progress examples:
//   - "Snapshotting mmattox@dr-syncer:/data ..."
//   - " * 0 hashing, 1234 hashed (5.6 GB), 0 cached (0 B), uploaded 1.2 GB, estimating..."
//   - " * 0 hashing, 5678 hashed (12.3 GB), 100 cached (500 MB), uploaded 5.6 GB, estimated 20.5 GB (60.0%) 50m12s left"
//   - "Created snapshot with root k1234567890abcdef ..."
//
// Restore progress examples:
//   - "Processed 12877 (43.9 MB) of 79255 (614.3 MB) 351.5 Mbit/s (7.2%) remaining 12s."
//   - "Processed 30953 (258.4 GB) of 30952 (258.4 GB) 28.4 MB/s (100.0%) remaining 0s."
//   - "Processed 500 (1.2 GB) of 1000 (2.5 GB)"
var (
	// reKopiaHashing matches Kopia's incremental progress lines.
	// Pattern: N hashed (X.Y GB), ... uploaded Z.W GB, estimated T.U GB (P%) ...
	reKopiaHashing = regexp.MustCompile(
		`(\d+)\s+hashed\s+\(([^)]+)\).*uploaded\s+([^\s,]+)`,
	)

	// reKopiaEstimated matches the estimated total and percentage.
	// Pattern: estimated X.Y GB (P%) ...
	reKopiaEstimated = regexp.MustCompile(
		`estimated\s+[\d.]+\s+\w+\s+\((\d+(?:\.\d+)?)%\)`,
	)

	// reKopiaRestore matches Kopia's restore progress lines.
	// Pattern: Processed N (X.Y GB) of M (T.U GB) [speed (P%) remaining Ts]
	reKopiaRestore = regexp.MustCompile(
		`[Pp]rocessed\s+(\d+)\s+\(([^)]+)\)\s+of\s+(\d+)\s+\(([^)]+)\)`,
	)

	// reKopiaRestorePercent extracts percentage from restore progress lines.
	// Pattern: (P%) remaining ...
	reKopiaRestorePercent = regexp.MustCompile(
		`\((\d+(?:\.\d+)?)%\)\s+remaining`,
	)

	// reKopiaRestoreSpeed extracts speed from restore progress lines.
	// Pattern: 351.5 Mbit/s or 28.4 MB/s
	reKopiaRestoreSpeed = regexp.MustCompile(
		`(\d+(?:\.\d+)?)\s+((?:K|M|G|T)?(?:bit|B)/s)`,
	)

	// reKopiaProcessed matches simpler "Processed" output (backup-specific).
	// Pattern: Processed N files, X.Y GB ...
	reKopiaProcessed = regexp.MustCompile(
		`[Pp]rocessed\s+(\d+)\s+(?:files?|contents?).*?(\d+(?:\.\d+)?)\s*((?:K|M|G|T)?i?B)`,
	)

	// reCombinedSize matches a combined size string like "5.6GB" or "1.2GiB".
	reCombinedSize = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*((?:K|M|G|T)?i?B)$`)
)

// parseKopiaProgressLine attempts to extract progress from a single Kopia log line.
func parseKopiaProgressLine(line string) (ProgressUpdate, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ProgressUpdate{}, false
	}

	update := ProgressUpdate{
		PercentComplete: -1,
		RawLine:         line,
	}

	// Try the hashing pattern first (most common during snapshot create).
	if matches := reKopiaHashing.FindStringSubmatch(line); matches != nil {
		filesHashed, _ := strconv.ParseInt(matches[1], 10, 64)
		hashedBytes := parseSizeString(matches[2])
		uploadedBytes := parseSizeString(matches[3])

		update.FilesProcessed = filesHashed
		update.BytesProcessed = hashedBytes

		// If we also have an estimated line, parse it.
		if estMatches := reKopiaEstimated.FindStringSubmatch(line); estMatches != nil {
			pct, err := strconv.ParseFloat(estMatches[1], 64)
			if err == nil && pct >= 0 && pct <= 100 {
				update.PercentComplete = int32(pct)
			}
		}

		update.Speed = formatBytes(uploadedBytes) + " uploaded"
		return update, true
	}

	// Try the restore progress pattern: "Processed N (X.Y GB) of M (T.U GB) ..."
	if matches := reKopiaRestore.FindStringSubmatch(line); matches != nil {
		filesProcessed, _ := strconv.ParseInt(matches[1], 10, 64)
		bytesProcessed := parseSizeString(matches[2])
		totalFiles, _ := strconv.ParseInt(matches[3], 10, 64)
		totalBytes := parseSizeString(matches[4])

		update.FilesProcessed = filesProcessed
		update.BytesProcessed = bytesProcessed
		update.TotalFiles = totalFiles
		update.TotalBytes = totalBytes

		// Extract percentage if present.
		if pctMatches := reKopiaRestorePercent.FindStringSubmatch(line); pctMatches != nil {
			pct, err := strconv.ParseFloat(pctMatches[1], 64)
			if err == nil && pct >= 0 && pct <= 100 {
				update.PercentComplete = int32(pct)
			}
		}

		// Compute percentage from bytes if not explicitly provided.
		if update.PercentComplete == -1 && update.TotalBytes > 0 {
			computed := float64(update.BytesProcessed) / float64(update.TotalBytes) * 100
			if computed >= 0 && computed <= 100 {
				update.PercentComplete = int32(computed)
			}
		}

		// Extract speed if present.
		if speedMatches := reKopiaRestoreSpeed.FindStringSubmatch(line); speedMatches != nil {
			update.Speed = speedMatches[1] + " " + speedMatches[2]
		}

		return update, true
	}

	// Try the processed pattern (backup-specific: "Processed N files, X.Y GB").
	if matches := reKopiaProcessed.FindStringSubmatch(line); matches != nil {
		files, _ := strconv.ParseInt(matches[1], 10, 64)
		sizeVal, _ := strconv.ParseFloat(matches[2], 64)
		sizeBytes := applyUnit(sizeVal, matches[3])

		update.FilesProcessed = files
		update.BytesProcessed = sizeBytes
		return update, true
	}

	return ProgressUpdate{}, false
}

// parseSizeString parses a human-readable size like "5.6 GB" into bytes.
func parseSizeString(s string) int64 {
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	if len(parts) < 2 {
		// Try parsing as a combined string like "5.6GB"
		return parseCombinedSize(s)
	}
	val, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	return applyUnit(val, parts[1])
}

// parseCombinedSize handles strings like "5.6GB" without space.
func parseCombinedSize(s string) int64 {
	matches := reCombinedSize.FindStringSubmatch(strings.TrimSpace(s))
	if matches == nil {
		return 0
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}
	return applyUnit(val, matches[2])
}

// applyUnit converts a float value with a size unit to bytes.
func applyUnit(val float64, unit string) int64 {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B":
		return int64(val)
	case "KB", "KIB":
		return int64(val * 1024)
	case "MB", "MIB":
		return int64(val * 1024 * 1024)
	case "GB", "GIB":
		return int64(val * 1024 * 1024 * 1024)
	case "TB", "TIB":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(val)
	}
}

// formatBytes formats a byte count into a human-readable string.
func formatBytes(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case bytes >= tb:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(tb))
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
