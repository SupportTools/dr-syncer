---
sidebar_position: 5
---

# Development Guide

This guide provides information for developers who want to contribute to or understand the DR-Syncer codebase.

## Project Structure

The DR-Syncer project follows a standard Go project layout with Kubernetes controller-runtime conventions:

```
dr-syncer/
├── api/                     # API definitions
│   └── v1alpha1/            # API version directory
│       ├── cluster_types.go # RemoteCluster CRD
│       ├── namespacemapping_types.go
│       └── types.go         # Common types
├── cmd/                     # Command-line entry points
│   └── agent/               # Agent command
│       └── main.go          # Agent main
├── pkg/                     # Packages directory
│   ├── agent/               # Agent code
│   │   ├── daemon/          # Agent daemon
│   │   ├── ssh/             # SSH utilities
│   │   └── leader/          # Leader election
│   └── controller/          # Controller code
│       └── replication/     # Replication controller
├── build/                   # Build artifacts
│   ├── Dockerfile           # Controller Docker
│   ├── Dockerfile.agent     # Agent Docker
│   └── Dockerfile.rsync     # Rsync Docker
├── charts/                  # Helm charts
│   └── dr-syncer/           # Main chart
├── config/                  # Configuration
│   └── crd/                 # Generated CRDs
└── test/                    # Test cases
    └── cases/               # Specific test cases
```

## Setup Development Environment

### Prerequisites

- Go 1.23+
- Docker
- kubectl
- Kind or Minikube (for local testing)
- controller-gen

### Building from Source

1. Clone the repository:
   ```bash
   git clone https://github.com/supporttools/dr-syncer.git
   cd dr-syncer
   ```

2. Build the controller binaries:
   ```bash
   make build
   ```

3. Build Docker images:
   ```bash
   make docker-build
   ```

### Running Locally

To run the controller locally for development:

```bash
./run-local.sh /path/to/controller/kubeconfig
```

The script handles:
- Scaling down the in-cluster controller deployment to prevent conflicts
- Setting the KUBECONFIG environment variable
- Restoring the original deployment when you exit

## Working with APIs

### CRD Management

DR-Syncer uses CRDs for configuration. The workflow for updating CRDs is:

1. Update the Go types in `api/v1alpha1/types.go`
2. Add proper validation using kubebuilder markers
3. Generate CRDs using:
   ```bash
   make manifests
   ```
4. Apply the updated CRDs to your cluster:
   ```bash
   kubectl apply -f config/crd/bases/
   ```

### Important API Types

#### RemoteCluster CRD

Defines remote cluster configurations and authentication:

```go
type RemoteClusterSpec struct {
	// KubeconfigSecret specifies the secret containing the kubeconfig
	KubeconfigSecret SecretReference `json:"kubeconfigSecret"`
	
	// PVCSync configures PVC synchronization
	PVCSync *PVCSyncConfig `json:"pvcSync,omitempty"`
}
```

#### Replication CRD

Defines what resources to replicate and how:

```go
type ReplicationSpec struct {
	// RemoteCluster references a RemoteCluster resource
	RemoteCluster RemoteClusterReference `json:"remoteCluster"`
	
	// SourceNamespace is the namespace to replicate from
	SourceNamespace string `json:"sourceNamespace"`
	
	// DestinationNamespace is the namespace to replicate to
	DestinationNamespace string `json:"destinationNamespace"`
	
	// ResourceTypes lists the types of resources to replicate
	ResourceTypes []string `json:"resourceTypes,omitempty"`
}
```

## Controller Architecture

DR-Syncer uses the controller-runtime library and implements the reconciler pattern:

```go
func (r *RemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Get the RemoteCluster resource
    // Process remote cluster configuration
    // Update status
    return ctrl.Result{}, nil
}
```

### Reconciliation Flow

1. Controller watches for resource changes
2. When changes are detected, it queues reconciliation requests
3. Reconciler fetches the current state
4. Reconciler compares with desired state
5. Reconciler makes changes to match the desired state
6. Status is updated to reflect the reconciliation

## Testing

### Running Tests

To run the tests:

```bash
make test
```

### Test Cases

The test directory contains various test cases that validate different features:

- Test case 00: Standard resources test
- Test case 01: Wildcard namespace selection
- Test case 02: Ignore label handling
- Test case 03: Scale down behavior
- Test case 04: Scale override
- ...and more

### Writing New Tests

1. Create a new directory in `test/cases/` with your test name
2. Add controller.yaml for Replication configuration
3. Add remote.yaml for test resources
4. Implement test.sh following the standard test structure
5. Update test/README.md with your test details

## Logging

DR-Syncer uses a structured logging approach:

```go
// Setup package-level logger
var log = logging.SetupLogging()

// Usage in code
log.Info("message", "key", value)
log.WithError(err).Error("error message")
```

### Log Levels

- Debug: Verbose information for debugging purposes
- Info: Standard operational information
- Warning: Non-critical issues that might require attention
- Error: Failures that require investigation

## Release Process

The project uses GitHub Actions for CI/CD and follows semantic versioning:

1. Ensure all changes are committed and pushed to the main branch
2. Create an annotated tag following semantic versioning:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   ```
3. Push the tag to GitHub:
   ```bash
   git push origin v1.0.0
   ```

The GitHub Actions pipeline will build images, package the Helm chart, and create a GitHub Release.

## Contributing Guidelines

1. Fork the repository and create a feature branch
2. Make your changes, ensuring they follow project conventions
3. Write tests for new functionality
4. Update documentation as needed
5. Submit a pull request with a clear description of the changes
6. Ensure CI checks pass for your pull request

### Code Style

- Follow standard Go formatting and linting practices
- Use meaningful variable and function names
- Write clear comments and documentation
- Keep functions focused and reasonably sized
- Use proper error handling with context
