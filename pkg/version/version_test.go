package version

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetVersionInfo(t *testing.T) {
	info := GetVersionInfo()

	// Verify runtime information is populated correctly
	assert.Equal(t, runtime.Version(), info.GoVersion, "GoVersion should match runtime.Version()")
	assert.Equal(t, runtime.GOOS, info.GoOS, "GoOS should match runtime.GOOS")
	assert.Equal(t, runtime.GOARCH, info.GoArch, "GoArch should match runtime.GOARCH")

	// Verify version variables are set (will be "unknown" in test environment)
	assert.NotEmpty(t, info.Version, "Version should not be empty")
	assert.NotEmpty(t, info.GitCommit, "GitCommit should not be empty")
	assert.NotEmpty(t, info.BuildTime, "BuildTime should not be empty")
}

func TestGetVersionString(t *testing.T) {
	versionStr := GetVersionString()

	// Verify the string contains expected components
	assert.Contains(t, versionStr, "Version:", "Should contain Version label")
	assert.Contains(t, versionStr, "GitCommit:", "Should contain GitCommit label")
	assert.Contains(t, versionStr, "BuildTime:", "Should contain BuildTime label")

	// Verify format matches expected pattern
	assert.True(t, strings.HasPrefix(versionStr, "Version:"), "Should start with Version:")
}

func TestGetVersionJSON(t *testing.T) {
	jsonStr := GetVersionJSON()

	// Verify it's valid JSON
	require.NotEmpty(t, jsonStr, "JSON string should not be empty")

	var info Info
	err := json.Unmarshal([]byte(jsonStr), &info)
	require.NoError(t, err, "Should be valid JSON that unmarshals to Info struct")

	// Verify runtime information is in the JSON
	assert.Equal(t, runtime.Version(), info.GoVersion, "GoVersion in JSON should match runtime")
	assert.Equal(t, runtime.GOOS, info.GoOS, "GoOS in JSON should match runtime")
	assert.Equal(t, runtime.GOARCH, info.GoArch, "GoArch in JSON should match runtime")
}

func TestVersionVariablesDefaults(t *testing.T) {
	// Test that default values are set when not overridden by build flags
	// In test environment, these should be "unknown"
	assert.Equal(t, "unknown", Version, "Default Version should be 'unknown'")
	assert.Equal(t, "unknown", GitCommit, "Default GitCommit should be 'unknown'")
	assert.Equal(t, "unknown", BuildTime, "Default BuildTime should be 'unknown'")
}

func TestInfoStruct(t *testing.T) {
	// Test Info struct JSON tags and fields
	info := Info{
		Version:    "1.0.0",
		GitCommit:  "abc123",
		BuildTime:  "2024-01-01T00:00:00Z",
		GoVersion:  "go1.21.0",
		GoOS:       "linux",
		GoArch:     "amd64",
		BuildFlags: "-ldflags",
	}

	// Marshal and unmarshal to verify JSON tags work correctly
	jsonBytes, err := json.Marshal(info)
	require.NoError(t, err, "Should marshal Info struct to JSON")

	var unmarshaled Info
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	require.NoError(t, err, "Should unmarshal JSON back to Info struct")

	assert.Equal(t, info, unmarshaled, "Unmarshaled struct should match original")
}
