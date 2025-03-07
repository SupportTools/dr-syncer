# Test Case: Multi-Cluster Circular Replication

This test validates DR Syncer's ability to handle complex multi-directional replication patterns across multiple clusters.

## Test Pattern

The test implements a circular replication pattern with multiple namespaces distributed across three clusters:

```
Namespace test-case-22-a: dr-syncer-nyc3 → dr-syncer-sfo3
Namespace test-case-22-b: dr-syncer-sfo3 → dr-syncer-tor1
Namespace test-case-22-c: dr-syncer-tor1 → dr-syncer-nyc3
Namespace test-case-22-d: dr-syncer-nyc3 → dr-syncer-sfo3
Namespace test-case-22-e: dr-syncer-sfo3 → dr-syncer-nyc3
```

## Test Scenarios

1. **Multi-Directional Replication**: Resources are replicated correctly in each path
2. **Multiple Namespaces Same Source/Target**: Multiple namespaces can be replicated between the same clusters
3. **Bidirectional Replication**: Resources can be replicated bidirectionally between clusters
4. **Resource Identification**: Resources are properly identified in their source/target clusters
5. **Simultaneous Replication**: The system can handle multiple replication paths simultaneously

## Test Resources

The test uses various Kubernetes resources to validate replication:
- ConfigMaps
- Secrets
- Deployments
- Services

Each resource is uniquely identified to verify proper replication paths.

## Prerequisites

- Three configured clusters:
  - NYC3 (PROD_KUBECONFIG - Production cluster)
  - SFO3 (DR_KUBECONFIG - DR cluster)
  - TOR1 (EDGE_KUBECONFIG - Edge cluster)
- ClusterMapping resources already created and connected:
  - nyc3-to-sfo3
  - sfo3-to-tor1
  - tor1-to-nyc3
  - sfo3-to-nyc3
- Agent pods running on all clusters
