# Test Case 00: Basic Resource Synchronization

## Purpose
This test case verifies the basic resource synchronization functionality of the DR Syncer controller. It tests the controller's ability to synchronize explicitly specified resource types from a source namespace to a destination namespace in the DR cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates RemoteCluster resources for the source and target clusters
- Creates a ClusterMapping resource to connect the source and target clusters
- Creates a NamespaceMapping resource in the `dr-syncer` namespace to define replication
- Explicitly specifies resource types to sync:
  - ConfigMaps
  - Secrets
  - Deployments
  - Services
  - Ingresses

### Source Resources (`remote.yaml`)
Deploys the following resources in the source namespace:
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- Deployment (`test-deployment`)
- Service (`test-service`)
- Ingress (`test-ingress`)

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Resource Synchronization
   - ConfigMap synchronization
   - Secret synchronization
   - Deployment synchronization with zero replicas
   - Service synchronization
   - Ingress synchronization

3. Deployment Handling
   - Verifies deployments are synced with 0 replicas in the DR cluster
   - Preserves original deployment configuration for DR activation

4. Status Updates
   - Verifies the Replication resource status is updated correctly
   - Checks for "Synced: True" condition

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 00

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All resources should be synchronized to the DR cluster
- Deployments should have 0 replicas in DR cluster
- Replication status should show successful synchronization
- All other resource attributes should match the source cluster
