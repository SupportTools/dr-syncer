# Test Case 10: PVC Basic Sync

## Purpose
This test case verifies the DR Syncer controller's basic PVC synchronization capabilities without any special configurations or mappings. It tests that PVCs are correctly synchronized while maintaining their original specifications exactly as they are in the source cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- Basic PVC configuration:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Basic PVC Types:
   - Standard PVC (`test-pvc-standard`)
     * Default storage class
     * Basic volume attributes
     * Simple configuration

   - Block PVC (`test-pvc-block`)
     * Block volume mode
     * Raw device access
     * Direct volume binding

   - Filesystem PVC (`test-pvc-filesystem`)
     * Filesystem volume mode
     * Standard filesystem access
     * Default mounting options

2. Supporting Resources:
   - Deployment with PVC mounts
   - ConfigMap for application settings
   - Service for network access

## What is Tested
1. Basic PVC Creation
   - Verifies PVCs are created in DR cluster
   - Verifies basic attributes are preserved
   - Verifies volume modes are maintained
   - Verifies storage requests match

2. Volume Modes
   - Verifies block device configuration
   - Verifies filesystem configuration
   - Verifies mode preservation
   - Verifies access patterns

3. Storage Configuration
   - Verifies storage class references
   - Verifies capacity requests
   - Verifies volume binding
   - Verifies access modes

4. Resource References
   - Verifies deployment volume mounts
   - Verifies PVC references
   - Verifies namespace settings
   - Verifies label preservation

5. Status Verification
   - Verifies binding status
   - Verifies phase transitions
   - Verifies capacity tracking
   - Verifies access status

6. Supporting Resources
   - Verifies deployment synchronization
   - Verifies configmap synchronization
   - Verifies service synchronization
   - Verifies resource relationships

7. Status Updates
   - Verifies the Replication resource status
   - Checks for "Synced: True" condition
   - Verifies PVC-specific status fields
   - Monitors binding progress

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 10

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster
- Volume modes should be preserved exactly
- Storage requests should match source
- Access modes should be maintained
- Volume attributes should be preserved
- Deployment mounts should be configured
- Status should show successful synchronization
- No special mappings or transformations should occur
