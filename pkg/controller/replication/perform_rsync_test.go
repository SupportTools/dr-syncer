package replication

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for isTransientError function

func TestIsTransientError_ErrorPatterns(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "connection refused",
			err:      errors.New("dial tcp 10.0.0.1:22: connection refused"),
			expected: true,
		},
		{
			name:     "connection reset",
			err:      errors.New("read: connection reset by peer"),
			expected: true,
		},
		{
			name:     "connection timed out",
			err:      errors.New("connection timed out after 30s"),
			expected: true,
		},
		{
			name:     "no route to host",
			err:      errors.New("connect: no route to host"),
			expected: true,
		},
		{
			name:     "network unreachable",
			err:      errors.New("network is unreachable"),
			expected: true,
		},
		{
			name:     "i/o timeout",
			err:      errors.New("read tcp: i/o timeout"),
			expected: true,
		},
		{
			name:     "broken pipe",
			err:      errors.New("write: broken pipe"),
			expected: true,
		},
		{
			name:     "EOF",
			err:      errors.New("unexpected EOF"),
			expected: true,
		},
		{
			name:     "temporary failure",
			err:      errors.New("temporary failure in name resolution"),
			expected: true,
		},
		{
			name:     "resource temporarily unavailable",
			err:      errors.New("resource temporarily unavailable"),
			expected: true,
		},
		{
			name:     "permanent error - file not found",
			err:      errors.New("file not found"),
			expected: false,
		},
		{
			name:     "permanent error - permission denied generic",
			err:      errors.New("permission denied: cannot read file"),
			expected: false,
		},
		{
			name:     "permanent error - invalid argument",
			err:      errors.New("invalid argument"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientError(tt.err, "")
			assert.Equal(t, tt.expected, result, "isTransientError(%v, \"\") should return %v", tt.err, tt.expected)
		})
	}
}

func TestIsTransientError_StderrPatterns(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected bool
	}{
		{
			name:     "Connection timed out",
			stderr:   "ssh: connect to host 10.0.0.1 port 22: Connection timed out",
			expected: true,
		},
		{
			name:     "Connection reset by peer",
			stderr:   "Connection reset by peer during read",
			expected: true,
		},
		{
			name:     "Connection closed",
			stderr:   "Connection closed by remote host",
			expected: true,
		},
		{
			name:     "Host key verification failed",
			stderr:   "Host key verification failed.",
			expected: true,
		},
		{
			name:     "Permission denied publickey",
			stderr:   "Permission denied (publickey).",
			expected: true,
		},
		{
			name:     "ssh exchange identification",
			stderr:   "ssh_exchange_identification: Connection closed by remote host",
			expected: true,
		},
		{
			name:     "Read from socket failed",
			stderr:   "Read from socket failed: Connection reset by peer",
			expected: true,
		},
		{
			name:     "Write failed",
			stderr:   "Write failed: Broken pipe",
			expected: true,
		},
		{
			name:     "rsync protocol data stream error",
			stderr:   "rsync error: error in rsync protocol data stream (code 12)",
			expected: true,
		},
		{
			name:     "rsync connection unexpectedly closed",
			stderr:   "rsync: connection unexpectedly closed (0 bytes received)",
			expected: true,
		},
		{
			name:     "normal rsync output - no error",
			stderr:   "sending incremental file list",
			expected: false,
		},
		{
			name:     "rsync file not found",
			stderr:   "rsync: link_stat \"/path/to/file\" failed: No such file or directory",
			expected: false,
		},
		{
			name:     "empty stderr",
			stderr:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientError(nil, tt.stderr)
			assert.Equal(t, tt.expected, result, "isTransientError(nil, %q) should return %v", tt.stderr, tt.expected)
		})
	}
}

func TestIsTransientError_CombinedErrorAndStderr(t *testing.T) {
	// Test that either error or stderr can trigger transient detection
	tests := []struct {
		name     string
		err      error
		stderr   string
		expected bool
	}{
		{
			name:     "transient error, normal stderr",
			err:      errors.New("connection refused"),
			stderr:   "normal output",
			expected: true,
		},
		{
			name:     "normal error, transient stderr",
			err:      errors.New("command failed"),
			stderr:   "Connection timed out",
			expected: true,
		},
		{
			name:     "both transient",
			err:      errors.New("i/o timeout"),
			stderr:   "Connection reset by peer",
			expected: true,
		},
		{
			name:     "neither transient",
			err:      errors.New("invalid input"),
			stderr:   "file not found",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientError(tt.err, tt.stderr)
			assert.Equal(t, tt.expected, result,
				"isTransientError(%v, %q) should return %v", tt.err, tt.stderr, tt.expected)
		})
	}
}

func TestIsTransientError_CaseInsensitiveErrorMatching(t *testing.T) {
	// Error string matching is case-insensitive
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "lowercase",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "uppercase",
			err:      errors.New("CONNECTION REFUSED"),
			expected: true,
		},
		{
			name:     "mixed case",
			err:      errors.New("Connection Refused"),
			expected: true,
		},
		{
			name:     "broken pipe uppercase",
			err:      errors.New("BROKEN PIPE"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientError(tt.err, "")
			assert.Equal(t, tt.expected, result,
				"isTransientError should be case-insensitive for error: %v", tt.err)
		})
	}
}

func TestIsTransientError_StderrIsCaseSensitive(t *testing.T) {
	// Stderr patterns are case-sensitive (matching specific SSH/rsync output)
	tests := []struct {
		name     string
		stderr   string
		expected bool
	}{
		{
			name:     "exact case - Connection timed out",
			stderr:   "Connection timed out",
			expected: true,
		},
		{
			name:     "wrong case - connection timed out",
			stderr:   "connection timed out",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientError(nil, tt.stderr)
			assert.Equal(t, tt.expected, result,
				"Stderr pattern matching is case-sensitive: %q", tt.stderr)
		})
	}
}
