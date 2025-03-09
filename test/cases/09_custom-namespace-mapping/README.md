# Test Case 09: Custom Namespace Mapping

## Purpose
This test case verifies the DR Syncer controller's ability to properly handle custom destination namespace mapping functionality. It tests mapping a source namespace to a destination namespace with a different name, ensuring resources are correctly synchronized between clusters.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a NamespaceMapping resource in the `dr-syncer` namespace
- Maps `namespace-prod` directly to `namespace-prod-dr` (using a suffix naming convention)
- Uses the ClusterMapping reference `prod-to-dr`
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- Scales deployments to zero in DR cluster
- Preserves namespace labels and annotations

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
- Namespace with labels and annotations
- ConfigMap (`test-configmap`) for application configuration
- Secret (`test-secret`) with credentials
- Deployment (`test-deployment`) with multiple replicas
- Service (`test-service`) for the deployment
- Ingress (`test-ingress`) with host and path-based routing

## What is Tested

1. Custom Destination Namespace Mapping
   - Verifies direct mapping to a custom destination namespace name
   - Tests explicit naming with suffix (`-dr`)
   - Ensures namespace is created in DR cluster with the custom name
   - Validates that labels and annotations are preserved

2. Resource Synchronization
   - Verifies ConfigMap synchronization
   - Verifies Secret synchronization
   - Verifies Deployment synchronization
   - Verifies Service synchronization
   - Verifies Ingress synchronization
   - Ensures all resource metadata is preserved

3. Scaling Configuration
   - Verifies deployments are scaled to zero in DR cluster
   - Ensures the original replicas count is maintained in the spec

4. Status Updates
   - Verifies the NamespaceMapping resource status is updated correctly
   - Checks for "Synced: True" condition
   - Validates sync statistics (successful/failed syncs)
   - Verifies sync timestamps are present

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 09

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- Custom destination namespace mapping should work correctly
- All resources should be synchronized to DR cluster with the custom namespace name
- Namespace metadata should be preserved according to configuration
- Deployments should be scaled to zero in the DR environment
- Replication status should show successful synchronization