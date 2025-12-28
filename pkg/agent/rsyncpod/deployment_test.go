package rsyncpod

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for PodType constants
func TestPodType_Constants(t *testing.T) {
	assert.Equal(t, PodType("source"), SourcePodType)
	assert.Equal(t, PodType("destination"), DestinationPodType)
}

func TestPodType_String(t *testing.T) {
	assert.Equal(t, "source", string(SourcePodType))
	assert.Equal(t, "destination", string(DestinationPodType))
}

// Tests for RsyncPodOptions struct
func TestRsyncPodOptions_Struct(t *testing.T) {
	opts := RsyncPodOptions{
		Namespace:       "test-ns",
		PVCName:         "test-pvc",
		NodeName:        "node-1",
		Type:            SourcePodType,
		SyncID:          "sync-123",
		ReplicationName: "repl-1",
		DestinationInfo: "dest-cluster",
	}

	assert.Equal(t, "test-ns", opts.Namespace)
	assert.Equal(t, "test-pvc", opts.PVCName)
	assert.Equal(t, "node-1", opts.NodeName)
	assert.Equal(t, SourcePodType, opts.Type)
	assert.Equal(t, "sync-123", opts.SyncID)
	assert.Equal(t, "repl-1", opts.ReplicationName)
	assert.Equal(t, "dest-cluster", opts.DestinationInfo)
}

func TestRsyncPodOptions_MinimalFields(t *testing.T) {
	// Test with only required fields
	opts := RsyncPodOptions{
		Namespace: "ns",
		PVCName:   "pvc",
	}

	assert.Equal(t, "ns", opts.Namespace)
	assert.Equal(t, "pvc", opts.PVCName)
	assert.Empty(t, opts.NodeName)
	assert.Empty(t, string(opts.Type))
	assert.Empty(t, opts.SyncID)
}

func TestRsyncPodOptions_SourceType(t *testing.T) {
	opts := RsyncPodOptions{
		Namespace: "test-ns",
		PVCName:   "test-pvc",
		Type:      SourcePodType,
	}

	assert.Equal(t, SourcePodType, opts.Type)
	assert.Equal(t, "source", string(opts.Type))
}

func TestRsyncPodOptions_DestinationType(t *testing.T) {
	opts := RsyncPodOptions{
		Namespace: "test-ns",
		PVCName:   "test-pvc",
		Type:      DestinationPodType,
	}

	assert.Equal(t, DestinationPodType, opts.Type)
	assert.Equal(t, "destination", string(opts.Type))
}

// Tests for Manager struct
func TestManager_Struct(t *testing.T) {
	manager := &Manager{}
	assert.NotNil(t, manager)
	assert.Nil(t, manager.client)
}

// Tests for RsyncDeployment struct
func TestRsyncDeployment_Struct(t *testing.T) {
	deployment := &RsyncDeployment{
		Name:      "test-deployment",
		Namespace: "test-ns",
		PodName:   "test-pod-abc123",
		PVCName:   "test-pvc",
		SyncID:    "sync-456",
	}

	assert.Equal(t, "test-deployment", deployment.Name)
	assert.Equal(t, "test-ns", deployment.Namespace)
	assert.Equal(t, "test-pod-abc123", deployment.PodName)
	assert.Equal(t, "test-pvc", deployment.PVCName)
	assert.Equal(t, "sync-456", deployment.SyncID)
	assert.Nil(t, deployment.client)
}

func TestRsyncDeployment_EmptyFields(t *testing.T) {
	deployment := &RsyncDeployment{}

	assert.Empty(t, deployment.Name)
	assert.Empty(t, deployment.Namespace)
	assert.Empty(t, deployment.PodName)
	assert.Empty(t, deployment.PVCName)
	assert.Empty(t, deployment.SyncID)
}

// Tests for RetryableError
func TestRetryableError_Error(t *testing.T) {
	err := &RetryableError{Err: assert.AnError}
	assert.Equal(t, assert.AnError.Error(), err.Error())
}

func TestRetryableError_NilErr(t *testing.T) {
	// Testing that Error() panics with nil Err
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for nil Err, but didn't panic")
		}
	}()

	err := &RetryableError{Err: nil}
	_ = err.Error() // Should panic
}

func TestRetryableError_CustomError(t *testing.T) {
	customErr := &RetryableError{Err: assert.AnError}
	assert.Contains(t, customErr.Error(), "assert.AnError")
}

func TestRetryableError_ConnectionRefused(t *testing.T) {
	err := &RetryableError{Err: assert.AnError}
	assert.NotNil(t, err)
	assert.NotEmpty(t, err.Error())
}

// Tests for sanitizeNameForLabel
func TestSanitizeNameForLabel_NoChanges(t *testing.T) {
	// Valid name should not be modified
	result := sanitizeNameForLabel("valid-name")
	assert.Equal(t, "valid-name", result)
}

func TestSanitizeNameForLabel_WithSlash(t *testing.T) {
	result := sanitizeNameForLabel("name/with/slashes")
	assert.Equal(t, "name-with-slashes", result)
}

func TestSanitizeNameForLabel_WithDot(t *testing.T) {
	result := sanitizeNameForLabel("name.with.dots")
	assert.Equal(t, "name-with-dots", result)
}

func TestSanitizeNameForLabel_WithColon(t *testing.T) {
	result := sanitizeNameForLabel("name:with:colons")
	assert.Equal(t, "name-with-colons", result)
}

func TestSanitizeNameForLabel_WithTilde(t *testing.T) {
	result := sanitizeNameForLabel("name~with~tildes")
	assert.Equal(t, "name-with-tildes", result)
}

func TestSanitizeNameForLabel_MixedInvalidChars(t *testing.T) {
	result := sanitizeNameForLabel("name/with.mixed:chars~here")
	assert.Equal(t, "name-with-mixed-chars-here", result)
}

func TestSanitizeNameForLabel_StartsWithInvalidChar(t *testing.T) {
	result := sanitizeNameForLabel("/startswithslash")
	assert.Equal(t, "-startswithslash", result)
}

func TestSanitizeNameForLabel_EndsWithInvalidChar(t *testing.T) {
	result := sanitizeNameForLabel("endswithslash/")
	assert.Equal(t, "endswithslash-", result)
}

func TestSanitizeNameForLabel_MaxLength(t *testing.T) {
	// Create a string longer than 63 characters
	longName := "this-is-a-very-long-name-that-exceeds-the-kubernetes-label-value-limit-of-63-characters"
	result := sanitizeNameForLabel(longName)
	assert.Len(t, result, 63)
	assert.Equal(t, longName[:63], result)
}

func TestSanitizeNameForLabel_ExactlyMaxLength(t *testing.T) {
	// Create a string exactly 63 characters
	exactName := "this-is-exactly-63-characters-long-for-testing-the-limit-here"
	assert.Len(t, exactName, 61)
	// Pad to exactly 63
	exactName = exactName + "ab"
	assert.Len(t, exactName, 63)

	result := sanitizeNameForLabel(exactName)
	assert.Len(t, result, 63)
	assert.Equal(t, exactName, result)
}

func TestSanitizeNameForLabel_Empty(t *testing.T) {
	result := sanitizeNameForLabel("")
	assert.Equal(t, "", result)
}

func TestSanitizeNameForLabel_OnlyInvalidChars(t *testing.T) {
	result := sanitizeNameForLabel("/./:")
	assert.Equal(t, "----", result)
}

func TestSanitizeNameForLabel_RealisticPVCNames(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"my-app-data", "my-app-data"},
		{"pvc-12345678-abcd-1234-5678-123456789abc", "pvc-12345678-abcd-1234-5678-123456789abc"},
		{"nfs/share", "nfs-share"},
		{"data.v1", "data-v1"},
		{"app:v2", "app-v2"},
		{"my~backup", "my-backup"},
	}

	for _, tc := range testCases {
		result := sanitizeNameForLabel(tc.input)
		assert.Equal(t, tc.expected, result, "Input: %s", tc.input)
	}
}

func TestSanitizeNameForLabel_ConsecutiveInvalidChars(t *testing.T) {
	result := sanitizeNameForLabel("name//double")
	assert.Equal(t, "name--double", result)
}

func TestSanitizeNameForLabel_AllInvalidCharsTypes(t *testing.T) {
	result := sanitizeNameForLabel("a/b.c:d~e")
	assert.Equal(t, "a-b-c-d-e", result)
}

// Tests for OutputCapture
func TestOutputCapture_Write(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stdout",
		podName:   "test-pod",
		namespace: "test-ns",
		command:   "ls -la",
	}

	data := []byte("test output")
	n, err := capture.Write(data)

	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, "test output", buf.String())
}

func TestOutputCapture_WriteMultiple(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stdout",
		podName:   "test-pod",
		namespace: "test-ns",
		command:   "cat file",
	}

	_, _ = capture.Write([]byte("line1\n"))
	_, _ = capture.Write([]byte("line2\n"))
	_, _ = capture.Write([]byte("line3\n"))

	assert.Equal(t, "line1\nline2\nline3\n", buf.String())
}

func TestOutputCapture_WriteEmpty(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stdout",
		podName:   "test-pod",
		namespace: "test-ns",
		command:   "echo",
	}

	n, err := capture.Write([]byte{})

	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, buf.String())
}

func TestOutputCapture_StdoutType(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stdout",
		podName:   "test-pod",
		namespace: "test-ns",
		command:   "test",
	}

	assert.Equal(t, "stdout", capture.kind)
}

func TestOutputCapture_StderrType(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stderr",
		podName:   "test-pod",
		namespace: "test-ns",
		command:   "test",
	}

	assert.Equal(t, "stderr", capture.kind)
}

func TestOutputCapture_Fields(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stdout",
		podName:   "my-rsync-pod-abc",
		namespace: "production",
		command:   "rsync -avz /data/ dest:/backup/",
	}

	assert.Equal(t, "stdout", capture.kind)
	assert.Equal(t, "my-rsync-pod-abc", capture.podName)
	assert.Equal(t, "production", capture.namespace)
	assert.Equal(t, "rsync -avz /data/ dest:/backup/", capture.command)
}

func TestOutputCapture_WriteLargeData(t *testing.T) {
	buf := &bytes.Buffer{}
	capture := &OutputCapture{
		buffer:    buf,
		kind:      "stdout",
		podName:   "test-pod",
		namespace: "test-ns",
		command:   "large-output",
	}

	// Create 1MB of data
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = 'x'
	}

	n, err := capture.Write(largeData)

	assert.NoError(t, err)
	assert.Equal(t, len(largeData), n)
	assert.Len(t, buf.String(), len(largeData))
}

// Tests for RsyncDeployment methods with nil pod
func TestRsyncDeployment_NoPodName(t *testing.T) {
	deployment := &RsyncDeployment{
		Name:      "test-deployment",
		Namespace: "test-ns",
		PodName:   "", // No pod name set
	}

	assert.Empty(t, deployment.PodName)
}

// Tests for realistic sync scenarios
func TestRsyncPodOptions_RealisticSource(t *testing.T) {
	opts := RsyncPodOptions{
		Namespace:       "production",
		PVCName:         "app-data-pvc",
		NodeName:        "worker-node-1",
		Type:            SourcePodType,
		SyncID:          "sync-20231215-001",
		ReplicationName: "prod-to-dr",
		DestinationInfo: "dr-cluster.example.com",
	}

	assert.Equal(t, "production", opts.Namespace)
	assert.Equal(t, "app-data-pvc", opts.PVCName)
	assert.Equal(t, "worker-node-1", opts.NodeName)
	assert.Equal(t, SourcePodType, opts.Type)
}

func TestRsyncPodOptions_RealisticDestination(t *testing.T) {
	opts := RsyncPodOptions{
		Namespace:       "dr-production",
		PVCName:         "app-data-pvc",
		NodeName:        "dr-worker-1",
		Type:            DestinationPodType,
		SyncID:          "sync-20231215-001",
		ReplicationName: "prod-to-dr",
		DestinationInfo: "",
	}

	assert.Equal(t, "dr-production", opts.Namespace)
	assert.Equal(t, DestinationPodType, opts.Type)
}

// Tests for RsyncDeployment field interactions
func TestRsyncDeployment_AllFieldsSet(t *testing.T) {
	deployment := &RsyncDeployment{
		Name:      "dr-syncer-app-data-pvc-abc12345",
		Namespace: "production",
		PodName:   "dr-syncer-app-data-pvc-abc12345-xyz98765",
		PVCName:   "app-data-pvc",
		SyncID:    "sync-20231215-001",
	}

	// Verify all fields are accessible
	assert.Contains(t, deployment.Name, "dr-syncer")
	assert.Contains(t, deployment.Name, "app-data-pvc")
	assert.Equal(t, "production", deployment.Namespace)
	assert.Contains(t, deployment.PodName, deployment.Name)
}

// Test sanitization with long PVC names
func TestSanitizeNameForLabel_LongPVCName(t *testing.T) {
	// This simulates a realistic long PVC name
	longPVCName := "very-long-pvc-name-that-might-be-generated-by-a-statefulset-with-a-very-long-application-name-and-replica-index"
	result := sanitizeNameForLabel(longPVCName)

	assert.LessOrEqual(t, len(result), 63)
	assert.NotEmpty(t, result)
}

// Test multiple OutputCapture instances
func TestOutputCapture_MultipleInstances(t *testing.T) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	stdout := &OutputCapture{
		buffer:    stdoutBuf,
		kind:      "stdout",
		podName:   "pod-1",
		namespace: "ns-1",
		command:   "cmd",
	}

	stderr := &OutputCapture{
		buffer:    stderrBuf,
		kind:      "stderr",
		podName:   "pod-1",
		namespace: "ns-1",
		command:   "cmd",
	}

	stdout.Write([]byte("stdout output"))
	stderr.Write([]byte("stderr output"))

	assert.Equal(t, "stdout output", stdoutBuf.String())
	assert.Equal(t, "stderr output", stderrBuf.String())
}

// Test PodType in switch/case scenarios
func TestPodType_SwitchCase(t *testing.T) {
	testCases := []struct {
		podType  PodType
		expected string
	}{
		{SourcePodType, "source"},
		{DestinationPodType, "destination"},
	}

	for _, tc := range testCases {
		var result string
		switch tc.podType {
		case SourcePodType:
			result = "source"
		case DestinationPodType:
			result = "destination"
		default:
			result = "unknown"
		}
		assert.Equal(t, tc.expected, result)
	}
}

// Tests for edge cases in sanitization
func TestSanitizeNameForLabel_SingleChar(t *testing.T) {
	assert.Equal(t, "a", sanitizeNameForLabel("a"))
	assert.Equal(t, "-", sanitizeNameForLabel("/"))
	assert.Equal(t, "-", sanitizeNameForLabel("."))
	assert.Equal(t, "-", sanitizeNameForLabel(":"))
	assert.Equal(t, "-", sanitizeNameForLabel("~"))
}

func TestSanitizeNameForLabel_TwoChars(t *testing.T) {
	assert.Equal(t, "ab", sanitizeNameForLabel("ab"))
	assert.Equal(t, "-b", sanitizeNameForLabel("/b"))
	assert.Equal(t, "a-", sanitizeNameForLabel("a/"))
	assert.Equal(t, "--", sanitizeNameForLabel("//"))
}

func TestSanitizeNameForLabel_Unicode(t *testing.T) {
	// Unicode characters should be preserved (they're not in the invalidChars list)
	result := sanitizeNameForLabel("test-日本語")
	assert.Equal(t, "test-日本語", result)
}
