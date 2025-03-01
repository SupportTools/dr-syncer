# Technical Context: DR Syncer Controller

## Technology Stack

### Core Technologies
1. Go (1.23+)
   - Primary implementation language
   - Standard library utilization
   - Go modules for dependency management

2. Kubernetes
   - API server interaction
   - Custom Resource Definitions
   - Controller Runtime framework
   - Client-go library

### Dependencies

1. Controller Runtime
   ```go
   sigs.k8s.io/controller-runtime
   ```
   - Manager implementation
   - Reconciler patterns
   - Client interfaces
   - Metrics server

2. Client-go
   ```go
   k8s.io/client-go
   ```
   - Kubernetes API client
   - Authentication plugins
   - Informer/cache patterns
   - REST client implementation

3. API Machinery
   ```go
   k8s.io/apimachinery
   ```
   - Type definitions
   - Schema management
   - Runtime utilities
   - Conversion functions

## Development Setup

### Prerequisites
1. Development Tools
   - Go 1.23+
   - controller-gen
   - kubectl
   - Kubernetes cluster access
   - Helm (optional)

2. Environment Setup
   ```bash
   # Install controller-gen
   go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

   # Generate CRDs (remoteclusters and replications)
   controller-gen crd paths="./..." output:crd:artifacts:config=config/crd/bases
   
   # Create kubeconfig directory
   mkdir -p kubeconfig
   
   # Setup kubeconfig files for different environments
   # - controller: Controller cluster kubeconfig
   # - dr: DR cluster kubeconfig
   # - prod: Production cluster kubeconfig
   ```

### Build Process
1. Building the Controller
   ```bash
   go build -o dr-syncer
   ```

2. Running Tests
   ```bash
   # Run tests with default verbosity
   go test ./...
   
   # Run tests with verbose output
   go test -v ./...
   ```

3. CRD Generation
   ```bash
   controller-gen crd paths="./..." output:crd:artifacts:config=config/crd/bases
   ```

4. Makefile Options
   ```bash
   # Build with minimal output
   make build
   
   # Build with verbose output
   make build DEBUG=1
   
   # Deploy to controller cluster
   make deploy-local
   
   # Deploy to DR cluster
   make deploy-dr
   
   # Deploy to production cluster
   make deploy-prod
   ```

## Deployment

### Helm Deployment
1. Chart Structure
   ```
   charts/dr-syncer/
   ├── Chart.yaml
   ├── values.yaml
   ├── templates/
   │   ├── _helpers.tpl
   │   ├── deployment.yaml
   │   ├── rbac.yaml
   └── crds/
       ├── dr-syncer.io_remoteclusters.yaml
       └── dr-syncer.io_replications.yaml
   ```

2. Configuration Options
   ```yaml
   controller:
     logLevel: "debug"
     leaderElect: true
   resources:
     limits:
       cpu: "1"
       memory: 1Gi
     requests:
       cpu: "200m"
       memory: 256Mi
   ```

### Manual Deployment
1. CRD Installation
   ```bash
   kubectl apply -f config/crd/bases/
   ```

2. RBAC Setup
   ```bash
   kubectl apply -f config/rbac/
   ```

## PVC Sync Components

1. Agent Container
   - Base: Alpine Linux
   - Key packages:
     * OpenSSH server
     * rsync
     * bash
   - Configuration:
     * Custom SSH port
     * Restricted shell access
     * rsync-only commands

2. Build Process
   - Separate agent image build
   - Version alignment with controller
   - Multi-stage build optimization

## Technical Constraints

1. Resource Limitations
   - Memory usage for caching
   - CPU usage during reconciliation
   - Network bandwidth for syncs
   - Storage for CRD data

2. Kubernetes Version Support
   - Minimum version requirements
   - API compatibility
   - CRD version constraints
   - Controller features support

3. Security Constraints
   - RBAC requirements
   - Secret management
   - Network policies
   - Service account permissions

4. Operational Constraints
   - Leader election requirements
   - Metrics collection
   - Health monitoring
   - Resource quotas

## Development Patterns

1. Code Organization
   ```
   ├── api/
   │   └── v1alpha1/          # CRD type definitions
   ├── controllers/
   │   ├── remotecluster/     # RemoteCluster controller
   │   └── replication/       # Replication controller
   ├── pkg/
   │   ├── config/            # Configuration management
   │   └── logging/           # Logging setup and adapters
   │       ├── logging.go     # Logrus logger setup
   │       └── controller_runtime.go # Controller-runtime integration
   └── charts/
       └── dr-syncer/
           └── crds/          # Generated CRDs
   ```

2. Logging Integration
   - Logrus for application logging
   - Controller-runtime logging integration via custom adapter
   - LogrusLogAdapter implements logr.LogSink interface
   - Centralized logging configuration
   - Debug mode support via environment variables

2. Testing Strategy
   - Unit tests for business logic
   - Integration tests for controllers
   - End-to-end testing
   - Performance testing

3. Documentation
   - API documentation
   - Controller behavior
   - Deployment guides
   - Troubleshooting guides

4. Monitoring
   - Prometheus metrics
   - Health endpoints
   - Status conditions
   - Event recording
