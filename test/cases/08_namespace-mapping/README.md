# Test Case 08: Namespace Mapping

## Purpose
This test case verifies the DR Syncer controller's ability to properly handle namespace mapping between source and destination clusters. It tests that resources can be synchronized from one namespace to another while maintaining proper references and configurations.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace:
  ```yaml
  sourceNamespace: Namespace-Prod
  destinationNamespace: Namespace-DR
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace (`Namespace-Prod`):
- ConfigMap (`test-configmap`)
- Secret (`test-secret`)
- Deployment (`test-deployment`)
- Service (`test-service`)
- Ingress (`test-ingress`)

## What is Tested
1. Direct Namespace Mapping
   - Verifies resources from Namespace-Prod are synced to Namespace-DR
   - Verifies Namespace-DR is created if it doesn't exist
   - Verifies all resource references are updated to new namespace

2. Resource Configuration
   - Verifies all resource types are properly synchronized
   - Verifies namespace references in resources are updated
   - Verifies cross-namespace references are maintained
   - Verifies label selectors are preserved

3. Resource Types
   - Verifies ConfigMap synchronization with namespace mapping
   - Verifies Secret synchronization with namespace mapping
   - Verifies Deployment synchronization with namespace mapping
   - Verifies Service synchronization with namespace mapping
   - Verifies Ingress synchronization with namespace mapping

4. Status Updates
   - Verifies the Replication resource status is updated correctly
   - Checks for "Synced: True" condition
   - Verifies namespace-specific status fields

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 08

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- Namespace-DR should be created in DR cluster
- All resources should be synchronized from Namespace-Prod to Namespace-DR
- All namespace references should be updated correctly
- Resource configurations should be preserved
- Cross-namespace references should be maintained
- Label selectors should work correctly
- Replication status should show successful synchronization
