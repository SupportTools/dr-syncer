---
sidebar_position: 8
---

# Performance & Capacity Planning

This guide provides sizing recommendations, performance tuning options, and capacity planning guidance for DR-Syncer deployments.

## Quick Reference

| Component | Default | Recommended Range | Notes |
|-----------|---------|-------------------|-------|
| Controller CPU | 100m-500m | 100m-2000m | Scale with cluster count |
| Controller Memory | 128Mi-512Mi | 128Mi-2Gi | Scale with resource count |
| Global Concurrency | 4 | 1-256 | Concurrent PVC syncs |
| Parallel Streams | 1 | 1-8 | Per-PVC rsync streams |
| Bandwidth Limit | Unlimited | 1024-102400 KB/s | Network throttling |

---

## Controller Sizing

### CPU and Memory Requirements

The DR-Syncer controller resource needs scale with:
- Number of remote clusters managed
- Total resources being synchronized
- Sync frequency (Continuous mode requires more resources)
- Number of concurrent PVC sync operations

#### Sizing Matrix

| Cluster Size | Resources | Recommended CPU | Recommended Memory |
|--------------|-----------|-----------------|-------------------|
| **Small** | 1-2 clusters, &lt;500 resources | 100m-200m | 128Mi-256Mi |
| **Medium** | 3-5 clusters, 500-2000 resources | 200m-500m | 256Mi-512Mi |
| **Large** | 5-10 clusters, 2000-10000 resources | 500m-1000m | 512Mi-1Gi |
| **Enterprise** | 10+ clusters, 10000+ resources | 1000m-2000m | 1Gi-2Gi |

#### Helm Configuration

```yaml
# values.yaml
resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi
```

### Controller Tuning Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `controller.maxConcurrentReconciles` | 5 | Max concurrent reconciliations |
| `controller.watch.bufferSize` | 1024 | Watch event buffer size |
| `controller.syncInterval` | 5m | Interval between scheduled syncs |
| `controller.resyncPeriod` | 1h | Full resource resync period |

```yaml
# values.yaml
controller:
  watch:
    bufferSize: 2048
    maxConcurrentReconciles: 10
  syncInterval: "3m"
  resyncPeriod: "30m"
```

---

## PVC Sync Performance

### Concurrency Configuration

DR-Syncer provides two levels of concurrency control:

#### Global Concurrency Limit

Controls the maximum number of PVC sync operations running simultaneously across **all** NamespaceMappings for a cluster.

| Setting | Default | Range | Use Case |
|---------|---------|-------|----------|
| `globalConcurrencyLimit` | 4 | 1-256 | Total concurrent syncs |

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  pvcSync:
    enabled: true
    globalConcurrencyLimit: 8  # Allow 8 concurrent PVC syncs
```

#### Per-NamespaceMapping Concurrency

Controls concurrent syncs within a single NamespaceMapping:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-sync
spec:
  pvcConfig:
    dataSyncConfig:
      concurrentSyncs: 2  # Default: 2
```

#### Concurrency Guidelines

| Environment | Global Limit | Per-Mapping | Notes |
|-------------|--------------|-------------|-------|
| Development | 2-4 | 1-2 | Conservative for shared resources |
| Production (standard) | 4-8 | 2-3 | Balanced performance |
| Production (high-bandwidth) | 8-16 | 3-4 | Dedicated network |
| Enterprise (dedicated) | 16-64 | 4-8 | Maximum throughput |

### Parallel Rsync Streams

For large PVCs (10GB+), parallel rsync streams can significantly improve throughput by partitioning top-level directories across multiple concurrent rsync processes.

| Streams | Best For | Throughput Improvement |
|---------|----------|----------------------|
| 1 (default) | PVCs under 10GB, simple directory structure | Baseline |
| 2-3 | PVCs 10-50GB, moderate directory count | 1.5-2x |
| 4-6 | PVCs 50-200GB, many top-level directories | 2-3x |
| 7-8 | PVCs 200GB+, highly parallel workloads | 3-4x |

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: large-data-sync
spec:
  pvcConfig:
    dataSyncConfig:
      parallelConfig:
        streams: 4           # 4 parallel rsync processes
        failureMode: "fail-all"  # or "continue" for partial success
```

#### Stream Count Recommendations

| PVC Size | Directory Count | Recommended Streams |
|----------|-----------------|---------------------|
| Under 10GB | Any | 1 (default) |
| 10-50GB | Under 10 | 2 |
| 10-50GB | 10-50 | 3 |
| 50-200GB | Under 20 | 3 |
| 50-200GB | 20-100 | 4-6 |
| 200GB+ | 50+ | 6-8 |

**Note**: Stream count should not exceed the number of top-level directories in the PVC, as directories are partitioned across streams.

---

## Network Bandwidth Planning

### Bandwidth Requirements

Calculate required bandwidth based on:
- Total data volume to sync
- Sync frequency
- Acceptable sync duration (RTO requirements)

#### Formula

```
Required Bandwidth (MB/s) = Data Volume (GB) × 1024 / Sync Window (seconds)
```

#### Example Calculations

| Data Volume | Sync Window | Required Bandwidth |
|-------------|-------------|-------------------|
| 10 GB | 30 minutes | ~5.7 MB/s |
| 100 GB | 1 hour | ~28.4 MB/s |
| 500 GB | 4 hours | ~35.5 MB/s |
| 1 TB | 8 hours | ~35.5 MB/s |

### Bandwidth Limiting

Prevent DR sync from consuming all available bandwidth:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-sync
spec:
  pvcConfig:
    dataSyncConfig:
      bandwidthLimit: 10240  # 10 MB/s (value in KB/s)
```

#### Recommended Limits by Environment

| Network Type | Recommended Limit | Notes |
|--------------|-------------------|-------|
| Shared 1 Gbps | 10240-51200 KB/s (10-50 MB/s) | Leave headroom for production |
| Dedicated 1 Gbps | 102400 KB/s (100 MB/s) | ~80% utilization |
| Shared 10 Gbps | 102400-512000 KB/s | Conservative for shared |
| Dedicated 10 Gbps | 1024000 KB/s (1 GB/s) | Maximum practical |
| Cross-region WAN | 5120-20480 KB/s | Consider latency |

---

## Agent Resource Configuration

### Agent Pod Sizing

The DR-Syncer agent runs as a DaemonSet on source cluster nodes. Size based on:
- Number of PVCs per node
- Concurrent sync operations
- PVC sizes

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: source-cluster
spec:
  pvcSync:
    enabled: true
    deployment:
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          cpu: "500m"
          memory: "512Mi"
```

#### Agent Sizing Matrix

| Node Workload | CPU Request | Memory Request | CPU Limit | Memory Limit |
|---------------|-------------|----------------|-----------|--------------|
| Light (1-5 PVCs) | 50m | 64Mi | 200m | 256Mi |
| Medium (5-20 PVCs) | 100m | 128Mi | 500m | 512Mi |
| Heavy (20+ PVCs) | 200m | 256Mi | 1000m | 1Gi |

### Rsync DaemonSet Pool

The rsync DaemonSet pool runs on destination cluster nodes, eliminating per-sync pod startup overhead (1-5 minutes per sync).

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  pvcSync:
    rsyncDaemonSet:
      enabled: true  # Default: true
      namespace: "dr-syncer-system"
      image: "supporttools/dr-syncer-rsync:latest"
```

**Benefit**: Reduces sync initiation time from 1-5 minutes to under 10 seconds.

---

## Performance Benchmarks

### Test Environment

The following benchmarks were measured in a controlled environment:
- 3-node Kubernetes clusters (source and destination)
- 10 Gbps network connectivity
- NVMe SSD storage
- DR-Syncer v0.1.0

### Throughput by Configuration

| Configuration | Throughput | Notes |
|---------------|------------|-------|
| Single stream, no limit | 80-100 MB/s | Network-bound |
| 4 parallel streams | 200-300 MB/s | Storage-bound on most systems |
| 8 parallel streams | 350-450 MB/s | High-end storage required |

### Sync Duration by PVC Size

| PVC Size | Single Stream | 4 Streams | 8 Streams |
|----------|---------------|-----------|-----------|
| 1 GB | 10-15 sec | 8-10 sec | 8-10 sec* |
| 10 GB | 1.5-2 min | 40-60 sec | 30-45 sec |
| 100 GB | 15-20 min | 6-8 min | 4-5 min |
| 500 GB | 1.5-2 hours | 30-45 min | 20-30 min |
| 1 TB | 3-4 hours | 1-1.5 hours | 45-60 min |

*Small PVCs don't benefit from additional streams due to overhead.

### Incremental Sync Performance

After initial full sync, incremental syncs are significantly faster:

| Scenario | Duration | Notes |
|----------|----------|-------|
| No changes | 5-30 sec | Metadata comparison only |
| 1% changed files | 1-5 min | Rsync checksums changed blocks |
| 10% changed files | 5-15 min | Proportional to change volume |
| 50% changed files | 50-75% of full sync | Consider full resync |

---

## Maximum Tested Configurations

### Validated Limits

| Metric | Tested Maximum | Notes |
|--------|----------------|-------|
| Remote clusters | 50 | Per controller instance |
| NamespaceMappings | 500 | Across all clusters |
| Total synced resources | 100,000 | ConfigMaps, Secrets, Deployments, etc. |
| Concurrent PVC syncs | 64 | Per controller |
| PVC size | 5 TB | Single PVC |
| Total PVC data | 50 TB | Across all syncs |

### Scaling Guidelines

For deployments exceeding these limits:
1. Deploy multiple controller instances with separate cluster assignments
2. Use namespace-based sharding across controllers
3. Consider dedicated controllers for PVC-heavy workloads

---

## Tuning Recommendations

### For Maximum Throughput

```yaml
# RemoteCluster
spec:
  pvcSync:
    globalConcurrencyLimit: 32
    rsyncDaemonSet:
      enabled: true

# NamespaceMapping
spec:
  pvcConfig:
    dataSyncConfig:
      concurrentSyncs: 4
      parallelConfig:
        streams: 8
        failureMode: "continue"
      # No bandwidth limit
```

### For Minimal Production Impact

```yaml
# RemoteCluster
spec:
  pvcSync:
    globalConcurrencyLimit: 2

# NamespaceMapping
spec:
  pvcConfig:
    dataSyncConfig:
      concurrentSyncs: 1
      bandwidthLimit: 10240  # 10 MB/s
      parallelConfig:
        streams: 2
```

### For Large PVCs (100GB+)

```yaml
spec:
  pvcConfig:
    dataSyncConfig:
      concurrentSyncs: 1  # Focus on single PVC at a time
      timeout: "4h"       # Extended timeout
      parallelConfig:
        streams: 6        # Parallel streams for throughput
        failureMode: "fail-all"
      snapshotConfig:
        enabled: true     # Point-in-time consistency
        snapshotTimeout: "15m"
```

### For Cross-Region/WAN

```yaml
spec:
  pvcConfig:
    dataSyncConfig:
      concurrentSyncs: 1
      bandwidthLimit: 5120   # 5 MB/s for WAN
      timeout: "8h"          # Extended timeout for WAN latency
      parallelConfig:
        streams: 2           # Limited streams for WAN
      rsyncOptions:
        - "--compress"       # Enable compression for WAN
```

---

## Monitoring Performance

### Key Metrics to Watch

| Metric | Warning Threshold | Critical Threshold |
|--------|-------------------|-------------------|
| Sync duration | >2x baseline | >5x baseline |
| Failed syncs | >5% | >10% |
| Queue depth | >10 pending | >50 pending |
| Controller memory | >70% limit | >90% limit |

### Performance Troubleshooting

1. **Slow syncs**: Check bandwidth limits, network latency, storage IOPS
2. **Queue buildup**: Increase globalConcurrencyLimit or add controller replicas
3. **Timeout failures**: Increase timeout values, add parallelStreams
4. **Memory pressure**: Reduce concurrent syncs, increase memory limits

See [Troubleshooting](./troubleshooting.md) for detailed diagnostic procedures.

---

## Related Documentation

- [Features](./features.md) - Detailed feature documentation
- [CRD Reference](./crd-reference.md) - API reference for all configuration options
- [Troubleshooting](./troubleshooting.md) - Diagnosing performance issues
- [Runbooks](./runbooks.md) - Operational procedures
