package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Helper to set and restore environment variables
func withEnv(t *testing.T, key, value string) func() {
	t.Helper()
	oldValue, hadValue := os.LookupEnv(key)
	os.Setenv(key, value)
	return func() {
		if hadValue {
			os.Setenv(key, oldValue)
		} else {
			os.Unsetenv(key)
		}
	}
}

// Helper to unset and restore environment variables
func withoutEnv(t *testing.T, key string) func() {
	t.Helper()
	oldValue, hadValue := os.LookupEnv(key)
	os.Unsetenv(key)
	return func() {
		if hadValue {
			os.Setenv(key, oldValue)
		}
	}
}

func TestGetEnvOrDefault_EnvSet(t *testing.T) {
	cleanup := withEnv(t, "TEST_VAR", "custom_value")
	defer cleanup()

	result := getEnvOrDefault("TEST_VAR", "default_value")
	assert.Equal(t, "custom_value", result, "Should return the environment variable value")
}

func TestGetEnvOrDefault_EnvNotSet(t *testing.T) {
	cleanup := withoutEnv(t, "TEST_VAR_UNSET")
	defer cleanup()

	result := getEnvOrDefault("TEST_VAR_UNSET", "default_value")
	assert.Equal(t, "default_value", result, "Should return the default value when env not set")
}

func TestGetEnvOrDefault_EmptyEnv(t *testing.T) {
	cleanup := withEnv(t, "TEST_VAR_EMPTY", "")
	defer cleanup()

	result := getEnvOrDefault("TEST_VAR_EMPTY", "default_value")
	assert.Equal(t, "", result, "Should return empty string if env is set to empty")
}

func TestParseEnvBool_TruthyValues(t *testing.T) {
	truthyValues := []string{"1", "true", "True", "TRUE", "yes", "Yes", "YES", "on", "ON", "enabled", "ENABLED"}

	for _, value := range truthyValues {
		cleanup := withEnv(t, "TEST_BOOL", value)
		result := parseEnvBool("TEST_BOOL", false)
		cleanup()
		assert.True(t, result, "Value '%s' should be parsed as true", value)
	}
}

func TestParseEnvBool_FalsyValues(t *testing.T) {
	falsyValues := []string{"0", "false", "False", "FALSE", "no", "No", "NO", "off", "OFF", "disabled", "DISABLED"}

	for _, value := range falsyValues {
		cleanup := withEnv(t, "TEST_BOOL", value)
		result := parseEnvBool("TEST_BOOL", true)
		cleanup()
		assert.False(t, result, "Value '%s' should be parsed as false", value)
	}
}

func TestParseEnvBool_NotSet(t *testing.T) {
	cleanup := withoutEnv(t, "TEST_BOOL_NOTSET")
	defer cleanup()

	resultTrue := parseEnvBool("TEST_BOOL_NOTSET", true)
	assert.True(t, resultTrue, "Should return default true when not set")

	resultFalse := parseEnvBool("TEST_BOOL_NOTSET", false)
	assert.False(t, resultFalse, "Should return default false when not set")
}

func TestParseEnvBool_InvalidValue(t *testing.T) {
	cleanup := withEnv(t, "TEST_BOOL_INVALID", "invalid")
	defer cleanup()

	result := parseEnvBool("TEST_BOOL_INVALID", true)
	assert.True(t, result, "Should return default value for invalid input")
}

func TestParseEnvDuration_ValidDurations(t *testing.T) {
	testCases := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"30s", 30 * time.Second},
		{"1h30m", 90 * time.Minute},
		{"100ms", 100 * time.Millisecond},
	}

	for _, tc := range testCases {
		cleanup := withEnv(t, "TEST_DURATION", tc.input)
		result := parseEnvDuration("TEST_DURATION", "1m")
		cleanup()
		assert.Equal(t, tc.expected, result, "Duration '%s' should parse correctly", tc.input)
	}
}

func TestParseEnvDuration_NotSet(t *testing.T) {
	cleanup := withoutEnv(t, "TEST_DURATION_NOTSET")
	defer cleanup()

	result := parseEnvDuration("TEST_DURATION_NOTSET", "10m")
	assert.Equal(t, 10*time.Minute, result, "Should return default duration when not set")
}

func TestParseEnvDuration_InvalidValue(t *testing.T) {
	cleanup := withEnv(t, "TEST_DURATION_INVALID", "invalid")
	defer cleanup()

	result := parseEnvDuration("TEST_DURATION_INVALID", "5m")
	assert.Equal(t, 5*time.Minute, result, "Should return default duration for invalid input")
}

func TestParseEnvInt_ValidIntegers(t *testing.T) {
	testCases := []struct {
		input    string
		expected int
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"-1", -1},
		{"100", 100},
	}

	for _, tc := range testCases {
		cleanup := withEnv(t, "TEST_INT", tc.input)
		result := parseEnvInt("TEST_INT", 999)
		cleanup()
		assert.Equal(t, tc.expected, result, "Integer '%s' should parse correctly", tc.input)
	}
}

func TestParseEnvInt_NotSet(t *testing.T) {
	cleanup := withoutEnv(t, "TEST_INT_NOTSET")
	defer cleanup()

	result := parseEnvInt("TEST_INT_NOTSET", 42)
	assert.Equal(t, 42, result, "Should return default int when not set")
}

func TestParseEnvInt_InvalidValue(t *testing.T) {
	cleanup := withEnv(t, "TEST_INT_INVALID", "not_a_number")
	defer cleanup()

	result := parseEnvInt("TEST_INT_INVALID", 99)
	assert.Equal(t, 99, result, "Should return default int for invalid input")
}

func TestParseEnvInt_FloatValue(t *testing.T) {
	cleanup := withEnv(t, "TEST_INT_FLOAT", "3.14")
	defer cleanup()

	result := parseEnvInt("TEST_INT_FLOAT", 0)
	assert.Equal(t, 0, result, "Should return default for float input")
}

func TestLoadConfiguration_Defaults(t *testing.T) {
	// Clear relevant environment variables
	envVars := []string{
		"KUBECONFIG", "SYNC_INTERVAL", "RESYNC_PERIOD", "LOG_VERBOSITY",
		"METRICS_ADDR", "PROBE_ADDR", "ENABLE_LEADER_ELECTION",
		"LEADER_ELECTION_ID", "LOG_LEVEL", "IGNORE_CERT",
	}

	cleanups := make([]func(), 0, len(envVars))
	for _, env := range envVars {
		cleanups = append(cleanups, withoutEnv(t, env))
	}
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()

	LoadConfiguration()

	// Verify defaults
	assert.Equal(t, "", CFG.KubeConfig)
	assert.Equal(t, 5*time.Minute, CFG.SyncInterval)
	assert.Equal(t, time.Hour, CFG.ResyncPeriod)
	assert.Equal(t, 0, CFG.LogVerbosity)
	assert.Equal(t, ":8080", CFG.MetricsAddr)
	assert.Equal(t, ":8081", CFG.ProbeAddr)
	assert.False(t, CFG.EnableLeaderElection)
	assert.Equal(t, "dr-syncer.io", CFG.LeaderElectionID)
	assert.Equal(t, "info", CFG.LogLevel)
	assert.False(t, CFG.IgnoreCert)
}

func TestLoadConfiguration_CustomValues(t *testing.T) {
	// Set custom environment variables
	cleanups := []func(){
		withEnv(t, "KUBECONFIG", "/custom/path/kubeconfig"),
		withEnv(t, "SYNC_INTERVAL", "10m"),
		withEnv(t, "RESYNC_PERIOD", "2h"),
		withEnv(t, "LOG_VERBOSITY", "5"),
		withEnv(t, "METRICS_ADDR", ":9090"),
		withEnv(t, "PROBE_ADDR", ":9091"),
		withEnv(t, "ENABLE_LEADER_ELECTION", "true"),
		withEnv(t, "LEADER_ELECTION_ID", "custom-leader-id"),
		withEnv(t, "LOG_LEVEL", "debug"),
		withEnv(t, "IGNORE_CERT", "yes"),
	}
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()

	LoadConfiguration()

	assert.Equal(t, "/custom/path/kubeconfig", CFG.KubeConfig)
	assert.Equal(t, 10*time.Minute, CFG.SyncInterval)
	assert.Equal(t, 2*time.Hour, CFG.ResyncPeriod)
	assert.Equal(t, 5, CFG.LogVerbosity)
	assert.Equal(t, ":9090", CFG.MetricsAddr)
	assert.Equal(t, ":9091", CFG.ProbeAddr)
	assert.True(t, CFG.EnableLeaderElection)
	assert.Equal(t, "custom-leader-id", CFG.LeaderElectionID)
	assert.Equal(t, "debug", CFG.LogLevel)
	assert.True(t, CFG.IgnoreCert)
}
