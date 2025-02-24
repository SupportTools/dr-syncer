# Test Case 14: PVC Sync Persistent Volumes

## Purpose
This test case verifies the DR Syncer controller's ability to properly synchronize PVCs along with their associated Persistent Volumes (PVs). It tests that both PVCs and their bound PVs are correctly synchronized to the DR cluster while maintaining their relationships and configurations.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- PVC and PV configuration:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
    syncPersistentVolumes: true
    volumeConfig:
      preserveCapacity: true
      preserveAccessModes: true
      preserveReclaimPolicy: true
      preserveMountOptions: true
      preserveVolumeMode: true
      preserveNodeAffinity: true
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
   - Verifies the Replication resource status
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
- All PVs should be synchronized to DR cluster
- All PVCs should be synchronized to DR cluster
- PVC-PV bindings should be maintained
- Volume configurations should be preserved
- Storage settings should be matched
- Node affinity rules should be preserved
- Mount configurations should be kept
- Deployment mounts should be configured
- Status should show successful synchronization
