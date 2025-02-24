# Test Case 13: PVC Preserve Attributes

## Purpose
This test case verifies the DR Syncer controller's ability to properly preserve specific PVC attributes during synchronization. It tests that PVCs are correctly synchronized while maintaining specified volume attributes, ensuring critical configurations are preserved in the DR cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- PVC attribute preservation configuration:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
    preservedAttributes:
      - volumeMode
      - resources
      - selector
      - volumeName
      - dataSource
      - storageClassName
      - mountOptions
      - nodeAffinity
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Basic PVC with Volume Mode:
   - Uses `Filesystem` volume mode
   - Basic configuration
   - Standard storage class

2. PVC with Resource Requirements:
   - Specific storage requests
   - Resource limits
   - Performance settings

3. PVC with Selector:
   - Label selector configuration
   - Match expressions
   - Volume binding control

4. PVC with Volume Name:
   - Pre-provisioned volume
   - Static binding
   - Volume reference

5. PVC with Data Source:
   - Snapshot data source
   - Clone configuration
   - Source reference

6. PVC with Mount Options:
   - Custom mount options
   - File system settings
   - Performance tuning

7. PVC with Node Affinity:
   - Node selector terms
   - Topology constraints
   - Placement rules

8. Supporting Resources:
   - Deployment with PVC mounts
   - ConfigMap for application settings
   - Service for network access

## What is Tested
1. Volume Mode Preservation
   - Verifies volume mode settings
   - Verifies filesystem configuration
   - Verifies block device settings
   - Verifies mode consistency

2. Resource Requirements
   - Verifies storage requests
   - Verifies resource limits
   - Verifies capacity settings
   - Verifies request preservation

3. Selector Configuration
   - Verifies label selectors
   - Verifies match expressions
   - Verifies selector preservation
   - Verifies binding control

4. Volume Name Reference
   - Verifies volume name preservation
   - Verifies static binding
   - Verifies volume references
   - Verifies name consistency

5. Data Source Settings
   - Verifies snapshot sources
   - Verifies clone configurations
   - Verifies source references
   - Verifies data source preservation

6. Mount Options
   - Verifies custom mount options
   - Verifies filesystem settings
   - Verifies performance options
   - Verifies option preservation

7. Node Affinity Rules
   - Verifies node selector terms
   - Verifies topology constraints
   - Verifies placement rules
   - Verifies affinity preservation

8. Supporting Resources
   - Verifies deployment synchronization
   - Verifies configmap synchronization
   - Verifies service synchronization
   - Verifies resource relationships

9. Status Updates
   - Verifies the Replication resource status
   - Checks for "Synced: True" condition
   - Verifies PVC-specific status fields
   - Monitors binding progress

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 13

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster
- Volume modes should be preserved exactly
- Resource requirements should match
- Selectors should be maintained
- Volume names should be preserved
- Data sources should be referenced
- Mount options should be kept
- Node affinity rules should be preserved
- Deployment mounts should be configured
- Status should show successful synchronization
