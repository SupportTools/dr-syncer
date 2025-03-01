# PVC Data Replication Test

This test verifies the functionality of PVC data replication between clusters. It tests the ability to synchronize the actual data inside Persistent Volume Claims from a source cluster to a destination cluster.

## What this test does

1. Creates a namespace and PVCs in both source and destination clusters
2. Creates a pod in the source cluster that writes test data to the source PVC
3. Creates a replication resource with `syncData: true` to enable data replication
4. Triggers the replication process
5. Creates a pod in the destination cluster to read data from the destination PVC
6. Verifies that the data was correctly replicated from source to destination

## Components tested

- PVC data synchronization using rsync
- SSH key management for secure data transfer
- Replication status updates for PVC sync operations

## Expected outcome

The test is successful if the data written to the source PVC is correctly replicated to the destination PVC, demonstrating that the PVC data synchronization mechanism works as expected.
