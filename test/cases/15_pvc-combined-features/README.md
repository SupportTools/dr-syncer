# Test Case 15: PVC Combined Features

## Purpose
This test case verifies the DR Syncer controller's ability to handle multiple PVC features simultaneously, ensuring that all features work correctly in combination. It tests the interaction between storage class mapping, access mode mapping, attribute preservation, and PV synchronization to ensure they function properly together.

## Implementation Notes
This test case validates the combined functionality of:
1. Storage class mapping from generic classes to DO-specific storage classes
2. Access mode mapping (ReadOnlyMany -> ReadWriteMany)
3. Volume attributes preservation
4. PV synchronization with volume name preservation
5. Multiple PV types - standard, static, local, and block volumes

The test has been designed to be resilient against pre-existing stuck PVs and includes cleanup mechanisms to force delete resources that might be stuck in terminating states.

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
- Combined PVC configuration:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
    syncPersistentVolumes: true
    waitForBinding: true
    bindingTimeout: 5m
    
    # Storage class mapping
    storageClassMapping:
      standard: dr-standard
      premium: dr-premium
      local-storage: dr-local
      block-storage: dr-block
    
    # Access mode mapping
    accessModeMapping:
      ReadWriteOnce: ReadWriteOnce
      ReadWriteMany: ReadWriteMany
      ReadOnlyMany: ReadWriteMany
      ReadWriteOncePod: ReadWriteOnce
    
    # Volume configuration
    volumeConfig:
      preserveCapacity: true
      preserveAccessModes: true
      preserveReclaimPolicy: true
      preserveMountOptions: true
      preserveVolumeMode: true
      preserveNodeAffinity: true
      preserveVolumeSource: true
      preserveStorageClass: false  # Using storage class mapping
    
    # Volume attributes to preserve
    preservedAttributes:
      - volumeMode
      - resources
      - selector
      - volumeName
      - dataSource
      - mountOptions
      - nodeAffinity
      - annotations
      - labels
      - finalizers
      - ownerReferences
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Standard PVC with Storage Class Mapping:
   - Uses `standard` storage class
   - Maps to `dr-standard` in DR
   - Basic configuration

2. Premium PVC with Access Mode Mapping:
   - Uses `premium` storage class
   - ReadOnlyMany access mode
   - Maps to ReadWriteMany in DR

3. Local PVC with Node Affinity:
   - Uses `local-storage` class
   - Node-specific volume
   - Preserves node affinity

4. Block PVC with Volume Mode:
   - Uses `block-storage` class
   - Raw block device
   - Preserves volume mode

5. Dynamic PVC with Resource Limits:
   - Dynamic provisioning
   - Resource requirements
   - Performance settings

6. Static PVC with Mount Options:
   - Pre-provisioned volume
   - Custom mount options
   - Static binding

7. Supporting Resources:
   - Deployment with PVC mounts
   - ConfigMap for application settings
   - Service for network access

## What is Tested
1. Storage Class Mapping
   - Verifies class translation
   - Verifies mapping configuration
   - Verifies default handling
   - Verifies class preservation

2. Access Mode Mapping
   - Verifies mode translation
   - Verifies mapping rules
   - Verifies mode compatibility
   - Verifies access patterns

3. Volume Attributes
   - Verifies attribute preservation
   - Verifies selective overrides
   - Verifies metadata handling
   - Verifies configuration options

4. PV Synchronization
   - Verifies PV creation
   - Verifies binding status
   - Verifies volume sources
   - Verifies PV attributes

5. Resource Requirements
   - Verifies capacity settings
   - Verifies resource limits
   - Verifies request handling
   - Verifies performance options

6. Node Affinity
   - Verifies topology constraints
   - Verifies node selection
   - Verifies affinity rules
   - Verifies placement control

7. Mount Configuration
   - Verifies mount options
   - Verifies filesystem settings
   - Verifies device paths
   - Verifies access patterns

8. Feature Interactions
   - Verifies combined mappings
   - Verifies attribute conflicts
   - Verifies priority handling
   - Verifies configuration merging

9. Status Updates
   - Verifies the NamespaceMapping resource status
   - Checks for "Synced: True" condition
   - Verifies PVC-specific status fields
   - Monitors binding progress

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 15

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster
- Storage classes should be correctly mapped
- Access modes should be properly translated
- Volume attributes should be preserved as configured
- PVs should be synchronized where specified
- Resource requirements should be maintained
- Node affinity rules should be preserved
- Mount configurations should be kept
- Deployment mounts should be configured
- Status should show successful synchronization
