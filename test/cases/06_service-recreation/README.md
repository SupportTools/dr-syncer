# Test Case 06: Service Recreation

## Purpose
This test case verifies the DR Syncer controller's ability to properly handle service recreation in the DR cluster. It tests that services are correctly synchronized with appropriate handling of cluster-specific fields (like ClusterIP) and that service configurations are properly preserved during recreation.

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
- ConfigMap (`test-configmap`) for application configuration
- Deployment (`test-deployment`) with multiple pods
- Service Types:
  * ClusterIP Service (`test-service-clusterip`)
  * NodePort Service (`test-service-nodeport`)
  * LoadBalancer Service (`test-service-loadbalancer`)
  * Headless Service (`test-service-headless`)
- Each service has:
  * Multiple ports and protocols
  * Various selectors
  * Different annotations and labels
  * Specific configurations per type

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Service Recreation
   - Verifies ClusterIP services are recreated with new IPs
   - Verifies NodePort services maintain port configurations
   - Verifies LoadBalancer services handle provider-specific fields
   - Verifies headless services maintain null ClusterIP

3. Service Configuration Preservation
   - Verifies all port configurations are preserved
   - Verifies all selectors are maintained
   - Verifies all labels are synchronized
   - Verifies all annotations are preserved
   - Verifies service type-specific fields are handled correctly

4. Supporting Resources
   - Verifies ConfigMap synchronization
   - Verifies Deployment synchronization
   - Ensures backend pods are properly referenced

5. Status Updates
   - Verifies the Replication resource status is updated correctly
   - Checks for "Synced: True" condition
   - Verifies service-specific status fields

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 06

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All services should be recreated in DR cluster
- Service configurations should match source (except cluster-specific fields)
- ClusterIP values should be different between clusters
- NodePort values should be preserved if specified
- LoadBalancer configurations should be properly handled
- Headless services should maintain null ClusterIP
- All selectors, labels, and annotations should match
- Supporting resources should be properly synchronized
- Replication status should show successful synchronization
