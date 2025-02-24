# Test Case 07: Ingress Handling

## Purpose
This test case verifies the DR Syncer controller's ability to properly handle Ingress resources in the DR cluster. It tests that Ingress configurations are correctly synchronized, including various annotations, rules, TLS configurations, and backend service references.

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
- Services:
  * Backend Service 1 (`test-service-1`)
  * Backend Service 2 (`test-service-2`)
- Ingress Types:
  * Basic Ingress (`test-ingress-basic`)
    - Single host rule
    - Path-based routing
  * Complex Ingress (`test-ingress-complex`)
    - Multiple host rules
    - Multiple path rules
    - TLS configuration
    - Various annotations
  * Annotated Ingress (`test-ingress-annotations`)
    - Provider-specific annotations
    - Custom configurations
    - Rate limiting rules
  * Default Backend Ingress (`test-ingress-default`)
    - Default backend configuration
    - Fallback routing

## What is Tested
1. Namespace Creation
   - Verifies the target namespace is created in the DR cluster

2. Ingress Configuration Preservation
   - Verifies all host rules are preserved
   - Verifies all path configurations are maintained
   - Verifies TLS configurations are synchronized
   - Verifies all annotations are preserved
   - Verifies backend service references are correct

3. Ingress Types
   - Verifies basic ingress with simple routing
   - Verifies complex ingress with multiple rules
   - Verifies annotated ingress with provider configurations
   - Verifies default backend ingress functionality

4. Supporting Resources
   - Verifies ConfigMap synchronization
   - Verifies Deployment synchronization
   - Verifies Service synchronization
   - Ensures backend services are properly referenced

5. Status Updates
   - Verifies the Replication resource status is updated correctly
   - Checks for "Synced: True" condition
   - Verifies ingress-specific status fields

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 07

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All ingress resources should be synchronized to DR cluster
- All ingress configurations should match source exactly
- Host rules should be preserved
- Path configurations should be maintained
- TLS configurations should be synchronized
- Annotations should be preserved
- Backend service references should be correct
- Supporting resources should be properly synchronized
- Replication status should show successful synchronization
