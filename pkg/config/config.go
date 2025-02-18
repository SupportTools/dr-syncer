package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// AppConfig defines the structure for application configuration loaded from environment variables.
type AppConfig struct {
	KubeConfig           string        `json:"kubeConfig"`       // Path to kubeconfig file
	SyncInterval         time.Duration `json:"syncInterval"`     // Interval between sync operations
	ResyncPeriod         time.Duration `json:"resyncPeriod"`     // Period for full resync of resources
	MetricsAddr          string        `json:"metricsAddr"`      // The address the metric endpoint binds to
	ProbeAddr            string        `json:"probeAddr"`        // The address the probe endpoint binds to
	EnableLeaderElection bool          `json:"leaderElection"`   // Enable leader election for controller manager
	LeaderElectionID     string        `json:"leaderElectionId"` // ID for leader election
	LogLevel             string        `json:"logLevel"`         // Log level for the application
	IgnoreCert           bool          `json:"ignoreCert"`       // Ignore certificate errors
}

// CFG is the global configuration instance.
var CFG AppConfig

// LoadConfiguration loads the configuration from environment variables and validates it.
func LoadConfiguration() {
	// Load environment variables with defaults
	CFG.KubeConfig = getEnvOrDefault("KUBECONFIG", "")
	CFG.SyncInterval = parseEnvDuration("SYNC_INTERVAL", "5m")
	CFG.ResyncPeriod = parseEnvDuration("RESYNC_PERIOD", "1h")
	CFG.MetricsAddr = getEnvOrDefault("METRICS_ADDR", ":8080")
	CFG.ProbeAddr = getEnvOrDefault("PROBE_ADDR", ":8081")
	CFG.EnableLeaderElection = parseEnvBool("ENABLE_LEADER_ELECTION", false)
	CFG.LeaderElectionID = getEnvOrDefault("LEADER_ELECTION_ID", "dr-syncer.io")
	CFG.LogLevel = getEnvOrDefault("LOG_LEVEL", "info")
	CFG.IgnoreCert = parseEnvBool("IGNORE_CERT", false)
}

// getEnvOrDefault retrieves the value of an environment variable or returns a default value if not set.
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	log.Printf("Environment variable %s not set. Using default: %s", key, defaultValue)
	return defaultValue
}

// parseEnvBool parses an environment variable as a boolean, supporting common truthy/falsy values.
func parseEnvBool(key string, defaultValue bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Printf("Environment variable %s not set. Using default: %t", key, defaultValue)
		return defaultValue
	}

	value = strings.ToLower(value)
	switch value {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		boolValue, err := strconv.ParseBool(value)
		if err != nil {
			log.Printf("Error parsing %s as bool: %v. Using default value: %t", key, err, defaultValue)
			return defaultValue
		}
		return boolValue
	}
}

// parseEnvDuration parses an environment variable as a time.Duration, returning a default value if parsing fails.
func parseEnvDuration(key, defaultValue string) time.Duration {
	value := getEnvOrDefault(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("Error parsing %s as duration: %v. Using default: %s", key, err, defaultValue)
		duration, _ = time.ParseDuration(defaultValue)
	}
	return duration
}
