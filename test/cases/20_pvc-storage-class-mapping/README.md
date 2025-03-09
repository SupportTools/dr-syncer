# Test Case 20: PVC Storage Class Mapping

## Purpose
This test case verifies the PVC storage class mapping functionality of the DR Syncer controller between the dr-syncer-nyc3 and dr-syncer-sfo3 clusters. It focuses on replicating a PVC with a different storage class in the DR cluster.

## Test Configuration

### Controller Resources (`controller.yaml`)
- Creates RemoteCluster resources for the source and target clusters
- Creates a ClusterMapping resource to connect the source and target clusters
- Creates a NamespaceMapping resource in the `dr-syncer` namespace
- Configures PVC replication with:
  - Storage class mapping from `do-block-storage-xfs` to `do-block-storage-xfs-retain`
  - Does not preserve volume attributes like volumeName
  - Does not sync persistent volumes (creates new ones in DR)

### Source Resources (`remote.yaml`)
Deploys the following resources in the source namespace:
- A PVC for storage class mapping (`test-pvc-storage-class`)
- Deployment that uses the PVC
- Service for the deployment

## What is Tested

1. Storage Class Mapping
   - Verifies PVC is replicated with the mapped storage class
   - Checks that `do-block-storage-xfs` is mapped to `do-block-storage-xfs-retain`
   - Confirms all other attributes are preserved
   - Verifies the PVC can bind to a new PV in the DR cluster

2. Data Replication
   - Writes timestamp data to the PVC in the source cluster
   - Verifies the data is correctly replicated to the DR cluster
   - Confirms file contents match exactly between source and DR

3. Deployment and Service Replication
   - Verifies deployment is replicated with 0 replicas in DR
   - Confirms volume mounts reference the correct PVC
   - Verifies service is properly replicated

4. Status Updates
   - Verifies the NamespaceMapping resource status is updated correctly
   - Checks for "Synced: True" condition
   - Confirms sync statistics are tracked

## How to Run
```bash
# Run this test case only
./test/run-tests.sh --test 20

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results
- The PVC should be synchronized to the DR cluster
- `test-pvc-storage-class` should have `do-block-storage-xfs-retain` storage class in DR
- The PVC should be bound to a new PV in the DR cluster
- Data written to the PVC should be replicated to the DR cluster
- Deployment should have 0 replicas in DR cluster
- Replication status should show successful synchronization
