// pkg/version/version.go
package version

import (
	"encoding/json"
	"fmt"
	"runtime"
)

// Variables set during build time via -ldflags
var (
	Version   = "unknown" // The semantic version
	GitCommit = "unknown" // The git commit hash
	BuildTime = "unknown" // The build timestamp
)

// Info holds version details
type Info struct {
	Version    string `json:"version"`
	GitCommit  string `json:"git_commit"`
	BuildTime  string `json:"build_time"`
	GoVersion  string `json:"go_version"`
	GoOS       string `json:"go_os"`
	GoArch     string `json:"go_arch"`
	BuildFlags string `json:"build_flags,omitempty"`
}

// GetVersionInfo returns version details as a struct
func GetVersionInfo() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
		GoOS:      runtime.GOOS,
		GoArch:    runtime.GOARCH,
	}
}

// GetVersionString returns a formatted string with version information
func GetVersionString() string {
	return fmt.Sprintf("Version: %s, GitCommit: %s, BuildTime: %s", Version, GitCommit, BuildTime)
}

// GetVersionJSON returns version information as a JSON string
func GetVersionJSON() string {
	info := GetVersionInfo()
	jsonBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error marshaling version info: %v", err)
	}
	return string(jsonBytes)
}
