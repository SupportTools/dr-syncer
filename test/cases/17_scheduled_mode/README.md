# Scheduled Mode Test

This test case verifies the functionality of the Scheduled replication mode with ClusterMapping reference. In Scheduled mode, resources are synchronized on a periodic schedule defined by a cron expression.

## What This Test Does

1. Deploys a NamespaceMapping resource configured with `replicationMode: Scheduled` and a schedule of `*/5 * * * *` (every 5 minutes)
2. Verifies that resources are synced as part of the initial deployment
3. Tests the scheduled nature of the controller by:
   - Creating a new resource in the source namespace
   - Verifying it doesn't sync immediately (proving it's not continuous mode)
   - Verifying it is synced after the next scheduled run
4. Verifies that the controller properly calculates and displays the next scheduled run time

## Key Features Tested

- Scheduled replication mode with cron expression
- Schedule-based synchronization
- Proper scheduling of future sync operations
- ClusterMapping reference for cluster connectivity
- Resource synchronization across clusters
- Correct handling of PVC storage class mapping
- Verification of sync statistics and timing information

## Implementation Notes

The test includes specific functionality for testing the scheduled nature of the synchronization:

1. A function that creates a new ConfigMap in the source namespace with a timestamp
2. Verification that the ConfigMap is not synced immediately
3. Waiting for the next scheduled sync to verify the ConfigMap is synced
4. Verification of the next scheduled sync time in the NamespaceMapping status

This test proves that the controller is operating in scheduled mode and not continuous mode by verifying that new resources are not synced immediately but only during scheduled sync operations.

## How to Run

```bash
# Run this test case only
./test/run-tests.sh --test 17

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results

- All resources should be properly synchronized from source to target cluster
- New resources should NOT be synced immediately
- New resources should be synced during the next scheduled run
- The NamespaceMapping should show the next scheduled sync time
- The status should show successful synchronization statistics