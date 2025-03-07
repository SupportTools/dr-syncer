# Test Case 23: Resource Change Detection and Relationship Tracking

## Purpose
This test case verifies the new controller functionality for detecting changes across related resources and preventing reconciliation loops. It tests the controllers' ability to:
1. Detect changes in source resources (ConfigMaps, Deployments) and propagate them to the destination cluster
2. Avoid infinite reconciliation loops caused by status updates
3. (Future) Detect changes in related resources and propagate them through relationship chains

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a RemoteCluster resource
- Creates a ClusterMapping resource that references the RemoteCluster
- Creates a NamespaceMapping resource that references the ClusterMapping

### Source Resources (`remote.yaml`)
- Deploys a namespace with test resources in the source cluster
- Includes ConfigMap and Deployment resources

## What is Tested
1. Resource Change Detection
   - Verifies changes to source resources (ConfigMaps) are detected and propagated to the destination cluster
   - Tests the continuous watch mechanism for detecting changes to resources

2. Status Update Handling
   - Verifies status updates don't trigger unnecessary reconciliations
   - Ensures resources are only reconciled when their specs change
   - Tests the absence of reconciliation loops

3. Future Tests
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