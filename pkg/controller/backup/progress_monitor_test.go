package backup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// --- parseKopiaProgressLine tests ---

func TestParseKopiaProgressLine_HashingWithEstimate(t *testing.T) {
	line := " * 0 hashing, 5678 hashed (12.3 GB), 100 cached (500 MB), uploaded 5.6 GB, estimated 20.5 GB (60.0%) 50m12s left"
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected progress line to be parsed")
	}
	if update.FilesProcessed != 5678 {
		t.Errorf("expected 5678 files, got %d", update.FilesProcessed)
	}
	// 12.3 GB = 12.3 * 1024 * 1024 * 1024 ≈ 13206769869
	if update.BytesProcessed < 13000000000 || update.BytesProcessed > 13500000000 {
		t.Errorf("expected ~13.2 billion bytes, got %d", update.BytesProcessed)
	}
	if update.PercentComplete != 60 {
		t.Errorf("expected 60%%, got %d", update.PercentComplete)
	}
}

func TestParseKopiaProgressLine_HashingWithoutEstimate(t *testing.T) {
	line := " * 0 hashing, 1234 hashed (5.6 GB), 0 cached (0 B), uploaded 1.2 GB, estimating..."
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected progress line to be parsed")
	}
	if update.FilesProcessed != 1234 {
		t.Errorf("expected 1234 files, got %d", update.FilesProcessed)
	}
	if update.PercentComplete != -1 {
		t.Errorf("expected -1 (unknown), got %d", update.PercentComplete)
	}
}

func TestParseKopiaProgressLine_Processed(t *testing.T) {
	line := "Processed 500 files, 2.5 GB in 30s"
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected progress line to be parsed")
	}
	if update.FilesProcessed != 500 {
		t.Errorf("expected 500 files, got %d", update.FilesProcessed)
	}
	// 2.5 GB ≈ 2684354560
	if update.BytesProcessed < 2600000000 || update.BytesProcessed > 2800000000 {
		t.Errorf("expected ~2.68 billion bytes, got %d", update.BytesProcessed)
	}
}

func TestParseKopiaProgressLine_EmptyLine(t *testing.T) {
	_, ok := parseKopiaProgressLine("")
	if ok {
		t.Error("expected empty line to not parse")
	}
}

func TestParseKopiaProgressLine_NonProgressLine(t *testing.T) {
	lines := []string{
		"Snapshotting mmattox@dr-syncer:/data ...",
		"Created snapshot with root k1234567890abcdef",
		`{"id":"k1234567890abcdef","rootEntry":{"obj":"x123abc"}}`,
		"kopia repository connect s3 --bucket=my-bucket",
	}
	for _, line := range lines {
		_, ok := parseKopiaProgressLine(line)
		if ok {
			t.Errorf("expected line to not parse as progress: %q", line)
		}
	}
}

func TestParseKopiaProgressLine_ProcessedContents(t *testing.T) {
	line := "processed 1000 contents, 3.5 GB total"
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected progress line to be parsed")
	}
	if update.FilesProcessed != 1000 {
		t.Errorf("expected 1000, got %d", update.FilesProcessed)
	}
}

// --- Restore progress parsing tests ---

func TestParseKopiaProgressLine_RestoreFullLine(t *testing.T) {
	line := "Processed 12877 (43.9 MB) of 79255 (614.3 MB) 351.5 Mbit/s (7.2%) remaining 12s."
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected restore progress line to be parsed")
	}
	if update.FilesProcessed != 12877 {
		t.Errorf("expected 12877 files processed, got %d", update.FilesProcessed)
	}
	if update.TotalFiles != 79255 {
		t.Errorf("expected 79255 total files, got %d", update.TotalFiles)
	}
	// 43.9 MB ≈ 46,031,462
	if update.BytesProcessed < 45000000 || update.BytesProcessed > 47000000 {
		t.Errorf("expected ~46M bytes processed, got %d", update.BytesProcessed)
	}
	// 614.3 MB ≈ 644,087,603
	if update.TotalBytes < 640000000 || update.TotalBytes > 650000000 {
		t.Errorf("expected ~644M total bytes, got %d", update.TotalBytes)
	}
	if update.PercentComplete != 7 {
		t.Errorf("expected 7%%, got %d", update.PercentComplete)
	}
	if update.Speed != "351.5 Mbit/s" {
		t.Errorf("expected '351.5 Mbit/s', got %q", update.Speed)
	}
}

func TestParseKopiaProgressLine_RestoreComplete(t *testing.T) {
	line := "Processed 30953 (258.4 GB) of 30952 (258.4 GB) 28.4 MB/s (100.0%) remaining 0s."
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected restore progress line to be parsed")
	}
	if update.FilesProcessed != 30953 {
		t.Errorf("expected 30953 files, got %d", update.FilesProcessed)
	}
	if update.TotalFiles != 30952 {
		t.Errorf("expected 30952 total files, got %d", update.TotalFiles)
	}
	if update.PercentComplete != 100 {
		t.Errorf("expected 100%%, got %d", update.PercentComplete)
	}
	if update.Speed != "28.4 MB/s" {
		t.Errorf("expected '28.4 MB/s', got %q", update.Speed)
	}
}

func TestParseKopiaProgressLine_RestoreWithoutSpeed(t *testing.T) {
	line := "Processed 500 (1.2 GB) of 1000 (2.5 GB)"
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected restore progress line to be parsed")
	}
	if update.FilesProcessed != 500 {
		t.Errorf("expected 500 files, got %d", update.FilesProcessed)
	}
	if update.TotalFiles != 1000 {
		t.Errorf("expected 1000 total files, got %d", update.TotalFiles)
	}
	// 1.2 GB ≈ 1,288,490,188
	if update.BytesProcessed < 1280000000 || update.BytesProcessed > 1300000000 {
		t.Errorf("expected ~1.29B bytes processed, got %d", update.BytesProcessed)
	}
	// 2.5 GB ≈ 2,684,354,560
	if update.TotalBytes < 2680000000 || update.TotalBytes > 2700000000 {
		t.Errorf("expected ~2.68B total bytes, got %d", update.TotalBytes)
	}
	// When no explicit percentage, it's computed from bytes: ~1.29B / ~2.68B ≈ 48%
	// int32 truncation means this lands at 47 or 48 depending on binary size rounding.
	if update.PercentComplete < 47 || update.PercentComplete > 48 {
		t.Errorf("expected ~48%% (computed from bytes), got %d", update.PercentComplete)
	}
	if update.Speed != "" {
		t.Errorf("expected empty speed, got %q", update.Speed)
	}
}

func TestParseKopiaProgressLine_RestoreLargeDataset(t *testing.T) {
	line := "Processed 150000 (1.5 TB) of 200000 (2.0 TB) 512.0 MB/s (75.0%) remaining 15m30s."
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected restore progress line to be parsed")
	}
	if update.FilesProcessed != 150000 {
		t.Errorf("expected 150000 files, got %d", update.FilesProcessed)
	}
	if update.TotalFiles != 200000 {
		t.Errorf("expected 200000 total files, got %d", update.TotalFiles)
	}
	// 1.5 TB = 1.5 * 1024^4 ≈ 1,649,267,441,664
	if update.BytesProcessed < 1640000000000 || update.BytesProcessed > 1660000000000 {
		t.Errorf("expected ~1.65T bytes processed, got %d", update.BytesProcessed)
	}
	// 2.0 TB = 2.0 * 1024^4 ≈ 2,199,023,255,552
	if update.TotalBytes < 2190000000000 || update.TotalBytes > 2210000000000 {
		t.Errorf("expected ~2.2T total bytes, got %d", update.TotalBytes)
	}
	if update.PercentComplete != 75 {
		t.Errorf("expected 75%%, got %d", update.PercentComplete)
	}
	if update.Speed != "512.0 MB/s" {
		t.Errorf("expected '512.0 MB/s', got %q", update.Speed)
	}
}

func TestParseKopiaProgressLine_RestoreLowercase(t *testing.T) {
	line := "processed 100 (50.0 MB) of 200 (100.0 MB) 10.0 MB/s (50.0%) remaining 5s."
	update, ok := parseKopiaProgressLine(line)
	if !ok {
		t.Fatal("expected lowercase restore progress line to be parsed")
	}
	if update.FilesProcessed != 100 {
		t.Errorf("expected 100 files, got %d", update.FilesProcessed)
	}
	if update.TotalFiles != 200 {
		t.Errorf("expected 200 total files, got %d", update.TotalFiles)
	}
	if update.PercentComplete != 50 {
		t.Errorf("expected 50%%, got %d", update.PercentComplete)
	}
}

// --- Size parsing tests ---

func TestParseSizeString(t *testing.T) {
	tests := []struct {
		input string
		min   int64
		max   int64
	}{
		{"5.6 GB", 6000000000, 6100000000},
		{"100 MB", 104000000, 105000000},
		{"0 B", 0, 0},
		{"1.5 TB", 1600000000000, 1700000000000},
		{"512 KB", 520000, 530000},
	}
	for _, tt := range tests {
		result := parseSizeString(tt.input)
		if result < tt.min || result > tt.max {
			t.Errorf("parseSizeString(%q) = %d, want between %d and %d", tt.input, result, tt.min, tt.max)
		}
	}
}

func TestParseCombinedSize(t *testing.T) {
	tests := []struct {
		input string
		min   int64
		max   int64
	}{
		{"5.6GB", 6000000000, 6100000000},
		{"100MB", 104000000, 105000000},
		{"1.5TB", 1600000000000, 1700000000000},
	}
	for _, tt := range tests {
		result := parseCombinedSize(tt.input)
		if result < tt.min || result > tt.max {
			t.Errorf("parseCombinedSize(%q) = %d, want between %d and %d", tt.input, result, tt.min, tt.max)
		}
	}
}

func TestApplyUnit(t *testing.T) {
	tests := []struct {
		val    float64
		unit   string
		expect int64
	}{
		{100, "B", 100},
		{1, "KB", 1024},
		{1, "MB", 1048576},
		{1, "GB", 1073741824},
		{1, "TB", 1099511627776},
	}
	for _, tt := range tests {
		got := applyUnit(tt.val, tt.unit)
		if got != tt.expect {
			t.Errorf("applyUnit(%f, %q) = %d, want %d", tt.val, tt.unit, got, tt.expect)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input  int64
		expect string
	}{
		{500, "500 B"},
		{1536, "1.5 KB"},
		{1572864, "1.5 MB"},
		{1610612736, "1.5 GB"},
		{1649267441664, "1.5 TB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.expect {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

// --- Additional size parsing edge cases ---

func TestApplyUnit_IECUnits(t *testing.T) {
	tests := []struct {
		val    float64
		unit   string
		expect int64
	}{
		{1, "KiB", 1024},
		{1, "MiB", 1048576},
		{1, "GiB", 1073741824},
		{1, "TiB", 1099511627776},
	}
	for _, tt := range tests {
		got := applyUnit(tt.val, tt.unit)
		if got != tt.expect {
			t.Errorf("applyUnit(%f, %q) = %d, want %d", tt.val, tt.unit, got, tt.expect)
		}
	}
}

func TestApplyUnit_UnknownUnit(t *testing.T) {
	got := applyUnit(42, "XB")
	if got != 42 {
		t.Errorf("applyUnit(42, 'XB') = %d, want 42", got)
	}
}

func TestParseSizeString_CombinedFormat(t *testing.T) {
	result := parseSizeString("5.6GB")
	if result < 6000000000 || result > 6100000000 {
		t.Errorf("parseSizeString('5.6GB') = %d, expected ~6 billion", result)
	}
}

func TestParseSizeString_InvalidInput(t *testing.T) {
	result := parseSizeString("not-a-size")
	if result != 0 {
		t.Errorf("expected 0 for invalid input, got %d", result)
	}
}

func TestParseCombinedSize_InvalidInput(t *testing.T) {
	result := parseCombinedSize("notasize")
	if result != 0 {
		t.Errorf("expected 0 for invalid input, got %d", result)
	}
}

func TestFormatBytes_Zero(t *testing.T) {
	got := formatBytes(0)
	if got != "0 B" {
		t.Errorf("formatBytes(0) = %q, want '0 B'", got)
	}
}

// --- processLogStream tests ---

func TestProcessLogStream_ParsesProgress(t *testing.T) {
	lines := strings.Join([]string{
		"Snapshotting mmattox@dr-syncer:/data ...",
		" * 0 hashing, 100 hashed (1.0 GB), 0 cached (0 B), uploaded 500 MB, estimating...",
		" * 0 hashing, 200 hashed (2.0 GB), 0 cached (0 B), uploaded 1.0 GB, estimated 4.0 GB (50.0%) 2m left",
		`{"id":"k123","rootEntry":{"obj":"x456"}}`,
	}, "\n")

	reader := strings.NewReader(lines)
	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   1 * time.Minute,
		updateInterval: 0, // no throttling for tests
		log:            logrusTestEntry(),
	}

	var updates []ProgressUpdate
	var mu sync.Mutex
	callback := func(ctx context.Context, update ProgressUpdate) error {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
		return nil
	}

	err := monitor.processLogStream(context.Background(), monitor.log, reader, callback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) < 2 {
		t.Fatalf("expected at least 2 updates, got %d", len(updates))
	}

	// Second update should have 50% estimate.
	last := updates[len(updates)-1]
	if last.PercentComplete != 50 {
		t.Errorf("expected 50%%, got %d", last.PercentComplete)
	}
	if last.FilesProcessed != 200 {
		t.Errorf("expected 200 files, got %d", last.FilesProcessed)
	}
}

func TestProcessLogStream_ParsesRestoreProgress(t *testing.T) {
	lines := strings.Join([]string{
		"Restoring to /data ...",
		"Processed 500 (1.2 GB) of 1000 (2.5 GB) 100.0 MB/s (48.0%) remaining 13s.",
		"Processed 1000 (2.5 GB) of 1000 (2.5 GB) 120.0 MB/s (100.0%) remaining 0s.",
	}, "\n")

	reader := strings.NewReader(lines)
	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   1 * time.Minute,
		updateInterval: 0,
		log:            logrusTestEntry(),
	}

	var updates []ProgressUpdate
	var mu sync.Mutex
	callback := func(ctx context.Context, update ProgressUpdate) error {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
		return nil
	}

	err := monitor.processLogStream(context.Background(), monitor.log, reader, callback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) < 2 {
		t.Fatalf("expected at least 2 restore updates, got %d", len(updates))
	}

	last := updates[len(updates)-1]
	if last.PercentComplete != 100 {
		t.Errorf("expected 100%%, got %d", last.PercentComplete)
	}
	if last.TotalFiles != 1000 {
		t.Errorf("expected 1000 total files, got %d", last.TotalFiles)
	}
	if last.TotalBytes == 0 {
		t.Error("expected non-zero TotalBytes")
	}
}

func TestProcessLogStream_StallDetection(t *testing.T) {
	// Use a slow reader that produces no progress lines.
	reader := &slowReader{
		data:  "Snapshotting mmattox@dr-syncer:/data ...\n",
		delay: 200 * time.Millisecond,
	}

	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   100 * time.Millisecond,
		updateInterval: 0,
		log:            logrusTestEntry(),
	}

	err := monitor.processLogStream(context.Background(), monitor.log, reader, nil)
	if err != ErrStalled {
		t.Errorf("expected ErrStalled, got %v", err)
	}
}

func TestProcessLogStream_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Reader that blocks until context is cancelled.
	reader := &blockingReader{ctx: ctx}

	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   10 * time.Second,
		updateInterval: 0,
		log:            logrusTestEntry(),
	}

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := monitor.processLogStream(ctx, monitor.log, reader, nil)
	if err != nil {
		t.Errorf("expected nil on context cancel, got %v", err)
	}
}

func TestProcessLogStream_CallbackError(t *testing.T) {
	lines := " * 0 hashing, 100 hashed (1.0 GB), 0 cached (0 B), uploaded 500 MB, estimating...\n"
	reader := strings.NewReader(lines)

	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   1 * time.Minute,
		updateInterval: 0,
		log:            logrusTestEntry(),
	}

	callbackErr := fmt.Errorf("status update failed")
	callback := func(ctx context.Context, update ProgressUpdate) error {
		return callbackErr
	}

	err := monitor.processLogStream(context.Background(), monitor.log, reader, callback)
	if err == nil {
		t.Fatal("expected error from callback")
	}
	if !strings.Contains(err.Error(), "status update failed") {
		t.Errorf("expected callback error message, got %v", err)
	}
}

func TestProcessLogStream_EmptyStream(t *testing.T) {
	reader := strings.NewReader("")

	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   1 * time.Minute,
		updateInterval: 0,
		log:            logrusTestEntry(),
	}

	err := monitor.processLogStream(context.Background(), monitor.log, reader, nil)
	if err != nil {
		t.Errorf("expected nil for empty stream, got %v", err)
	}
}

func TestProcessLogStream_UpdateThrottling(t *testing.T) {
	lines := strings.Join([]string{
		" * 0 hashing, 100 hashed (1.0 GB), 0 cached (0 B), uploaded 500 MB, estimating...",
		" * 0 hashing, 200 hashed (2.0 GB), 0 cached (0 B), uploaded 1.0 GB, estimating...",
		" * 0 hashing, 300 hashed (3.0 GB), 0 cached (0 B), uploaded 1.5 GB, estimating...",
	}, "\n")
	reader := strings.NewReader(lines)

	monitor := &KopiaPodProgressMonitor{
		stallTimeout:   1 * time.Minute,
		updateInterval: 1 * time.Hour, // throttle so only first update goes through
		log:            logrusTestEntry(),
	}

	var updates []ProgressUpdate
	callback := func(ctx context.Context, update ProgressUpdate) error {
		updates = append(updates, update)
		return nil
	}

	err := monitor.processLogStream(context.Background(), monitor.log, reader, callback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With extreme throttling, only the first update should go through.
	if len(updates) != 1 {
		t.Errorf("expected 1 update with throttling, got %d", len(updates))
	}
}

// --- NewKopiaPodProgressMonitor tests ---

func TestNewKopiaPodProgressMonitor_Defaults(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	m := NewKopiaPodProgressMonitor(fakeClient)

	if m.stallTimeout != defaultStallTimeout {
		t.Errorf("expected default stall timeout %v, got %v", defaultStallTimeout, m.stallTimeout)
	}
	if m.updateInterval != defaultUpdateInterval {
		t.Errorf("expected default update interval %v, got %v", defaultUpdateInterval, m.updateInterval)
	}
	if m.clientset == nil {
		t.Error("expected non-nil clientset")
	}
}

func TestNewKopiaPodProgressMonitor_Options(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	m := NewKopiaPodProgressMonitor(fakeClient,
		WithStallTimeout(30*time.Second),
		WithUpdateInterval(5*time.Second),
	)

	if m.stallTimeout != 30*time.Second {
		t.Errorf("expected 30s stall timeout, got %v", m.stallTimeout)
	}
	if m.updateInterval != 5*time.Second {
		t.Errorf("expected 5s update interval, got %v", m.updateInterval)
	}
}

// --- Test helpers ---

// logrusTestEntry returns a discarding logrus entry for tests.
func logrusTestEntry() *logrus.Entry {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logrus.NewEntry(logger)
}

// slowReader returns data once, then blocks until stall timeout fires.
type slowReader struct {
	data  string
	delay time.Duration
	sent  bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		n := copy(p, r.data)
		return n, nil
	}
	// Block for delay then return EOF so we don't violate io.Reader contract.
	time.Sleep(r.delay)
	return 0, io.EOF
}

// blockingReader blocks until context is cancelled.
type blockingReader struct {
	ctx context.Context
}

func (r *blockingReader) Read(p []byte) (int, error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

// --- MonitorPodProgress integration tests ---
//
// These test the public MonitorPodProgress entry point end-to-end by using
// a test HTTP server that serves pod log content via the Kubernetes API format.

// newTestClientsetWithLogServer creates a kubernetes.Clientset backed by a test
// HTTP server. The handler receives the log request and can write arbitrary
// log content. Returns the clientset and a cleanup function.
func newTestClientsetWithLogServer(t *testing.T, handler http.HandlerFunc) (kubernetes.Interface, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	clientset, err := kubernetes.NewForConfig(&rest.Config{
		Host: server.URL,
	})
	if err != nil {
		server.Close()
		t.Fatalf("failed to create clientset: %v", err)
	}
	return clientset, server.Close
}

func TestMonitorPodProgress_Success(t *testing.T) {
	logContent := strings.Join([]string{
		"Snapshotting mmattox@dr-syncer:/data ...",
		" * 0 hashing, 100 hashed (1.0 GB), 0 cached (0 B), uploaded 500 MB, estimating...",
		" * 0 hashing, 500 hashed (5.0 GB), 0 cached (0 B), uploaded 2.5 GB, estimated 10.0 GB (50.0%) 5m left",
	}, "\n")

	clientset, cleanup := newTestClientsetWithLogServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify the request is for pod logs.
		if !strings.Contains(r.URL.Path, "/pods/test-pod/log") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(logContent))
	})
	defer cleanup()

	monitor := NewKopiaPodProgressMonitor(clientset,
		WithStallTimeout(5*time.Second),
		WithUpdateInterval(0),
	)

	var updates []ProgressUpdate
	var mu sync.Mutex
	callback := func(ctx context.Context, update ProgressUpdate) error {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
		return nil
	}

	err := monitor.MonitorPodProgress(context.Background(), "test-ns", "test-pod", callback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) < 2 {
		t.Fatalf("expected at least 2 updates, got %d", len(updates))
	}

	last := updates[len(updates)-1]
	if last.PercentComplete != 50 {
		t.Errorf("expected 50%%, got %d", last.PercentComplete)
	}
	if last.FilesProcessed != 500 {
		t.Errorf("expected 500 files, got %d", last.FilesProcessed)
	}
}

func TestMonitorPodProgress_LogStreamError(t *testing.T) {
	clientset, cleanup := newTestClientsetWithLogServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Return an error status for the log request.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","message":"container not found"}`))
	})
	defer cleanup()

	monitor := NewKopiaPodProgressMonitor(clientset,
		WithStallTimeout(5*time.Second),
	)

	err := monitor.MonitorPodProgress(context.Background(), "test-ns", "test-pod", nil)
	if err == nil {
		t.Fatal("expected error when log stream fails")
	}
	if !strings.Contains(err.Error(), "open log stream") {
		t.Errorf("expected 'open log stream' in error, got: %v", err)
	}
}

func TestMonitorPodProgress_StallTimeout(t *testing.T) {
	clientset, cleanup := newTestClientsetWithLogServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Write a non-progress line, then hold the connection open until client disconnects.
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if ok {
			w.Write([]byte("Snapshotting mmattox@dr-syncer:/data ...\n"))
			flusher.Flush()
		}
		// Hold connection open to trigger stall.
		<-r.Context().Done()
	})
	defer cleanup()

	monitor := NewKopiaPodProgressMonitor(clientset,
		WithStallTimeout(100*time.Millisecond),
		WithUpdateInterval(0),
	)

	err := monitor.MonitorPodProgress(context.Background(), "test-ns", "test-pod", nil)
	if err != ErrStalled {
		t.Errorf("expected ErrStalled, got: %v", err)
	}
}

func TestMonitorPodProgress_ContextCancellation(t *testing.T) {
	clientset, cleanup := newTestClientsetWithLogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if ok {
			w.Write([]byte("Starting...\n"))
			flusher.Flush()
		}
		// Hold connection open until client disconnects.
		<-r.Context().Done()
	})
	defer cleanup()

	monitor := NewKopiaPodProgressMonitor(clientset,
		WithStallTimeout(10*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := monitor.MonitorPodProgress(ctx, "test-ns", "test-pod", nil)
	if err != nil {
		t.Errorf("expected nil on context cancel, got: %v", err)
	}
}

func TestMonitorPodProgress_CallbackError(t *testing.T) {
	logContent := " * 0 hashing, 100 hashed (1.0 GB), 0 cached (0 B), uploaded 500 MB, estimating...\n"

	clientset, cleanup := newTestClientsetWithLogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(logContent))
	})
	defer cleanup()

	monitor := NewKopiaPodProgressMonitor(clientset,
		WithStallTimeout(5*time.Second),
		WithUpdateInterval(0),
	)

	callbackErr := fmt.Errorf("status update failed")
	callback := func(ctx context.Context, update ProgressUpdate) error {
		return callbackErr
	}

	err := monitor.MonitorPodProgress(context.Background(), "test-ns", "test-pod", callback)
	if err == nil {
		t.Fatal("expected error from callback")
	}
	if !strings.Contains(err.Error(), "status update failed") {
		t.Errorf("expected 'status update failed' in error, got: %v", err)
	}
}

func TestMonitorPodProgress_RestoreProgress(t *testing.T) {
	logContent := strings.Join([]string{
		"Restoring to /data ...",
		"Processed 500 (1.2 GB) of 1000 (2.5 GB) 100.0 MB/s (48.0%) remaining 13s.",
		"Processed 1000 (2.5 GB) of 1000 (2.5 GB) 120.0 MB/s (100.0%) remaining 0s.",
	}, "\n")

	clientset, cleanup := newTestClientsetWithLogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(logContent))
	})
	defer cleanup()

	monitor := NewKopiaPodProgressMonitor(clientset,
		WithStallTimeout(5*time.Second),
		WithUpdateInterval(0),
	)

	var updates []ProgressUpdate
	var mu sync.Mutex
	callback := func(ctx context.Context, update ProgressUpdate) error {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
		return nil
	}

	err := monitor.MonitorPodProgress(context.Background(), "test-ns", "test-pod", callback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) < 2 {
		t.Fatalf("expected at least 2 restore updates, got %d", len(updates))
	}

	last := updates[len(updates)-1]
	if last.PercentComplete != 100 {
		t.Errorf("expected 100%%, got %d", last.PercentComplete)
	}
	if last.TotalFiles != 1000 {
		t.Errorf("expected 1000 total files, got %d", last.TotalFiles)
	}
}

// --- Interface compliance ---

var _ ProgressMonitor = (*KopiaPodProgressMonitor)(nil)
var _ kubernetes.Interface = (*fake.Clientset)(nil)
