# Test Case 11: PVC Storage Class Mapping

## Purpose
This test case verifies the DR Syncer controller's ability to properly map storage classes between source and destination clusters. It tests that PVCs are correctly synchronized while applying configured storage class mappings to ensure appropriate storage types are used in the DR cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates a Replication resource in the `dr-syncer` namespace
- Uses wildcard resource type selection:
  ```yaml
  resourceTypes:
    - "*"  # Synchronize all resource types
  ```
- Storage class mapping configuration:
  ```yaml
  pvcConfig:
    enabled: true
    preserveVolumeAttributes: true
    storageClassMapping:
      standard: standard-dr       # Map standard to DR-specific class
      premium-ssd: premium-dr    # Map premium SSD to DR premium class
      high-iops: performance-dr  # Map high IOPS to DR performance class
      archive: backup-dr         # Map archive to DR backup class
  ```

### Source Resources (`remote.yaml`)
Deploys test resources in the source namespace:
1. Standard Storage PVC:
   - Uses `standard` storage class
   - Basic performance requirements
   - Default configuration

2. Premium SSD PVC:
   - Uses `premium-ssd` storage class
   - High performance requirements
   - SSD-specific configuration

3. High IOPS PVC:
   - Uses `high-iops` storage class
   - IOPS-optimized configuration
   - Performance settings

4. Archive Storage PVC:
   - Uses `archive` storage class
   - Backup/archive configuration
   - Cost-optimized settings

5. Supporting Resources:
   - Deployment with PVC mounts
   - ConfigMap for application settings
   - Service for network access

## What is Tested
1. Storage Class Mapping
   - Verifies storage class translation
   - Verifies mapping configuration
   - Verifies default handling
   - Verifies class preservation

2. PVC Creation
   - Verifies PVCs created with correct classes
   - Verifies volume attributes preserved
   - Verifies access modes maintained
   - Verifies capacity requests matched

3. Storage Types
   - Verifies standard storage mapping
   - Verifies SSD storage mapping
   - Verifies IOPS storage mapping
   - Verifies archive storage mapping

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
./test/run-tests.sh --test 11

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- All PVCs should be synchronized to DR cluster
- Storage classes should be correctly mapped
- Volume attributes should be preserved
- Access modes should be maintained
- Capacity requests should match
- Deployment mounts should be configured
- Status should show successful synchronization
- Each PVC should use its mapped storage class
