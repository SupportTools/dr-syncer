# System Patterns: DR Syncer Controller

## Architecture Overview

### Controller Pattern
1. Core Components
   - Manager: Handles controller lifecycle and shared dependencies
   - Reconcilers: Implement controller business logic
   - Clients: Interact with Kubernetes API
   - Custom Resources: Define configuration and state

2. Reconciliation Flow
   - Watch for resource changes
   - Compare desired vs actual state
   - Execute synchronization operations
   - Update status and metrics

### Resource Handling

1. Custom Resources
   - RemoteCluster: Defines remote cluster configuration and authentication
   - Replication: Defines synchronization configuration and resource filtering

2. Resource Processing
   - Resource filtering based on type
   - Exclusion list handling
   - Metadata preservation
   - Status updates

3. Deployment Handling
   - Zero replicas in DR cluster
   - Original replica count preservation
   - Annotation management
   - Controlled scaling

4. Service/Ingress Management
   - ClusterIP handling
   - Network configuration adaptation
   - Service recreation logic

## Design Patterns

1. Operator Pattern
   - Custom Resource Definitions
   - Controller-based automation
   - Kubernetes-native interfaces
   - Declarative configuration

2. Leader Election
   - High availability support
   - Single active controller
   - Automatic failover
   - Resource locking

3. Health Monitoring
   - Readiness probes
   - Liveness checks
   - Metrics exposure
   - Status reporting

4. Error Handling
   - Graceful failure recovery
   - Retry mechanisms
   - Event recording
   - Status updates

## Component Relationships

1. Controller Manager
   - Manages controller lifecycle
   - Handles shared resources
   - Coordinates reconcilers
   - Manages metrics server

2. Reconcilers
   - RemoteClusterReconciler: Manages remote cluster connections
   - ReplicationReconciler: Handles resource synchronization
   - Resource synchronization logic
   - Status management

3. Kubernetes Integration
   - API server communication
   - Resource watching
   - Event handling
   - Client caching

## Implementation Patterns

1. Resource Synchronization
   ```go
   type Reconciler struct {
       Client client.Client
       Scheme *runtime.Scheme
   }

   func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
       // Fetch resource
       // Compare states
       // Execute sync
       // Update status
   }
   ```

2. Error Management
   ```go
   if err != nil {
       // Record event
       // Update status
       // Return with requeue
   }
   ```

3. Status Updates
   ```go
   status.LastSyncTime = metav1.Now()
   status.Conditions = append(status.Conditions, condition)
   ```

4. Resource Filtering
   ```go
   if shouldSync(resource, config.ResourceTypes) {
       // Process resource
   }
   ```

## PVC Sync Pattern

1. Agent Architecture
   - DaemonSet-based deployment
   - Host network access
   - Kubelet volume mounting
   - SSH service for rsync

2. Security Pattern
   - Per-cluster SSH key pairs
   - Controller-managed key distribution
   - Restricted RBAC permissions
   - Secure rsync over SSH

3. Deployment Pattern
   - Controller-managed agent lifecycle
   - Automated remote cluster setup
   - Resource management via RemoteCluster CRD

## Best Practices

1. Resource Management
   - Immutable fields preservation
   - Proper garbage collection
   - Resource version handling
   - Namespace isolation

2. Security
   - RBAC configuration
   - Secret management
   - Secure communication
   - Access control

3. Performance
   - Efficient reconciliation
   - Resource caching
   - Batch processing
   - Rate limiting

4. Monitoring
   - Prometheus metrics
   - Event recording
   - Status conditions
   - Logging levels

5. Logging Pattern
   - Package-level logger initialization via logger.go
   - Shared logging setup across packages
   - Consistent logging interface
   ```go
   // logger.go in each package
   package mypackage

   import "github.com/supporttools/dr-syncer/pkg/logging"

   var log = logging.SetupLogging()
   ```
   - Usage pattern in package files:
   ```go
   log.Info("message")
   log.WithError(err).Error("error message")
   ```
   - Log viewing best practices:
     * Always use --tail flag to limit log output:
       ```bash
       kubectl logs pod-name --tail=100  # Last 100 lines
       docker logs container-name --tail=50  # Last 50 lines
       ```
     * Never use -f/--follow in automated scripts or tests:
       - These commands will never return and cause scripts to hang
       - Reserve for interactive debugging only
       - Use --tail instead for bounded output
     * Recommended tail sizes:
       - Normal operations: --tail=100
       - Detailed debugging: --tail=1000
       - Quick checks: --tail=20
   - Benefits:
     * Centralized logging configuration
     * Consistent logging format
     * Package-level logging control
     * Clean separation of concerns
     * Easy logging level management
     * Prevents memory issues from unbounded log output
     * Avoids hanging scripts due to -f flags

## CRD Management

1. CRD Locations and Flow
   - Source of Truth: Go types in pkg/api/v1alpha1/types.go
   - Generated CRDs: config/crd/bases/dr-syncer.io_{remoteclusters,replications}.yaml
   - Helm Chart CRDs: charts/dr-syncer/crds/*.yaml
   - Automated sync via `make manifests`

2. Update Process
   - Start with API types in types.go
   - Add new fields with proper JSON tags and validation
   - Implement DeepCopy methods for new types
   - Run `make manifests` to:
     * Generate CRDs in config/crd/bases/
     * Automatically sync to Helm chart with templating

3. Automation Tools
   - controller-gen for generating CRDs from Go types
   - kubebuilder markers for validation and printer columns
   - make manifests to regenerate CRDs

4. Validation Steps
   - Verify OpenAPI schema matches types
   - Check printer columns configuration
   - Test CRD installation via Helm
   - Validate with kubectl explain

5. Common Changes
   - Status fields for better monitoring
   - Printer columns for kubectl output
   - Validation rules and defaults
   - New spec fields for features

6. Best Practices
   - Keep CRDs in sync across all locations
   - Document all validation rules
   - Use consistent naming patterns
   - Include clear field descriptions
   - Add examples in CRD documentation

7. CRD Update Workflow
   - Step 1: Update Go Types
     * Add new structs/fields in api/v1alpha1/types.go
     * Include kubebuilder markers for validation
     * Implement DeepCopy methods if needed
     * Follow existing patterns for similar fields

   - Required Files for CRD Updates:
     * For Spec Changes:
       - api/v1alpha1/types.go: Add new structs and fields
       - api/v1alpha1/types.go: Add DeepCopy methods for new types
       - api/v1alpha1/types.go: Update ReplicationSpec/RemoteClusterSpec
       - test/cases/XX_*: Add test cases covering new fields
       - charts/dr-syncer/values.yaml: Add default values if needed

     * For Status Changes:
       - api/v1alpha1/types.go: Add new status structs and fields
       - api/v1alpha1/types.go: Add DeepCopy methods for new types
       - api/v1alpha1/types.go: Update ReplicationStatus/RemoteClusterStatus
       - controllers/*_controller.go: Update status handling in reconciler

     * Generated/Updated by make manifests:
       - config/crd/bases/dr-syncer.io_*.yaml
       - charts/dr-syncer/crds/*.yaml

   - Step 2: Regenerate CRDs
     * Run `make manifests` to:
       - Generate new CRD YAML
       - Update Helm chart CRDs
       - Validate schema changes

   - Step 3: Apply Changes
     * Apply updated CRD to cluster:
       ```bash
       kubectl apply -f config/crd/bases/dr-syncer.io_replications.yaml
       ```
     * For production:
       - Update via Helm chart
       - Follow proper release process

   - Step 4: Validation
     * Verify new fields with kubectl explain
     * Run affected test cases
     * Check controller logs for schema errors
