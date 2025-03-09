# Test Case 16: Manual Mode Replication

## Purpose
This test case verifies the functionality of the Manual replication mode with ClusterMapping reference. In Manual mode, resource synchronization only occurs when explicitly triggered by the user via annotations.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates RemoteCluster resources for the source and target clusters
- Creates a ClusterMapping resource to connect the source and target clusters
- Creates a NamespaceMapping resource in Manual mode
- Uses wildcard resource types to sync all resources
- Configures immutable resources to be recreated

### Source Resources (`remote.yaml`)
Deploys the following resources in the source namespace:
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- StatefulSet (`test-statefulset`)

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Initial State Verification
   - Verifies resources are NOT synced before manual trigger

3. Manual Sync Behavior
   - Triggers manual sync via the `dr-syncer.io/trigger-sync` annotation
   - Verifies resources are synced after manual triggering

4. Resource Synchronization
   - ConfigMap synchronization
   - Secret synchronization
   - StatefulSet synchronization with zero replicas

5. Status Updates
   - Verifies the NamespaceMapping resource status is updated correctly
   - Checks for "Synced: True" condition

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 16

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- Before manual trigger: No resources should be synchronized
- After manual trigger: All resources should be synchronized to the DR cluster
- StatefulSet should have 0 replicas in DR cluster
- Replication status should show successful synchronization
