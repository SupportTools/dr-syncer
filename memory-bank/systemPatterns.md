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
   - RemoteCluster: Defines remote cluster configuration
   - NamespaceReplication: Defines sync configuration
   - NamespaceMapping: Maps source to destination namespaces

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
   - RemoteClusterReconciler
   - NamespaceReplicationReconciler
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
