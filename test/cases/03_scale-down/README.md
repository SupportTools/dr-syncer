# Test Case 03: Scale Down Functionality

## Purpose
This test case verifies the DR Syncer controller's ability to properly scale down deployments in the DR cluster. It tests that deployments with non-zero replica counts in the source cluster are correctly synchronized to the DR cluster with zero replicas, while maintaining all other deployment configurations.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```

### Source Resources (`remote.yaml`)
Deploys standard test resources in the source namespace:
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- Deployment (`test-deployment`) with 3 replicas
- Service (`test-service`)
- Ingress (`test-ingress`)

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Deployment Scale Down
   - Verifies deployment with 3 replicas in source cluster is synced with 0 replicas in DR cluster
   - Confirms all other deployment configurations are preserved
   - Ensures the scale down doesn't affect other deployment attributes

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
./test/run-tests.sh --test 03

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All resources should be synchronized to the DR cluster
- Deployment should have 0 replicas in DR cluster (scaled down from 3)
- Replication status should show successful synchronization
- All other resource attributes should match the source cluster
