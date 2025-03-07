# Test Case 23: Resource Change Detection and Relationship Tracking

## Purpose
This test case verifies the new controller functionality for detecting changes across related resources and preventing reconciliation loops. It tests the controllers' ability to:
1. Detect changes in related resources (RemoteCluster, ClusterMapping, NamespaceMapping)
2. Propagate changes through resource relationship chains
3. Avoid infinite reconciliation loops caused by status updates

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a RemoteCluster resource
- Creates a ClusterMapping resource that references the RemoteCluster
- Creates a NamespaceMapping resource that references the ClusterMapping

### Source Resources (`remote.yaml`)
- Deploys a namespace with test resources in the source cluster
- Includes ConfigMap and Deployment resources

## What is Tested
1. Cross-Resource Change Detection
   - Verifies that changes to RemoteCluster trigger ClusterMapping reconciliation
   - Verifies that changes to ClusterMapping trigger NamespaceMapping reconciliation
   - Verifies that status-only updates don't cause reconciliation loops

2. Change Propagation
   - Verifies changes in related resources propagate correctly
   - Tests the complete chain: RemoteCluster → ClusterMapping → NamespaceMapping

3. Status Updates
   - Verifies status updates don't trigger unnecessary reconciliations
   - Ensures resources are only reconciled when their specs change

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