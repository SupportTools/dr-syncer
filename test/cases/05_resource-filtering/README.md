# Test Case 05: Resource Type Filtering

## Purpose
This test case verifies the DR Syncer controller's ability to selectively synchronize resources based on their types. It tests that only the specified resource types are synchronized to the DR cluster, while other resource types are ignored.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates RemoteCluster resources for the source and target clusters
- Creates a ClusterMapping resource to connect the source and target clusters
- Creates a NamespaceMapping resource in the `dr-syncer` namespace
- Explicitly specifies resource types to sync:
  ```yaml
  resourceTypes:
    - ConfigMaps
    - Secrets
  ```
  This configuration should only sync ConfigMaps and Secrets, ignoring other resource types.

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
- ConfigMap (`test-configmap`) - should be synced
- Secret (`test-secret`) - should be synced
- Deployment (`test-deployment`) - should NOT be synced
- Service (`test-service`) - should NOT be synced
- Ingress (`test-ingress`) - should NOT be synced

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Resource Type Filtering
   - Verifies ConfigMaps are synchronized
   - Verifies Secrets are synchronized
   - Verifies Deployments are NOT synchronized
   - Verifies Services are NOT synchronized
   - Verifies Ingresses are NOT synchronized

3. Resource Content Verification
   - For synced resources:
     * Verifies all metadata is preserved
     * Verifies all data/content is preserved
     * Verifies labels and annotations are preserved

4. Status Updates
   - Verifies the NamespaceMapping resource status is updated correctly
   - Checks for "Synced: True" condition
   - Verifies resource status counts match expectations

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 05

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- Namespace should be created in DR cluster
- Only ConfigMaps and Secrets should be synchronized
- Deployments, Services, and Ingresses should NOT be present in DR cluster
- Synced resources should match source exactly (except for system fields)
- Replication status should show successful synchronization
- Resource status should show correct count of synced resources
