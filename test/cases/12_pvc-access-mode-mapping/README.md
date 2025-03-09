# Test Case 12: PVC Access Mode Mapping

## Purpose
This test case verifies the DR Syncer controller's ability to map PVC access modes between source and target clusters. It tests that PVCs with different access modes are correctly synchronized to the target cluster with the appropriate access modes according to the configured mappings.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates RemoteCluster resources for the source and target clusters
- Creates a ClusterMapping resource to connect the source and target clusters
- Creates a NamespaceMapping resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- PVC configuration with access mode mapping:
  ```yaml
  pvcConfig:
    syncPersistentVolumes: false
    preserveVolumeAttributes: true
    accessModeMappings:
    - from: ReadOnlyMany 
      to: ReadWriteMany
    - from: ReadWriteOncePod
      to: ReadWriteOnce
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Different PVC Access Modes:
   - ReadWriteOnce PVC (`test-pvc-rwo`)
     * Standard single-node access
     * Stays as ReadWriteOnce in DR cluster

   - ReadWriteMany PVC (`test-pvc-rwm`)
     * Multi-node read-write access
     * Stays as ReadWriteMany in DR cluster

   - ReadOnlyMany PVC (`test-pvc-rom`)
     * Multi-node read-only access
     * Maps to ReadWriteMany in DR cluster

   - ReadWriteOncePod PVC (`test-pvc-rwop`)
     * Single pod access
     * Maps to ReadWriteOnce in DR cluster

2. Supporting Resources:
   - Deployment with PVC mounts for all access modes
   - ConfigMap for application settings
   - Service for network access

## What is Tested

1. Access Mode Mapping
   - Verifies ReadWriteOnce is preserved
   - Verifies ReadWriteMany is preserved
   - Verifies ReadOnlyMany is mapped to ReadWriteMany
   - Verifies ReadWriteOncePod is mapped to ReadWriteOnce

2. Basic PVC Creation
   - Verifies PVCs are created in DR cluster
   - Verifies basic attributes are preserved
   - Verifies storage requests match
   - Verifies volume modes are maintained

3. Resource References
   - Verifies deployment volume mounts
   - Verifies PVC references
   - Verifies namespace settings
   - Verifies label preservation

4. Status Verification
   - Verifies binding status
   - Verifies phase transitions
   - Verifies capacity tracking
   - Verifies access status

5. Supporting Resources
   - Verifies deployment synchronization
   - Verifies configmap synchronization
   - Verifies service synchronization
   - Verifies resource relationships

6. Status Updates
   - Verifies the NamespaceMapping resource status
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
- PVCs should be synchronized to DR cluster with appropriate access modes
- ReadWriteOnce should remain ReadWriteOnce
- ReadWriteMany should remain ReadWriteMany
- ReadOnlyMany should be mapped to ReadWriteMany
- ReadWriteOncePod should be mapped to ReadWriteOnce
- All other PVC attributes should be preserved
- Deployment mounts should be configured properly
- Status should show successful synchronization