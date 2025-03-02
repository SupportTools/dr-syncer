# PVC Data Replication Test

This test verifies the functionality of PVC data replication between clusters. It tests the ability to synchronize the actual data inside Persistent Volume Claims from a source cluster to a destination cluster using the enhanced PVC mounting and rsync functionality.

## What this test does

1. Creates a namespace and PVCs in both source and destination clusters
2. Creates a pod in the source cluster that writes various test data to the source PVC:
   - Simple text files
   - Large binary files (1MB)
   - Nested directory structures
   - Files with special characters
   - Symlinks
   - Files with different permissions
3. Creates a replication resource with `syncData: true` and enhanced rsync options
4. Triggers the replication process
5. Creates a pod in the destination cluster to read data from the destination PVC
6. Performs comprehensive verification of the replicated data:
   - Content verification for text files
   - Checksum verification for binary files
   - Directory structure verification
   - Symlink preservation verification
   - File permission preservation verification
7. Verifies replication status and statistics

## Components tested

- PVC data synchronization using rsync with enhanced options
- SSH key management for secure data transfer
- Node affinity for optimal PVC mounting
- Resource management for mount pods
- Replication status updates for PVC sync operations
- Error handling and recovery
- File metadata preservation (permissions, symlinks)

## Enhanced Features Tested

- Node affinity for PVC mounting
- Resource limits for mount pods
- Improved error handling and logging
- Configurable rsync options
- Comprehensive data verification

## Expected outcome

The test is successful if all aspects of the data written to the source PVC are correctly replicated to the destination PVC, including:
- File content
- File sizes
- Directory structures
- Symlinks
- File permissions

This demonstrates that the enhanced PVC data synchronization mechanism works as expected and preserves all important file attributes.
