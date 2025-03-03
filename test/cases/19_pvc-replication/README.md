# PVC Replication Test Case

This test case verifies the PVC replication functionality using the rsync deployment controller.

## Test Overview

This test simulates a disaster recovery scenario where persistent volume data needs to be replicated from a source cluster to a destination cluster. It tests the complete flow of:

1. Deploying an rsync deployment on the destination cluster and namespace
2. Starting with a pod in a waiting state
3. Generating SSH keys and establishing secure connectivity between clusters
4. Finding the source PVC mount path
5. Performing data synchronization using rsync
6. Verifying successful replication of data

## What This Test Validates

- PVC discovery in source cluster
- Node selection where the PVC is mounted
- Agent pod discovery on source node
- Deployment creation on destination cluster
- SSH key generation and exchange
- Rsync data transfer process
- Annotation updates on source PVC

## Test Steps

1. **Setup**: Creates namespaces, PVCs, and a pod in the source cluster that writes test data
2. **Deploy**: Applies a NamespaceMapping CR to trigger PVC replication
3. **Verify**: Creates a test pod in the destination cluster to verify data was replicated correctly
4. **Cleanup**: Removes all test resources

## Usage

Run this test directly:

```bash
./test.sh
```

Or as part of the test suite:

```bash
../run-tests.sh 19_pvc-replication
```

## Expected Output

Successful test execution will show:
- Creation of test resources
- Progress of replication process
- Verification that test data was successfully replicated
- "✅ PVC replication test passed!" message
