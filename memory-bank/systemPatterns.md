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
