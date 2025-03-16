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
   - NamespaceMapping: Defines synchronization configuration between namespaces
   - ClusterMapping: Defines the relationship between clusters for multiple namespace mappings

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

5. Namespace Mapping
   - Direct namespace mapping (sourceNamespace → destinationNamespace)
   - Namespace creation if it doesn't exist
   - Preservation of namespace labels and annotations
   - Resource reference updates across namespaces
   - Cross-namespace reference handling
   - Planned label-based namespace selection:
     * Automatic replication based on namespace labels
     * Dynamic namespace discovery and mapping
     * Destination namespace suffix pattern (e.g., "-dr")
     * Automatic cleanup when labels are removed

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
   - NamespaceMappingReconciler: Handles resource synchronization
   - ClusterMappingReconciler: Manages cluster relationship configuration
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
   - SSH proxy/bastion pattern
   - No root filesystem access required
   - Temporary PVC mount pods
   - Node-specific routing

2. Security Pattern
   - Two-layer SSH key management:
     * DR→Agent: Cluster-level keys stored in secrets
     * Agent→Temp: Operation-specific internal keys
     * NamespaceMapping→Temp: NamespaceMapping-specific keys for isolation
   - Controller-managed key generation and rotation
   - Restricted RBAC permissions
   - Secure rsync over SSH
   - Minimal pod permissions
   - Key management implementation:
     * NamespaceMapping-level key generation and storage
     * Automatic key rotation
     * Secure key distribution to temporary pods
     * Fingerprint tracking for key verification
     * Annotations for key metadata

3. Deployment Pattern
   - Controller-managed agent lifecycle
   - Automated remote cluster setup
   - Resource management via RemoteCluster CRD
   - On-demand temporary pod creation

4. Data Flow Pattern
   ```
   DR NamespaceMapping Pod → Agent SSH (port 2222) → Temp Pod rsync (internal port)
   ```
   - Direct node-to-node path
   - Minimal network overhead
   - Clean separation of concerns

5. Temporary Pod Pattern
   - Created on-demand for specific PVCs
   - Node affinity to run on same node as PVC
   - Direct PVC mount with minimal permissions
   - Short-lived, purpose-specific pods
   - Runs rsync server for data access
   - Automatic cleanup after sync completion

6. SSH Proxy Pattern
   - Agent pod acts as SSH proxy/bastion
   - SSH forwarding to temporary pods
   - Restricted command execution
   - Connection validation and logging
   - Secure key management

## Go Code Styling

### Project Organization

1. Project Structure
   - `main.go` should be kept as small as possible, serving only as the application entrypoint
   - All packages should be placed under the `./pkg/` directory
   - CRD definitions remain in `./api/` following Kubernetes conventions
   - Controllers remain in `./controllers/` following controller-runtime conventions
   - Command-line tools should be in `./cmd/`

2. File Organization
   - Break up package files into multiple smaller files, keeping each under 300 lines
   - Group small utility, helper, and setup functions in a `util.go` file within each package
   - Name files according to their primary responsibility (e.g., `reconciler.go`, `sync.go`, `client.go`)
   - Example package structure:
     ```
     pkg/syncer/
     ├── client.go      // Client setup and operations
     ├── reconciler.go  // Core reconciliation logic
     ├── sync.go        // Resource synchronization functions 
     ├── status.go      // Status management functions
     └── util.go        // Small utility and helper functions
     ```

3. Function Organization
   - Each exported function must have a detailed comment explaining what it does
   - Example function documentation:
     ```go
     // SyncResources synchronizes Kubernetes resources from the source namespace to
     // the destination namespace according to the provided configuration. It handles
     // filtering resources by type, applying exclusions, and transforming references
     // between namespaces.
     //
     // The function respects resource dependencies and handles special cases for
     // Deployments, Services, and Ingresses automatically.
     func SyncResources(ctx context.Context, sourceClient, destClient client.Client, 
         mapping *v1alpha1.NamespaceMapping) error {
         // Implementation
     }
     ```

### Source Control Practices

1. Git Commit Strategy
   - Make frequent, small commits while coding to create recovery points
   - Each commit should represent a logical unit of work
   - Use descriptive commit messages with a clear structure:
     ```
     [Component] Brief description of change
     
     More detailed explanation if needed
     ```
   - Example commit messages:
     ```
     [NamespaceMapping] Add PVC filtering support
     
     Adds ability to filter PVCs by storage class or access mode
     when synchronizing between namespaces.
     ```
     ```
     [CLI] Fix error handling in cluster connection
     
     Improves error messages when kubeconfig is invalid and adds
     retry logic for transient connection issues.
     ```

2. Code Recovery with Git
   - Use git to recover accidentally deleted code:
     ```bash
     # Find the commit where code was deleted
     git log -p -- path/to/file.go
     
     # Recover specific file from a previous commit without affecting other files
     git checkout [commit-hash] -- path/to/file.go
     
     # For recovering just part of a file, use git blame to find the right commit
     git blame path/to/file.go
     
     # Then checkout that specific file and manually extract the needed parts
     git checkout [commit-hash] -- path/to/file.go
     ```
   - Consider using `git stash` to temporarily save changes when switching tasks:
     ```bash
     # Save current changes
     git stash save "description of changes"
     
     # List saved stashes
     git stash list
     
     # Apply a specific stash (keeping it in the stash list)
     git stash apply stash@{0}
     
     # Remove stash after applying it
     git stash pop
     ```

3. Branching Strategy
   - Use feature branches for new features or significant changes
   - Keep feature branches short-lived (1-5 days)
   - Regularly rebase feature branches on the main branch
   - Use pull requests for code review
   - Clean up branches after merging

### Formatting and Organization

1. Code Formatting
   - Use `go fmt` or `gofmt` for automatic formatting
   - Maintain 80-100 character line length where reasonable
   - Follow standard Go indentation (tabs, not spaces)
   - Run formatting before committing: `go fmt ./...`

2. Import Organization
   - Group imports in three distinct blocks:
     * Standard library
     * External dependencies
     * Internal project packages
   ```go
   import (
       "context"
       "fmt"
       "time"

       "k8s.io/apimachinery/pkg/apis/meta/v1"
       "sigs.k8s.io/controller-runtime/pkg/client"
       
       "github.com/supporttools/dr-syncer/pkg/logging"
   )
   ```
   - Use explicit import names when necessary to avoid collisions
   - Avoid dot imports (e.g., `import . "fmt"`)

3. Package Organization
   - Organize packages by functional domain, not by type
   - Keep package names short, clear, and descriptive
   - Avoid package name collisions with common libraries
   - Follow established package patterns:
     * api/v1alpha1 - CRD type definitions
     * controllers - Controller implementations
     * pkg - Shared utilities and components

### Naming Conventions

1. General Naming
   - Use camelCase for private identifiers
   - Use PascalCase for exported identifiers
   - Avoid abbreviations except for common ones (e.g., API, CRD, DR)
   - Be explicit and descriptive in naming

2. Package-Specific Naming
   - CRD Types (api/v1alpha1):
     * Types: `[Resource]Spec`, `[Resource]Status` (e.g., `NamespaceMappingSpec`)
     * Fields: Descriptive, domain-specific names (e.g., `SourceNamespace`, `DestinationNamespace`)
   - Controllers:
     * Reconcilers: `[Resource]Reconciler` (e.g., `NamespaceMappingReconciler`)
     * Methods: Follow controller-runtime patterns (e.g., `Reconcile`, `SetupWithManager`)

3. Variable Naming
   - Contextual variables: `ctx` for context.Context
   - Loop indices: `i`, `j`, etc., or descriptive names for complex loops
   - Error returns: `err`
   - Controllers: `r` for reconciler instance
   - Kubernetes clients: `client` or `k8sClient`

### Error Handling Patterns

1. Standard Error Handling
   ```go
   if err != nil {
       // Record event
       r.Recorder.Event(obj, corev1.EventTypeWarning, "SyncFailed", err.Error())
       
       // Update status with condition
       meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
           Type:    "Synchronized",
           Status:  metav1.ConditionFalse,
           Reason:  "SyncFailed",
           Message: err.Error(),
       })
       
       // Update object status
       if updateErr := r.Status().Update(ctx, obj); updateErr != nil {
           log.WithError(updateErr).Error("failed to update status")
       }
       
       // Return with requeue to retry
       return ctrl.Result{RequeueAfter: time.Minute}, err
   }
   ```

2. Error Wrapping
   - Wrap errors with context using `fmt.Errorf("context: %w", err)`
   - Include relevant details when wrapping errors
   - Use error wrapping for operation context, not just message

3. Error Logging
   - Use structured logging with error fields:
     ```go
     log.WithError(err).WithField("resource", req.NamespacedName).Error("failed to sync")
     ```
   - Include appropriate context in error logs
   - Use consistent verbosity levels

### Documentation Standards

1. Package Documentation
   - Add package documentation comments at the top of each package's primary file:
     ```go
     // Package controller implements the core reconciliation logic for DR Syncer
     // resources, handling cluster connection management and resource synchronization.
     package controller
     ```

2. Type and Function Documentation
   - Document all exported types and functions with detailed comments
   - Include usage examples for complex functions
   - Document all parameters and return values
   ```go
   // SyncResources synchronizes resources from the source namespace to the
   // destination namespace according to the provided configuration.
   //
   // It filters resources based on resourceTypes and exclusions, and applies
   // transformations as needed for the destination cluster.
   //
   // Parameters:
   //   - ctx: Context for the operation
   //   - sourceClient: Client for the source cluster
   //   - destClient: Client for the destination cluster
   //   - mapping: NamespaceMapping configuration
   //
   // Returns:
   //   - error: Any error encountered during synchronization
   func SyncResources(ctx context.Context, sourceClient, destClient client.Client, 
       mapping *v1alpha1.NamespaceMapping) error {
       // Implementation
   }
   ```

3. Implementation Comments
   - Add comments for complex or non-obvious code sections
   - Explain algorithm choices and design decisions
   - Document known limitations or edge cases

### Testing Patterns

1. Test Organization
   - Use table-driven tests for multiple test cases
   - Group related test cases
   - Name tests clearly: `TestFunctionName_Scenario`

2. Test Helpers
   - Create shared test fixtures and helpers
   - Use testing.T helpers for common assertions
   - Isolate test dependencies

3. Test Coverage
   - Aim for high test coverage on core business logic
   - Test error cases and edge conditions
   - Include integration tests for controller behavior

### Kubernetes-Specific Patterns

1. Controller Patterns
   ```go
   // Reconcile implements the reconciliation loop for NamespaceMapping resources
   func (r *NamespaceMappingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
       log := r.Log.WithValues("namespacemapping", req.NamespacedName)
       
       // Fetch the NamespaceMapping resource
       var mapping v1alpha1.NamespaceMapping
       if err := r.Get(ctx, req.NamespacedName, &mapping); err != nil {
           if k8serrors.IsNotFound(err) {
               return ctrl.Result{}, nil // Object not found, no requeue
           }
           return ctrl.Result{}, err // Error reading, requeue
       }
       
       // Implement reconciliation logic
       
       // Update status
       mapping.Status.LastSyncTime = metav1.Now()
       if err := r.Status().Update(ctx, &mapping); err != nil {
           log.Error(err, "Failed to update status")
           return ctrl.Result{}, err
       }
       
       return ctrl.Result{RequeueAfter: time.Duration(mapping.Spec.IntervalSeconds) * time.Second}, nil
   }
   ```

2. Client-go Patterns
   - Prefer controller-runtime Client over direct clientsets
   - Use List operations with LabelSelectors for filtering
   - Follow API server pagination patterns for large resource lists

3. CRD Type Patterns
   - Use consistent type definitions across CRDs
   - Include validation markers for OpenAPI schema generation
   - Follow Kubernetes API conventions for status reporting

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
   - Controller-runtime logging integration
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
   - Controller-runtime integration in main.go:
   ```go
   // Initialize logging
   log := logging.SetupLogging()
   
   // Set up controller-runtime logging to use our logger
   logging.SetupControllerRuntimeLogging(log)
   ```
   - LogrusLogAdapter implementation:
   ```go
   // Implements logr.LogSink interface to bridge logrus with controller-runtime
   type LogrusLogAdapter struct {
       logger *logrus.Logger
       name   string
   }
   
   // SetupControllerRuntimeLogging configures controller-runtime
   func SetupControllerRuntimeLogging(logger *logrus.Logger) {
       ctrl := NewControllerRuntimeLogger(logger)
       ctrllog.SetLogger(ctrl)
   }
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
   - Generated CRDs: config/crd/bases/dr-syncer.io_{remoteclusters,namespacemappings,clustermappings}.yaml
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
       - api/v1alpha1/types.go: Update appropriate Spec (NamespaceMappingSpec, RemoteClusterSpec, ClusterMappingSpec)
       - test/cases/XX_*: Add test cases covering new fields
       - charts/dr-syncer/values.yaml: Add default values if needed

     * For Status Changes:
       - api/v1alpha1/types.go: Add new status structs and fields
       - api/v1alpha1/types.go: Add DeepCopy methods for new types
       - api/v1alpha1/types.go: Update appropriate Status (NamespaceMappingStatus, RemoteClusterStatus, ClusterMappingStatus)
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
       kubectl apply -f config/crd/bases/dr-syncer.io_namespacemappings.yaml
       ```
     * For production:
       - Update via Helm chart
       - Follow proper release process

   - Step 4: Validation
     * Verify new fields with kubectl explain
     * Run affected test cases
     * Check controller logs for schema errors
