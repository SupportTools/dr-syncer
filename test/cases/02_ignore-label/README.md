# Test Case 02: Resource Exclusion via Ignore Label

## Purpose
This test case verifies the DR Syncer controller's ability to exclude specific resources from synchronization using the `dr-syncer.io/ignore` label. It tests that the controller properly respects resource exclusion labels while still synchronizing non-ignored resources.

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
Deploys test resources in the source namespace:
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- Deployment 1 (`test-deployment-1`) - without ignore label
- Deployment 2 (`test-deployment-2`) - with ignore label:
  ```yaml
  metadata:
    labels:
      dr-syncer.io/ignore: "true"
  ```
- Service (`test-service`)
- Ingress (`test-ingress`)

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Resource Exclusion
   - Verifies resources with `dr-syncer.io/ignore: "true"` label are not synchronized
   - Confirms test-deployment-2 is not present in the DR cluster
   - Ensures the ignore label doesn't affect other resources

3. Normal Resource Synchronization
   - Verifies non-ignored resources are synchronized normally
   - Confirms test-deployment-1 is synced with 0 replicas
   - Ensures other resources (ConfigMap, Secret, Service, Ingress) are synced

4. Deployment Handling
   - Verifies non-ignored deployment (test-deployment-1) is synced with 0 replicas
   - Confirms ignored deployment (test-deployment-2) is not synced at all
   - Preserves original deployment configuration for DR activation

5. Status Updates
   - Verifies the NamespaceMapping resource status is updated correctly
   - Checks for "Synced: True" condition

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 02

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- Non-ignored resources should be synchronized to the DR cluster
- test-deployment-1 should be synced with 0 replicas
- test-deployment-2 (with ignore label) should NOT be present in DR cluster
- Replication status should show successful synchronization
- All other resource attributes should match the source cluster
