package rsync

import (
	"fmt"
	"os/exec"
	"strings"
)

// Options represents rsync operation options
type Options struct {
	Source      string
	Destination string
	Exclude     []string
	Delete      bool
	Compress    bool
	Archive     bool
	Verbose     bool
}

// DefaultOptions returns default rsync options
func DefaultOptions() Options {
	return Options{
		Archive:  true,  // -a: archive mode
		Delete:   true,  // --delete: delete extraneous files from dest dirs
		Compress: true,  // -z: compress file data during transfer
		Verbose:  false, // -v: increase verbosity
	}
}

// Sync performs an rsync operation with the given options
func Sync(opts Options) error {
	args := buildRsyncArgs(opts)

	cmd := exec.Command("rsync", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync failed: %v, output: %s", err, string(output))
	}

	return nil
}

// buildRsyncArgs constructs the rsync command arguments
func buildRsyncArgs(opts Options) []string {
	var args []string

	// Base options
	if opts.Archive {
		args = append(args, "-a")
	}
	if opts.Delete {
		args = append(args, "--delete")
	}
	if opts.Compress {
		args = append(args, "-z")
	}
	if opts.Verbose {
		args = append(args, "-v")
	}

	// Add exclude patterns
	for _, exclude := range opts.Exclude {
		args = append(args, "--exclude", exclude)
	}

	// Add progress reporting
	args = append(args, "--progress")

	// Add source and destination
	args = append(args, opts.Source)
	args = append(args, opts.Destination)

	return args
}

// ValidateOptions checks if the provided options are valid
func ValidateOptions(opts Options) error {
	if opts.Source == "" {
		return fmt.Errorf("source path is required")
	}
	if opts.Destination == "" {
		return fmt.Errorf("destination path is required")
	}

	// Validate source/destination format for remote paths
	if strings.Contains(opts.Source, ":") {
		parts := strings.Split(opts.Source, ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid source path format: %s", opts.Source)
		}
	}
	if strings.Contains(opts.Destination, ":") {
		parts := strings.Split(opts.Destination, ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid destination path format: %s", opts.Destination)
		}
	}

	return nil
}
