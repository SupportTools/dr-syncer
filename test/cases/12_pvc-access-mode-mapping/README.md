# Test Case 12: PVC Access Mode Mapping

## Purpose
This test case verifies the DR Syncer controller's ability to properly map PVC access modes between source and destination clusters. It tests that PVCs are correctly synchronized while applying configured access mode mappings to ensure appropriate access patterns are used in the DR cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- Access mode mapping configuration:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
    accessModeMapping:
      ReadWriteOnce: ReadWriteOnce     # Keep RWO as is
      ReadWriteMany: ReadWriteMany     # Keep RWM as is
      ReadOnlyMany: ReadWriteMany      # Map ROM to RWM
      ReadWriteOncePod: ReadWriteOnce  # Map RWOP to RWO
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. ReadWriteOnce PVC:
   - Uses `ReadWriteOnce` access mode
   - Single node access
   - Standard configuration

2. ReadWriteMany PVC:
   - Uses `ReadWriteMany` access mode
   - Multi-node access
   - Shared storage configuration

3. ReadOnlyMany PVC:
   - Uses `ReadOnlyMany` access mode
   - Read-only shared access
   - Distributed configuration

4. ReadWriteOncePod PVC:
   - Uses `ReadWriteOncePod` access mode
   - Pod-exclusive access
   - Performance optimized

5. Supporting Resources:
   - Deployment with PVC mounts
   - ConfigMap for application settings
   - Service for network access

## What is Tested
1. Access Mode Mapping
   - Verifies access mode translation
   - Verifies mapping configuration
   - Verifies default handling
   - Verifies mode preservation

2. PVC Creation
   - Verifies PVCs created with correct modes
   - Verifies volume attributes preserved
   - Verifies storage classes maintained
   - Verifies capacity requests matched

3. Access Patterns
   - Verifies ReadWriteOnce handling
   - Verifies ReadWriteMany handling
   - Verifies ReadOnlyMany mapping
   - Verifies ReadWriteOncePod mapping

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
./test/run-tests.sh --test 12

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster
- Access modes should be correctly mapped
- Volume attributes should be preserved
- Storage classes should be maintained
- Capacity requests should match
- Deployment mounts should be configured
- Status should show successful synchronization
- Each PVC should use its mapped access mode
