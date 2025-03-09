# Test Case 14: PVC Sync with Volume Name Preservation

## Purpose
This test case verifies the DR Syncer controller's ability to properly synchronize PVCs while preserving their volume name references. It tests that PVCs are synchronized to the DR cluster with their `volumeName` field intact, which helps maintain the relationship between PVCs and their volumes.

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
- PVC and PV configuration:
  ```yaml
  pvcConfig:
    syncPersistentVolumes: true  # Key difference from test case 13
    preserveVolumeAttributes: true
    syncData: false
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Static PV and PVC:
   - Pre-provisioned PV
   - Static binding
   - Fixed capacity

2. Dynamic PV and PVC:
   - Dynamic provisioning
   - Automatic binding
   - Storage class based

3. Local PV and PVC:
   - Node-specific volume
   - Local path configuration
   - Node affinity rules

4. NFS PV and PVC:
   - NFS server configuration
   - Network storage
   - Shared access

5. Block Device PV and PVC:
   - Raw block device
   - Direct volume access
   - Device path configuration

6. Supporting Resources:
   - Deployment with PVC mounts
   - ConfigMap for application settings
   - Service for network access

## What is Tested
1. PV Creation
   - Verifies PVs are created in DR cluster
   - Verifies volume configurations
   - Verifies source attributes
   - Verifies binding status

2. PVC Binding
   - Verifies PVC-PV binding
   - Verifies binding modes
   - Verifies claim references
   - Verifies binding status

3. Volume Types
   - Verifies static volumes
   - Verifies dynamic volumes
   - Verifies local volumes
   - Verifies network volumes
   - Verifies block devices

4. Volume Attributes
   - Verifies capacity settings
   - Verifies access modes
   - Verifies reclaim policies
   - Verifies mount options
   - Verifies volume modes
   - Verifies node affinity

5. Storage Configuration
   - Verifies storage class references
   - Verifies provisioner settings
   - Verifies storage parameters
   - Verifies volume sources

6. Node Affinity
   - Verifies node selector terms
   - Verifies topology constraints
   - Verifies placement rules
   - Verifies node matching

7. Mount Configuration
   - Verifies mount options
   - Verifies filesystem settings
   - Verifies device paths
   - Verifies access patterns

8. Supporting Resources
   - Verifies deployment synchronization
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
./test/run-tests.sh --test 14

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster with volume name references preserved
- PVCs in the DR cluster should have the same volumeName as in the source cluster
- PVCs may remain in Pending state since actual PVs aren't synchronized (just their references)
- Volume attributes should be preserved in the PVC specifications
- Deployment specifications should be preserved with volume mounts
- Status should show successful synchronization
