# Test Case 04: Scale Override Functionality

## Purpose
This test case verifies the DR Syncer controller's ability to handle scale override labels. While deployments are typically scaled to zero replicas in the DR cluster, this test verifies that deployments with the `dr-syncer.io/scale-override` label maintain their specified replica count in the DR cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- Deployment 1 (`test-deployment-1`) with 3 replicas and no override label (should scale to 0)
- Deployment 2 (`test-deployment-2`) with 3 replicas and scale override label (should maintain 3 replicas)
- Service (`test-service`)
- Ingress (`test-ingress`)

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Deployment Scale Handling
   - Verifies deployment without override label is scaled to 0 replicas in DR cluster
   - Verifies deployment with override label maintains its replica count in DR cluster
   - Confirms all other deployment configurations are preserved
   - Ensures the scale override doesn't affect other deployment attributes

3. Standard Resource Synchronization
   - Tests that all other resources are synchronized normally
   - Verifies ConfigMap synchronization
   - Verifies Secret synchronization
   - Verifies Service synchronization
   - Verifies Ingress synchronization

4. Status Updates
   - Verifies the Replication resource status is updated correctly
   - Checks for "Synced: True" condition

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 04

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All resources should be synchronized to the DR cluster
- Deployment without override label should have 0 replicas in DR cluster
- Deployment with override label should maintain 3 replicas in DR cluster
- Replication status should show successful synchronization
- All other resource attributes should match the source cluster
