# Test Case 09: PVC Handling

## Purpose
This test case verifies the DR Syncer controller's ability to properly handle Persistent Volume Claims (PVCs) in the DR cluster. It tests that PVCs are correctly synchronized while maintaining their specifications, access modes, and storage requirements.

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
- Configures PVC handling:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
    preserveStorageClass: true
    preserveAccessModes: true
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Basic PVC (`test-pvc-basic`)
   - Standard storage class
   - ReadWriteOnce access mode
   - Fixed size request

2. Multi-Access PVC (`test-pvc-multi`)
   - ReadWriteMany access mode
   - Multiple pod access
   - Shared storage configuration

3. Dynamic PVC (`test-pvc-dynamic`)
   - Dynamic provisioning
   - Storage class with provisioner
   - Automatic volume binding

4. Deployment with PVC Mounts
   - References all test PVCs
   - Multiple volume mounts
   - Different mount paths

## What is Tested
1. PVC Creation
   - Verifies PVCs are created in DR cluster
   - Verifies storage classes are preserved
   - Verifies access modes are maintained
   - Verifies capacity requests are matched

2. PVC Specifications
   - Verifies volume mode settings
   - Verifies storage requirements
   - Verifies volume attributes
   - Verifies selector configurations

3. PVC Access Modes
   - Verifies ReadWriteOnce configuration
   - Verifies ReadWriteMany configuration
   - Verifies access mode preservation
   - Verifies mount options

4. Storage Classes
   - Verifies storage class references
   - Verifies provisioner settings
   - Verifies storage parameters
   - Verifies reclaim policies

5. Volume Binding
   - Verifies binding mode settings
   - Verifies volume binding status
   - Verifies wait conditions
   - Verifies binding timeouts

6. Supporting Resources
   - Verifies Deployment synchronization
   - Verifies PVC mount configurations
   - Verifies volume references
   - Verifies namespace settings

7. Status Updates
   - Verifies the NamespaceMapping resource status
   - Checks for "Synced: True" condition
   - Verifies PVC-specific status fields
   - Monitors binding progress

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 09

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster
- Storage classes should be preserved
- Access modes should be maintained
- Capacity requests should match
- Volume attributes should be preserved
- Deployment mounts should be configured correctly
- Binding modes should be respected
- Status should show successful synchronization
