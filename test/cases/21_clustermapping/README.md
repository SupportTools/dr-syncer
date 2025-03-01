# ClusterMapping Test

This test case verifies the functionality of the ClusterMapping CRD and direct CSI path access for PVC replication.

## What This Test Does

1. Uses the global ClusterMapping resource (`nyc3-to-sfo3` in the `dr-syncer` namespace) that establishes SSH connectivity between agents on source and target clusters
2. Creates test PVCs on both source and target clusters
3. Creates a NamespaceMapping resource that references the global ClusterMapping
4. Triggers the namespace mapping to sync the PVC data from source to target
5. Verifies that the data was successfully replicated

## Key Features Tested

- ClusterMapping connectivity verification
- Direct CSI path access for PVC replication
- PVC data synchronization using the agent pods
- Integration with the global ClusterMapping resource

## Implementation Details

The test uses the global ClusterMapping resource defined in `test/remote-clusters.yaml`, which establishes SSH connectivity between the agent pods running on the source and target clusters. This eliminates the need to create temporary pods for each namespace mapping operation, improving efficiency and reducing resource usage.

The namespace mapping process uses direct CSI paths on the root filesystem (e.g., `/var/lib/kubelet/pods/<pod-uid>/volumes/kubernetes.io~csi/<pv-name>/mount`) to access the PVC data. This approach provides better performance and reliability compared to mounting PVCs in temporary pods.

If a PVC is not already mounted by a pod, the system will create a placeholder pod to mount the PVC, perform the replication, and then clean up the placeholder pod.
