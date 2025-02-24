# Test Case 08: Namespace Mapping

## Purpose
This test case verifies the DR Syncer controller's ability to properly handle namespace mapping between source and destination clusters. It tests that resources can be synchronized from one namespace to another while maintaining proper references and configurations.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates multiple Replication resources in the `dr-syncer` namespace:
  1. Direct Mapping:
     ```yaml
     sourceNamespace: dr-sync-test-case08-a
     destinationNamespace: dr-sync-test-case08-b
     ```
  2. Wildcard Mapping:
     ```yaml
     sourceNamespace: dr-sync-test-case08-*
     destinationNamespace: dr-sync-mapped-{name}
     ```

### Source Resources (`remote.yaml`)
Deploys test resources in multiple source namespaces:
1. First Namespace (`dr-sync-test-case08-a`):
   - ConfigMap (`test-configmap-a`)
   - Secret (`test-secret-a`)
   - Deployment (`test-deployment-a`)
   - Service (`test-service-a`)
   - Ingress (`test-ingress-a`)

2. Second Namespace (`dr-sync-test-case08-b`):
   - ConfigMap (`test-configmap-b`)
   - Secret (`test-secret-b`)
   - Deployment (`test-deployment-b`)
   - Service (`test-service-b`)
   - Ingress (`test-ingress-b`)

3. Third Namespace (`dr-sync-test-case08-c`):
   - ConfigMap (`test-configmap-c`)
   - Secret (`test-secret-c`)
   - Deployment (`test-deployment-c`)
   - Service (`test-service-c`)
   - Ingress (`test-ingress-c`)

## What is Tested
1. Direct Namespace Mapping
   - Verifies resources from namespace A are synced to namespace B
   - Verifies namespace B is created if it doesn't exist
   - Verifies all resource references are updated to new namespace

2. Wildcard Namespace Mapping
   - Verifies wildcard pattern matches correct namespaces
   - Verifies destination namespace naming pattern works
   - Verifies resources are synced to correct mapped namespaces

3. Resource Configuration
   - Verifies all resource types are properly synchronized
   - Verifies namespace references in resources are updated
   - Verifies cross-namespace references are maintained
   - Verifies label selectors are preserved

4. Resource Types
   - Verifies ConfigMap synchronization with namespace mapping
   - Verifies Secret synchronization with namespace mapping
   - Verifies Deployment synchronization with namespace mapping
   - Verifies Service synchronization with namespace mapping
   - Verifies Ingress synchronization with namespace mapping

5. Status Updates
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
- All namespaces should be created in DR cluster
- Resources should be synchronized to correct mapped namespaces
- All namespace references should be updated correctly
- Resource configurations should be preserved
- Cross-namespace references should be maintained
- Label selectors should work correctly
- Replication status should show successful synchronization
