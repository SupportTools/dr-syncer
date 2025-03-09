# Continuous Mode Test

This test case verifies the functionality of the Continuous replication mode with ClusterMapping reference. In Continuous mode, resources are synchronized in real-time based on changes detected by the Kubernetes watch API.

## What This Test Does

1. Deploys a NamespaceMapping resource configured with `replicationMode: Continuous` and a background sync interval of 30 seconds
2. Verifies that resources are synced as part of the initial deployment
3. Tests the real-time nature of continuous mode by:
   - Updating an existing ConfigMap in the source namespace and verifying the change is quickly propagated
   - Creating a new ConfigMap and verifying it appears in the target namespace without manual intervention
4. Verifies that the controller is properly watching for events by checking the `lastWatchEvent` timestamp

## Key Features Tested

- Continuous replication mode with real-time synchronization
- Watch API integration for detecting changes
- Background sync interval configuration 
- Real-time propagation of updates to existing resources
- Real-time propagation of newly created resources
- ClusterMapping reference for cluster connectivity
- Resource synchronization across clusters
- Correct handling of PVC storage class mapping
- Verification of watch event timestamps

## Implementation Notes

The test includes specific functionality for testing continuous mode:

1. A function that updates an existing ConfigMap and monitors how quickly the change propagates to the DR cluster
2. A function that creates a new ConfigMap and checks how quickly it's synced, demonstrating that continuous mode doesn't require manual triggers
3. Verification of the `lastWatchEvent` timestamp, which is specific to continuous mode
4. Multiple tests to verify both update and creation events are properly handled

This test specifically demonstrates the difference between continuous mode and other modes (manual and scheduled) by showing that changes are synchronized in real-time without any manual intervention or waiting for a scheduled sync time.

## How to Run

```bash
# Run this test case only
./test/run-tests.sh --test 18

# Run as part of all tests
./test/run-tests.sh
```

## Expected Results

- All resources should be properly synchronized from source to target cluster
- Changes to existing resources should be quickly propagated to the DR cluster
- New resources should be quickly propagated to the DR cluster
- The NamespaceMapping status should show watch event timestamps
- The phase should be "Running" (not "Completed" as in other modes)
- The status should show successful synchronization statistics