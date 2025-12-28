package replication

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRsyncOutput_SentBytes(t *testing.T) {
	output := `
sending incremental file list
./
file1.txt

Number of regular files transferred: 1

sent 1,234 bytes  received 35 bytes  2,538.00 bytes/sec
total size is 1,000  speedup is 0.79
`
	bytes, files, progress, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, int64(1234), bytes, "Should parse sent bytes with commas")
	assert.Equal(t, 1, files, "Should parse files transferred count")
	assert.Equal(t, 100, progress, "Should set progress to 100 when speedup is present")
}

func TestParseRsyncOutput_LargeSentBytes(t *testing.T) {
	output := `
sent 1,234,567,890 bytes  received 100 bytes  1.23 bytes/sec
total size is 1,234,567,890  speedup is 1.00
`
	bytes, _, _, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, int64(1234567890), bytes, "Should parse large sent bytes with commas")
}

func TestParseRsyncOutput_FileCount(t *testing.T) {
	output := `
Number of files: 100
Number of regular files transferred: 50
Number of created files: 10

sent 10,000 bytes  received 500 bytes  10.00 bytes/sec
total size is 10,000  speedup is 0.95
`
	_, files, _, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, 50, files, "Should parse files transferred count")
}

func TestParseRsyncOutput_FileCountFromProgressLines(t *testing.T) {
	output := `
sending incremental file list
file1.txt
          100  50%    0.00kB/s    0:00:00 (xfr#1, to-chk=2/3)
file2.txt
          200 100%    0.00kB/s    0:00:00 (xfr#2, to-chk=1/3)
file3.txt
          300 100%    0.00kB/s    0:00:00 (xfr#3, to-chk=0/3)

sent 1,000 bytes  received 100 bytes  1.00 bytes/sec
`
	_, files, _, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, 3, files, "Should count files from progress lines when no explicit count")
}

func TestParseRsyncOutput_Progress100WhenComplete(t *testing.T) {
	output := `
Number of regular files transferred: 5

sent 5,000 bytes  received 200 bytes  5.00 bytes/sec
total size is 5,000  speedup is 0.96
`
	_, files, progress, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, 5, files, "Should parse files transferred count")
	assert.Equal(t, 100, progress, "Should set progress to 100 when total size is present")
}

func TestParseRsyncOutput_ProgressSpeedupIndicator(t *testing.T) {
	output := `
Number of regular files transferred: 10
sent 5,000 bytes  received 200 bytes  5.00 bytes/sec
speedup is 0.96
`
	_, _, progress, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, 100, progress, "Should set progress to 100 when speedup is present")
}

func TestParseRsyncOutput_EmptyOutput(t *testing.T) {
	bytes, files, progress, err := ParseRsyncOutput("")

	assert.NoError(t, err)
	assert.Equal(t, int64(0), bytes)
	assert.Equal(t, 0, files)
	assert.Equal(t, 0, progress)
}

func TestParseRsyncOutput_NoMatchingPatterns(t *testing.T) {
	output := `
some random output
that doesn't match
any patterns
`
	bytes, files, progress, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, int64(0), bytes)
	assert.Equal(t, 0, files)
	assert.Equal(t, 0, progress)
}

func TestParseRsyncOutput_OnlyReceivedBytes(t *testing.T) {
	// When only received bytes are present (shouldn't add to bytesTransferred)
	output := `
received 5,000 bytes
`
	bytes, _, _, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, int64(0), bytes, "Should not count received bytes as transferred")
}

func TestParseRsyncOutput_ToCheckPattern(t *testing.T) {
	output := `
file1.txt
          100  50%    0.00kB/s    0:00:00 (xfr#1, to-check=2/3)
file2.txt
          200 100%    0.00kB/s    0:00:00 (xfr#2, to-check=1/3)
`
	_, files, _, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, 2, files, "Should count files with to-check pattern")
}

func TestMin(t *testing.T) {
	testCases := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-1, 1, -1},
		{100, 95, 95},
		{95, 100, 95},
	}

	for _, tc := range testCases {
		result := min(tc.a, tc.b)
		assert.Equal(t, tc.expected, result, "min(%d, %d) should be %d", tc.a, tc.b, tc.expected)
	}
}

func TestSyncStatus_Struct(t *testing.T) {
	// Test that SyncStatus struct is properly initialized
	status := SyncStatus{
		Phase:            "Running",
		BytesTransferred: 1000,
		FilesTransferred: 10,
		Progress:         50,
		Error:            "",
	}

	assert.Equal(t, "Running", status.Phase)
	assert.Equal(t, int64(1000), status.BytesTransferred)
	assert.Equal(t, 10, status.FilesTransferred)
	assert.Equal(t, 50, status.Progress)
	assert.Empty(t, status.Error)
}

func TestSyncStatus_WithError(t *testing.T) {
	status := SyncStatus{
		Phase:            "Failed",
		BytesTransferred: 0,
		FilesTransferred: 0,
		Progress:         0,
		Error:            "connection refused",
	}

	assert.Equal(t, "Failed", status.Phase)
	assert.Equal(t, "connection refused", status.Error)
}

func TestParseRsyncOutput_RealWorldExample(t *testing.T) {
	// Real-world rsync output example
	output := `
sending incremental file list
data/
data/file1.txt
data/file2.txt
data/subdir/
data/subdir/file3.txt

Number of files: 5 (reg: 3, dir: 2)
Number of created files: 0
Number of deleted files: 0
Number of regular files transferred: 3
Total file size: 1,234,567 bytes
Total transferred file size: 1,234,567 bytes
Literal data: 1,234,567 bytes
Matched data: 0 bytes
File list size: 123
File list generation time: 0.001 seconds
File list transfer time: 0.000 seconds
Total bytes sent: 1,234,890
Total bytes received: 78

sent 1,234,890 bytes  received 78 bytes  2,469,936.00 bytes/sec
total size is 1,234,567  speedup is 1.00
`
	bytes, files, progress, err := ParseRsyncOutput(output)

	assert.NoError(t, err)
	assert.Equal(t, int64(1234890), bytes, "Should parse sent bytes from real output")
	assert.Equal(t, 3, files, "Should parse files transferred from real output")
	assert.Equal(t, 100, progress, "Should set progress to 100 for completed transfer")
}
