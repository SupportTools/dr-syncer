# Test Case 13: PVC Preserve Attributes

## Purpose
This test case verifies the DR Syncer controller's ability to preserve specific PVC attributes during synchronization. It tests that various PVC attributes including volume mode, resource requirements, selectors, volume names, data sources, mount options, and node affinity are correctly maintained when synchronizing to the target cluster.

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
- PVC configuration with attribute preservation:
  ```yaml
  pvcConfig:
    syncPersistentVolumes: false
    preserveVolumeAttributes: true
    syncData: false
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Different PVC Attribute Types:
   - Basic PVC with volume mode
   - PVC with resource requirements (using annotations for non-standard fields)
   - PVC with selector
   - PVC with volume name (using annotations to ensure portability)
   - PVC with data source
   - PVC with mount options (using annotations)
   - PVC with node affinity (using annotations in JSON format)

2. Supporting Resources:
   - Deployment with PVC mounts for all attribute types
   - ConfigMap for application settings
   - Service for network access

Note: This test uses annotations for certain PVC attributes that might cause API validation issues when synchronized directly. This approach ensures test compatibility with various Kubernetes distributions while still validating DR-Syncer's ability to preserve these important attributes.

## What is Tested

1. Volume Mode Preservation
   - Verifies Filesystem mode is preserved
   - Verifies Block mode is preserved (if available)

2. Resource Requirements Preservation
   - Verifies standard storage requests are preserved directly
   - Verifies extended resources (IOPS, etc.) are preserved via annotations
   - Verifies resource limits are preserved via annotations

3. Selector Preservation
   - Verifies label selectors are maintained
   - Verifies matchExpressions are preserved

4. Volume Name Preservation
   - Verifies volume name references are maintained via annotations
   - Tests DR-Syncer's ability to handle pre-provisioned volumes safely

5. Data Source Preservation
   - Verifies data source references are preserved
   - Verifies data source API group is maintained

6. Mount Options Preservation
   - Verifies mount options are preserved via annotations
   - Verifies ordering is maintained

7. Node Affinity Preservation
   - Verifies node affinity rules are preserved via annotations (JSON format)
   - Verifies complex selector terms are properly maintained

8. Supporting Resources
   - Verifies deployment synchronization with PVC volume mounts
   - Verifies configmap synchronization
   - Verifies service synchronization
   - Verifies resource relationships

9. Status Updates
   - Verifies the NamespaceMapping resource status
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
- All PVCs should be synchronized to DR cluster with attributes preserved
- Volume modes should be preserved exactly
- Resource requests should be preserved exactly
- Selectors should be preserved exactly
- Volume names should be preserved exactly
- Data sources should be preserved exactly
- Mount options should be preserved exactly
- Node affinity should be preserved exactly
- Deployment should be scaled to zero but otherwise identical
- Status should show successful synchronization