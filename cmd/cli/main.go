package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/supporttools/dr-syncer/pkg/cli"
	"github.com/supporttools/dr-syncer/pkg/logging"
	"github.com/supporttools/dr-syncer/pkg/version"
)

func main() {
	// Initialize logging
	log := logging.SetupLogging()

	// Version flag
	showVersion := flag.Bool("version", false, "Display version information")
	showVersionJSON := flag.Bool("version-json", false, "Display version information in JSON format")

	// Required flags
	sourceKubeconfig := flag.String("source-kubeconfig", "", "Path to the source kubeconfig file")
	destKubeconfig := flag.String("dest-kubeconfig", "", "Path to the destination kubeconfig file")
	sourceNamespace := flag.String("source-namespace", "", "Namespace in the source cluster")
	destNamespace := flag.String("dest-namespace", "", "Namespace in the destination cluster")

	// Mode flag with validation
	mode := flag.String("mode", "", "Operation mode: Stage, Cutover, or Failback")

	// Optional flags
	includeCustomResources := flag.Bool("include-custom-resources", false, "Include custom resources in synchronization")
	migratePVCData := flag.Bool("migrate-pvc-data", false, "Migrate PVC data using pv-migrate (requires pv-migrate to be installed)")
	reverseMigratePVCData := flag.Bool("reverse-migrate-pvc-data", false, "Migrate PVC data from destination back to source (for Failback mode)")
	resourceTypes := flag.String("resource-types", "", "Comma-separated list of resource types to include (overrides defaults)")
	excludeResourceTypes := flag.String("exclude-resource-types", "", "Comma-separated list of resource types to exclude")
	pvMigrateFlags := flag.String("pv-migrate-flags", "", "Additional flags to pass to pv-migrate (e.g. \"--strategy rsync --lbsvc-timeout 10m\")")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")

	// Parse command line flags
	flag.Parse()

	// Set log level
	if err := logging.SetLogLevel(*logLevel); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid log level: %v\n", err)
		os.Exit(1)
	}

	// Handle version flags
	if *showVersion {
		fmt.Println(version.GetVersionString())
		os.Exit(0)
	}
	if *showVersionJSON {
		fmt.Println(version.GetVersionJSON())
		os.Exit(0)
	}

	// Validate required flags
	if *sourceKubeconfig == "" {
		fmt.Fprintln(os.Stderr, "Error: --source-kubeconfig is required")
		flag.Usage()
		os.Exit(1)
	}
	if *destKubeconfig == "" {
		fmt.Fprintln(os.Stderr, "Error: --dest-kubeconfig is required")
		flag.Usage()
		os.Exit(1)
	}
	if *sourceNamespace == "" {
		fmt.Fprintln(os.Stderr, "Error: --source-namespace is required")
		flag.Usage()
		os.Exit(1)
	}
	if *destNamespace == "" {
		fmt.Fprintln(os.Stderr, "Error: --dest-namespace is required")
		flag.Usage()
		os.Exit(1)
	}

	// Validate mode flag
	validModes := map[string]bool{
		"Stage":    true,
		"Cutover":  true,
		"Failback": true,
	}
	if *mode == "" {
		fmt.Fprintln(os.Stderr, "Error: --mode is required (Stage, Cutover, or Failback)")
		flag.Usage()
		os.Exit(1)
	}
	if !validModes[*mode] {
		fmt.Fprintf(os.Stderr, "Error: Invalid mode '%s'. Must be one of: Stage, Cutover, Failback\n", *mode)
		flag.Usage()
		os.Exit(1)
	}

	// Parse resource types
	var resourceTypesList []string
	if *resourceTypes != "" {
		resourceTypesList = strings.Split(*resourceTypes, ",")
		// Trim whitespace from each resource type
		for i, rt := range resourceTypesList {
			resourceTypesList[i] = strings.TrimSpace(rt)
		}
	}

	// Parse exclude resource types
	var excludeResourceTypesList []string
	if *excludeResourceTypes != "" {
		excludeResourceTypesList = strings.Split(*excludeResourceTypes, ",")
		// Trim whitespace from each resource type
		for i, rt := range excludeResourceTypesList {
			excludeResourceTypesList[i] = strings.TrimSpace(rt)
		}
	}

	// Create config
	config := &cli.Config{
		SourceKubeconfig:       *sourceKubeconfig,
		DestKubeconfig:         *destKubeconfig,
		SourceNamespace:        *sourceNamespace,
		DestNamespace:          *destNamespace,
		Mode:                   *mode,
		IncludeCustomResources: *includeCustomResources,
		MigratePVCData:         *migratePVCData,
		ReverseMigratePVCData:  *reverseMigratePVCData,
		ResourceTypes:          resourceTypesList,
		ExcludeResourceTypes:   excludeResourceTypesList,
		PVMigrateFlags:         *pvMigrateFlags,
	}

	// Log configuration
	log.Info("Starting DR Syncer CLI")
	log.Infof("Source kubeconfig: %s", *sourceKubeconfig)
	log.Infof("Destination kubeconfig: %s", *destKubeconfig)
	log.Infof("Source namespace: %s", *sourceNamespace)
	log.Infof("Destination namespace: %s", *destNamespace)
	log.Infof("Mode: %s", *mode)

	// Run CLI with config
	if err := cli.Run(config); err != nil {
		log.Errorf("Error: %v", err)
		os.Exit(1)
	}

	log.Info("Operation completed successfully")
}
