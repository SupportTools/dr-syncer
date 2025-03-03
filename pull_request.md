# PVC Sync Implementation

This PR implements PVC synchronization between clusters using a pod-based approach with rsync.

## Features

- Pod-based PVC synchronization
  * Creates sync pods in target cluster to mount PVCs
  * Uses pod volume mounts for data synchronization
  * Automatically cleans up sync pods after sync

- Smart Node Selection
  * Matches nodes based on topology and instance type labels
  * Special handling for ReadWriteMany PVCs
  * Fallback to available nodes when no match found

- Volume Management
  * Handles different PVC sizes between clusters
  * Supports volume size increases
  * Preserves storage class configuration
  * Tracks source PVC details in annotations

- Error Handling and Retries
  * Exponential backoff for sync retries
  * Proper cleanup on failures
  * Detailed error logging and status updates

## Implementation Details

### Agent Components
- SSH server for secure communication
- DaemonSet deployment on target cluster
- RBAC setup for pod and PVC management

### Sync Process
1. Discovers PVCs on source cluster nodes
2. Creates corresponding PVCs in target cluster
3. Deploys sync pods with volume mounts
4. Executes rsync through SSH
5. Cleans up resources after sync

### Testing
- Comprehensive test cases for:
  * ReadWriteOnce and ReadWriteMany PVCs
  * Different volume sizes
  * Node selection and scheduling
  * Storage class handling
  * Error scenarios and cleanup

## Usage

The feature is enabled through the RemoteCluster CRD:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  pvcSync:
    enabled: true
    retryConfig:
      maxRetries: 3
      initialDelay: 5s
      maxDelay: 30s
```

## Testing Done

1. Basic sync functionality
2. Node label matching
3. ReadWriteMany support
4. Volume size management
5. Error handling and retries
6. Resource cleanup
7. Integration with existing features

## Future Improvements

1. Support for volume snapshots
2. Bandwidth throttling
3. Progress reporting
4. Incremental sync optimization
