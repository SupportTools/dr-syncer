# Test Case 23: Status Update Loop Prevention

## Purpose
This test case verifies the controller's ability to prevent reconciliation loops caused by status updates. It focuses on testing:
1. Absence of reconciliation loops when status updates occur
2. Separation of spec changes from status changes in reconciliation logic

**Note:** Full resource change detection tests require a running controller and will be implemented in future tests.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a RemoteCluster resource
- Creates a ClusterMapping resource that references the RemoteCluster
- Creates a NamespaceMapping resource that references the ClusterMapping

### Source Resources (`remote.yaml`)
- Deploys a namespace with test resources in the source cluster
- Includes ConfigMap and Deployment resources

## What is Tested
1. Status Update Handling
   - Verifies status updates don't trigger unnecessary reconciliations
   - Ensures resources are only reconciled when their specs change
   - Tests the absence of reconciliation loops
   - Confirms that the reconciliation predicates correctly filter status-only updates

2. Future Tests
   - Resource change detection (ConfigMaps, Deployments)
   - Cross-resource change detection (RemoteCluster → ClusterMapping → NamespaceMapping)
   - Complete relationship chain propagation

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 23

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- Changes in one resource should trigger reconciliation in dependent resources
- Status-only updates should not cause unnecessary reconciliation loops
- Resources should be properly synced between clusters when changes occur