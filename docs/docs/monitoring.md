---
sidebar_position: 9
---

# Monitoring & Observability

This guide covers the monitoring and observability features of DR-Syncer, including Prometheus metrics, Kubernetes events, alerting recommendations, and troubleshooting patterns.

## Overview

DR-Syncer provides comprehensive observability through three channels:

| Channel | Purpose | Access Method |
|---------|---------|---------------|
| **Prometheus Metrics** | Quantitative performance data | `/metrics` endpoint on port 8080 |
| **Kubernetes Events** | Workflow progress and issues | `kubectl get events` |
| **Controller Logs** | Detailed debugging information | `kubectl logs` |

---

## Prometheus Metrics

DR-Syncer exposes custom Prometheus metrics for monitoring PVC synchronization operations. The metrics endpoint is available at `:8080/metrics` on the controller pod.

### Metrics Reference

#### Transfer Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dr_syncer_pvc_sync_bytes_transferred_total` | Counter | namespace, pvc_name, destination_namespace | Total bytes transferred during PVC sync operations |
| `dr_syncer_pvc_sync_files_transferred_total` | Counter | namespace, pvc_name, destination_namespace | Total files transferred during PVC sync operations |
| `dr_syncer_pvc_sync_speed_bytes_per_second` | Gauge | namespace, pvc_name, destination_namespace | Current transfer speed in bytes per second |

#### Progress Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dr_syncer_pvc_sync_progress_percent` | Gauge | namespace, pvc_name, destination_namespace | Current sync progress percentage (0-100) |
| `dr_syncer_pvc_sync_duration_seconds` | Histogram | namespace, pvc_name, destination_namespace, status | Duration of PVC sync operations |
| `dr_syncer_pvc_sync_operations_total` | Counter | namespace, pvc_name, destination_namespace, status | Total number of sync operations by status |

#### Queue Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dr_syncer_pvc_sync_queue_depth` | Gauge | - | Number of PVC syncs waiting in queue |
| `dr_syncer_pvc_sync_concurrent_count` | Gauge | - | Number of PVC syncs currently running |
| `dr_syncer_pvc_sync_queue_wait_seconds` | Histogram | - | Time spent waiting in queue before sync starts |

### Histogram Buckets

The `dr_syncer_pvc_sync_duration_seconds` histogram uses exponential buckets optimized for sync operations:

```
Buckets: 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s, 512s, 1024s, 2048s, 4096s, 8192s, 16384s
Range: 1 second to ~4.5 hours
```

### Metrics Endpoint Configuration

The metrics endpoint is configured in the Helm values:

```yaml
# values.yaml
controller:
  metricsAddr: ":8080"
```

### Example Prometheus Queries

#### Sync Throughput

```promql
# Average sync speed across all PVCs (MB/s)
avg(dr_syncer_pvc_sync_speed_bytes_per_second) / 1024 / 1024

# Total data transferred in last hour (GB)
increase(dr_syncer_pvc_sync_bytes_transferred_total[1h]) / 1024 / 1024 / 1024

# Files synced per minute
rate(dr_syncer_pvc_sync_files_transferred_total[5m]) * 60
```

#### Sync Performance

```promql
# 95th percentile sync duration
histogram_quantile(0.95, rate(dr_syncer_pvc_sync_duration_seconds_bucket[1h]))

# Average sync duration by PVC
avg by (pvc_name) (rate(dr_syncer_pvc_sync_duration_seconds_sum[1h]) / rate(dr_syncer_pvc_sync_duration_seconds_count[1h]))

# Sync success rate
sum(rate(dr_syncer_pvc_sync_operations_total{status="success"}[1h])) / sum(rate(dr_syncer_pvc_sync_operations_total[1h])) * 100
```

#### Queue Health

```promql
# Current queue depth
dr_syncer_pvc_sync_queue_depth

# Queue wait time 95th percentile
histogram_quantile(0.95, rate(dr_syncer_pvc_sync_queue_wait_seconds_bucket[1h]))

# Concurrent syncs vs limit
dr_syncer_pvc_sync_concurrent_count
```

---

## Kubernetes Events

DR-Syncer emits Kubernetes events on source PVCs during synchronization workflows. Events provide real-time visibility into sync progress and are the fastest way to monitor operations.

### Event Reference

| Event Reason | Type | Description |
|--------------|------|-------------|
| `SyncStarted` | Normal | Sync workflow has begun for this PVC |
| `LockAcquired` | Normal | Distributed lock acquired for this PVC |
| `RsyncPodDeployed` | Normal | Rsync pod deployed in destination cluster |
| `SSHConnected` | Normal | SSH connectivity established to source agent |
| `SyncCompleted` | Normal | Sync completed successfully |
| `SyncSkipped` | Normal | Sync skipped (PVC locked, not mounted, etc.) |
| `LockReleased` | Normal | Distributed lock released |
| `SyncFailed` | Warning | Sync operation failed |

### Viewing Events

```bash
# View all DR-Syncer events in a namespace
kubectl get events -n <namespace> --field-selector reason=SyncStarted,reason=SyncCompleted,reason=SyncFailed

# Watch events in real-time
kubectl get events -n <namespace> -w | grep -E 'Sync|Lock|SSH|Rsync'

# View events for a specific PVC
kubectl describe pvc <pvc-name> -n <namespace> | grep -A 20 Events

# View recent events across all namespaces
kubectl get events -A --sort-by='.lastTimestamp' | grep dr-syncer
```

### Event Sequence Examples

#### Successful Sync Workflow

```
LAST SEEN   TYPE     REASON            OBJECT                          MESSAGE
2m          Normal   SyncStarted       persistentvolumeclaim/data-pvc  Starting PVC sync to dr-cluster/data-pvc
2m          Normal   LockAcquired      persistentvolumeclaim/data-pvc  Acquired sync lock (lease dr-syncer-pvc-data-pvc)
90s         Normal   RsyncPodDeployed  persistentvolumeclaim/data-pvc  Rsync pod deployed in destination cluster
60s         Normal   SSHConnected      persistentvolumeclaim/data-pvc  SSH connection established to agent on node-1
10s         Normal   LockReleased      persistentvolumeclaim/data-pvc  Released sync lock
10s         Normal   SyncCompleted     persistentvolumeclaim/data-pvc  Sync completed: 1.5GB transferred in 1m50s
```

#### Failed Sync

```
LAST SEEN   TYPE      REASON      OBJECT                          MESSAGE
30s         Warning   SyncFailed  persistentvolumeclaim/data-pvc  Sync failed: rsync error: connection refused
```

#### Skipped Sync

```
LAST SEEN   TYPE     REASON       OBJECT                          MESSAGE
15s         Normal   SyncSkipped  persistentvolumeclaim/data-pvc  PVC locked by another sync operation
```

---

## Recommended Alert Rules

### Critical Alerts

```yaml
groups:
  - name: dr-syncer-critical
    rules:
      # Sync failure rate exceeds threshold
      - alert: DRSyncerHighFailureRate
        expr: |
          sum(rate(dr_syncer_pvc_sync_operations_total{status="failed"}[15m]))
          / sum(rate(dr_syncer_pvc_sync_operations_total[15m])) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "DR-Syncer sync failure rate exceeds 10%"
          description: "{{ $value | humanizePercentage }} of PVC syncs are failing"

      # No syncs completing for extended period
      - alert: DRSyncerNoSyncsCompleting
        expr: |
          sum(increase(dr_syncer_pvc_sync_operations_total{status="success"}[30m])) == 0
          and sum(dr_syncer_pvc_sync_queue_depth) > 0
        for: 15m
        labels:
          severity: critical
        annotations:
          summary: "DR-Syncer syncs are stalled"
          description: "No syncs have completed in 30 minutes while queue has pending items"

      # Controller pod not running
      - alert: DRSyncerControllerDown
        expr: |
          absent(up{job="dr-syncer-controller"}) == 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "DR-Syncer controller is down"
          description: "The DR-Syncer controller pod is not responding"
```

### Warning Alerts

```yaml
groups:
  - name: dr-syncer-warning
    rules:
      # High queue depth
      - alert: DRSyncerQueueBacklog
        expr: dr_syncer_pvc_sync_queue_depth > 50
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "DR-Syncer sync queue backlog"
          description: "{{ $value }} PVCs waiting in sync queue"

      # Long queue wait times
      - alert: DRSyncerLongQueueWait
        expr: |
          histogram_quantile(0.95, rate(dr_syncer_pvc_sync_queue_wait_seconds_bucket[15m])) > 600
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "DR-Syncer queue wait time is high"
          description: "95th percentile queue wait is {{ $value | humanizeDuration }}"

      # Slow sync operations
      - alert: DRSyncerSlowSyncs
        expr: |
          histogram_quantile(0.95, rate(dr_syncer_pvc_sync_duration_seconds_bucket[1h])) > 3600
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "DR-Syncer sync operations are slow"
          description: "95th percentile sync duration is {{ $value | humanizeDuration }}"

      # Approaching concurrency limit
      - alert: DRSyncerConcurrencyNearLimit
        expr: dr_syncer_pvc_sync_concurrent_count > 0.9 * dr_syncer_global_concurrency_limit
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "DR-Syncer near concurrency limit"
          description: "{{ $value }} of {{ $labels.limit }} concurrent syncs running"
```

### Informational Alerts

```yaml
groups:
  - name: dr-syncer-info
    rules:
      # Large data transfer
      - alert: DRSyncerLargeTransfer
        expr: |
          increase(dr_syncer_pvc_sync_bytes_transferred_total[1h]) > 100 * 1024 * 1024 * 1024
        labels:
          severity: info
        annotations:
          summary: "DR-Syncer transferred over 100GB in last hour"
          description: "{{ $value | humanize1024 }}B transferred"

      # Sync completed after long duration
      - alert: DRSyncerLongSyncCompleted
        expr: |
          dr_syncer_pvc_sync_duration_seconds_bucket{le="7200", status="success"} == 0
          and dr_syncer_pvc_sync_duration_seconds_bucket{le="+Inf", status="success"} > 0
        labels:
          severity: info
        annotations:
          summary: "DR-Syncer completed sync after 2+ hours"
          description: "Long-running sync completed for {{ $labels.pvc_name }}"
```

---

## Grafana Dashboard

### Dashboard JSON Overview

Create a Grafana dashboard with the following panels:

#### Row 1: Overview

| Panel | Type | Query |
|-------|------|-------|
| **Sync Success Rate** | Stat | `sum(rate(dr_syncer_pvc_sync_operations_total{status="success"}[1h])) / sum(rate(dr_syncer_pvc_sync_operations_total[1h])) * 100` |
| **Active Syncs** | Stat | `dr_syncer_pvc_sync_concurrent_count` |
| **Queue Depth** | Stat | `dr_syncer_pvc_sync_queue_depth` |
| **Data Transferred (24h)** | Stat | `sum(increase(dr_syncer_pvc_sync_bytes_transferred_total[24h]))` |

#### Row 2: Transfer Performance

| Panel | Type | Query |
|-------|------|-------|
| **Transfer Speed** | Time Series | `avg(dr_syncer_pvc_sync_speed_bytes_per_second) / 1024 / 1024` |
| **Bytes Transferred** | Time Series | `rate(dr_syncer_pvc_sync_bytes_transferred_total[5m])` |
| **Files Transferred** | Time Series | `rate(dr_syncer_pvc_sync_files_transferred_total[5m])` |

#### Row 3: Sync Duration

| Panel | Type | Query |
|-------|------|-------|
| **Sync Duration Heatmap** | Heatmap | `rate(dr_syncer_pvc_sync_duration_seconds_bucket[5m])` |
| **Duration Percentiles** | Time Series | `histogram_quantile(0.50/0.90/0.95/0.99, rate(...))` |

#### Row 4: Queue Health

| Panel | Type | Query |
|-------|------|-------|
| **Queue Wait Time** | Time Series | `histogram_quantile(0.95, rate(dr_syncer_pvc_sync_queue_wait_seconds_bucket[5m]))` |
| **Concurrent vs Limit** | Gauge | `dr_syncer_pvc_sync_concurrent_count` with threshold |

#### Row 5: Operations by Status

| Panel | Type | Query |
|-------|------|-------|
| **Operations by Status** | Pie Chart | `sum by (status) (increase(dr_syncer_pvc_sync_operations_total[24h]))` |
| **Failed Syncs** | Table | `topk(10, increase(dr_syncer_pvc_sync_operations_total{status="failed"}[24h]))` |

### Importing the Dashboard

1. Navigate to Grafana → Dashboards → Import
2. Upload JSON file or paste JSON content
3. Select Prometheus data source
4. Save dashboard

---

## Log Analysis Patterns

### Controller Log Structure

DR-Syncer uses structured JSON logging. Key fields:

| Field | Description |
|-------|-------------|
| `level` | Log level (info, error, debug) |
| `msg` | Log message |
| `controller` | Controller name (remotecluster, replication) |
| `namespace` | Kubernetes namespace |
| `name` | Resource name |
| `error` | Error message (if present) |

### Common Log Queries

#### Find Sync Errors

```bash
# Using kubectl
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | grep '"level":"error"'

# With jq for structured output
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | jq 'select(.level == "error")'

# Find specific PVC errors
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | jq 'select(.msg | contains("pvc-name"))'
```

#### Track Sync Progress

```bash
# Follow sync operations
kubectl logs -n dr-syncer -l app=dr-syncer -c controller -f | grep -E 'sync|rsync|transfer'

# Filter by namespace
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | jq 'select(.namespace == "production")'
```

#### Debug Connection Issues

```bash
# SSH connection problems
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | grep -i ssh

# Agent connectivity
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | grep -i 'agent\|connection'

# Cluster connectivity
kubectl logs -n dr-syncer -l app=dr-syncer -c controller | grep -i 'remote\|cluster'
```

### Log Level Configuration

Enable debug logging for troubleshooting:

```bash
# Update controller deployment
kubectl set env deployment/dr-syncer-controller -n dr-syncer LOG_LEVEL=debug

# Or via Helm
helm upgrade dr-syncer dr-syncer/dr-syncer \
  --namespace dr-syncer \
  --set controller.logLevel=debug
```

### Log Aggregation Integration

For centralized logging, DR-Syncer logs are compatible with:

| System | Configuration |
|--------|--------------|
| **Elasticsearch/Kibana** | JSON logs parsed automatically |
| **Loki/Grafana** | Use `json` parser in LogQL |
| **Splunk** | JSON sourcetype with automatic field extraction |
| **CloudWatch** | JSON format with Insights queries |

Example Loki LogQL:

```logql
{namespace="dr-syncer", app="dr-syncer"} | json | level="error"
```

---

## ServiceMonitor Configuration

If using Prometheus Operator, create a ServiceMonitor:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: dr-syncer
  namespace: dr-syncer
  labels:
    app: dr-syncer
spec:
  selector:
    matchLabels:
      app: dr-syncer
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
  namespaceSelector:
    matchNames:
      - dr-syncer
```

### Service Configuration

Ensure the controller Service exposes the metrics port:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: dr-syncer-controller-metrics
  namespace: dr-syncer
  labels:
    app: dr-syncer
spec:
  ports:
    - name: metrics
      port: 8080
      targetPort: 8080
      protocol: TCP
  selector:
    app: dr-syncer
```

---

## Health Endpoints

The controller exposes health endpoints:

| Endpoint | Port | Purpose |
|----------|------|---------|
| `/healthz` | 8081 | Liveness probe |
| `/readyz` | 8081 | Readiness probe |
| `/metrics` | 8080 | Prometheus metrics |

### Kubernetes Probe Configuration

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8081
  initialDelaySeconds: 15
  periodSeconds: 20

readinessProbe:
  httpGet:
    path: /readyz
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 10
```

---

## Troubleshooting with Metrics

### Diagnosing Slow Syncs

1. **Check queue metrics**: High `queue_depth` with low `concurrent_count` suggests bottleneck
2. **Check transfer speed**: Low `speed_bytes_per_second` indicates network or storage issues
3. **Check duration histogram**: Identify which percentile is problematic

### Diagnosing Failed Syncs

1. **Check events**: `kubectl get events --field-selector reason=SyncFailed`
2. **Check failure count**: Rising `operations_total{status="failed"}` counter
3. **Check controller logs**: Filter for error level messages

### Diagnosing Queue Buildup

1. **Check concurrent limit**: Compare `concurrent_count` vs configured limit
2. **Check wait time**: High `queue_wait_seconds` indicates limit too low
3. **Check sync duration**: Long syncs may be blocking queue

---

## Related Documentation

- [Troubleshooting](./troubleshooting.md) - Detailed diagnostic procedures
- [Performance](./performance.md) - Capacity planning and tuning
- [Features](./features.md) - PVC sync configuration options
