# Manual Mode Test

This test case verifies the functionality of the Manual replication mode with ClusterMapping reference.

## What This Test Does

1. Uses a ClusterMapping resource (`nyc3-to-sfo3` in the `dr-syncer` namespace) to establish connectivity between source and target clusters
2. Creates a Replication resource in Manual mode
3. Configures the replication to sync all resource types from the source namespace to the destination namespace
4. Verifies that resources are only synced when manually triggered

## Key Features Tested

- Manual replication mode
- ClusterMapping reference for cluster connectivity
- Resource synchronization across clusters
- Immutable resource handling with Recreate strategy
- PVC storage class and access mode mapping
- Volume attribute preservation

## Implementation Details

The test uses a ClusterMapping resource instead of directly specifying source and destination clusters. This approach provides better reusability and centralized management of cluster connectivity information.

When using ClusterMappingRef:
- The source cluster is determined by the `sourceCluster` field in the referenced ClusterMapping
- The destination cluster is determined by the `targetCluster` field in the referenced ClusterMapping

This test demonstrates how to migrate from the legacy direct cluster specification to the new ClusterMapping reference approach.
