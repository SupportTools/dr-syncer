# Test Case 01: Wildcard Namespace Selection

## Purpose
This test case verifies the DR Syncer controller's ability to synchronize all resource types using the wildcard selector (`"*"`). It tests that the controller can properly handle and synchronize resources when all types are selected for replication.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates RemoteCluster resources for the source and target clusters
- Creates a ClusterMapping resource to connect the source and target clusters
- Creates a NamespaceMapping resource in the `dr-syncer` namespace to define replication
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```

### Source Resources (`remote.yaml`)
Deploys standard test resources in the source namespace:
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- Deployment (`test-deployment`)
- Service (`test-service`)
- Ingress (`test-ingress`)

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Wildcard Resource Synchronization
   - Tests that all resources are synchronized regardless of type
   - Verifies the wildcard selector properly includes all resource types
   - Ensures no resources are accidentally excluded

3. Deployment Handling
   - Verifies deployments are synced with 0 replicas in the DR cluster
   - Confirms replica count behavior is maintained even with wildcard selection
   - Preserves original deployment configuration for DR activation

4. Service and Ingress Handling
   - Verifies network resources are properly synchronized
   - Ensures service configurations are maintained
   - Confirms ingress rules are correctly replicated

5. Status Updates
   - Verifies the NamespaceMapping resource status is updated correctly
   - Checks for "Synced: True" condition

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 01

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All resources should be synchronized to the DR cluster
- Deployments should have 0 replicas in DR cluster
- Replication status should show successful synchronization
- Resource synchronization should be identical to explicit type selection
- All resource attributes should match the source cluster except for deployment replicas
