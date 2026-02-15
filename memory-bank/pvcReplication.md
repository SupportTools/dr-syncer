# PVC Data Replication Design

*Last updated: 2025-12-30*

## Overview

DR-Syncer supports two PVC data replication paths:

1. **Rsync Path** (this document): Direct cluster-to-cluster data transfer via SSH tunnels and rsync pods.
2. **Backup Path** (Kopia): S3-mediated backup/restore with warm standby PVCs. See [docs/DESIGN.md](../docs/DESIGN.md) for full documentation.

The path is selected by `BackupConfig.Enabled` in the NamespaceMapping's `pvcConfig.dataSyncConfig` (routing logic in `pkg/controllers/syncer/pvc_handler.go`).

This document covers the **rsync path**. The rsync path enables synchronization of persistent volume data between source and destination clusters using rsync over SSH.

## Architecture

### Components

```
Source Cluster                          Destination Cluster
┌─────────────────────────┐            ┌─────────────────────────┐
│                         │            │                         │
│  ┌─────────────────┐    │            │    ┌─────────────────┐  │
│  │ Source PVC      │    │            │    │ Dest PVC        │  │
│  │ (mounted by app)│    │            │    │ (mounted by     │  │
│  └────────┬────────┘    │            │    │  rsync pod)     │  │
│           │             │            │    └────────┬────────┘  │
│  ┌────────▼────────┐    │   rsync    │    ┌────────▼────────┐  │
│  │ Agent DaemonSet │◄───┼────SSH─────┼───►│ Rsync Pod       │  │
│  │ (SSH server)    │    │   over     │    │ (rsync client)  │  │
│  └─────────────────┘    │   port     │    └─────────────────┘  │
│                         │   2222     │                         │
└─────────────────────────┘            └─────────────────────────┘

                    Controller Cluster
                    ┌─────────────────────────┐
                    │  DR-Syncer Controller   │
                    │  (orchestrates workflow)│
                    └─────────────────────────┘
```

### Workflow (13 Steps)

| Step | Action | Duration | Files |
|------|--------|----------|-------|
| 0 | Acquire PVC lock | <1s | `rsync_helpers.go` |
| 1 | Deploy rsync pod in destination | 1-5 min | `rsyncpod/deployment.go` |
| 2-3 | Generate SSH keys | ~15s | `rsync_workflow.go` |
| 4 | Check source PVC mounted | <1s | `rsync_helpers.go` |
| 5 | Find source node | <1s | `rsync_helpers.go` |
| 6 | Find agent pod | <1s | `rsync_helpers.go` |
| 7 | Find mount path | 15-45s | `rsync_helpers.go` |
| 8 | Push public key to agent | <1s | `rsync_helpers.go` |
| 9 | Test SSH connectivity | <5s | `rsync_workflow.go` |
| 10 | Execute rsync | varies | `perform_rsync.go` |
| 11 | Update annotations | <1s | `rsync_helpers.go` |
| 12 | Cleanup rsync pod | <10s | `rsync_workflow.go` |
| 13 | Release PVC lock | <1s | `rsync_helpers.go` |

## Configuration

### CRD Fields (NamespaceMapping)

```yaml
spec:
  pvcConfig:
    syncData: true                    # Enable data sync
    syncPersistentVolumes: false      # Sync PV objects (for shared storage)
    preserveVolumeAttributes: false   # Keep volume attributes

    storageClassMappings:
    - from: "gp2"
      to: "ebs-standard"

    accessModeMappings:
    - from: "ReadWriteOnce"
      to: "ReadWriteMany"

    dataSyncConfig:
      concurrentSyncs: 2              # Max concurrent PVC syncs
      bandwidthLimit: 10240           # KB/s (10 MB/s)
      timeout: "30m"                  # Sync timeout
      excludePaths:
        - ".git"
        - "node_modules"
      rsyncOptions:
        - "--checksum"
        - "--sparse"
```

### RemoteCluster PVC Sync Config

```yaml
spec:
  pvcSync:
    enabled: true
    image:
      repository: supporttools/dr-syncer-agent
      tag: latest
    ssh:
      port: 2222
    concurrency: 5                    # Max concurrent syncs per cluster
```

## Known Bottlenecks

### 1. SSH Key Generation (Steps 2-3, 8-9)
- **Problem**: 4096-bit RSA keys generated every sync
- **Impact**: ~30 seconds overhead per sync
- **Solution**: Pre-generate keys per RemoteCluster, cache in Secrets

### 2. Mount Path Discovery (Step 7)
- **Problem**: Cascading fallback: df → mount → find
- **Impact**: Up to 45 seconds for discovery
- **Solution**: Cache mount paths in PVC annotations

### 3. Rsync Pod Startup (Step 1)
- **Problem**: New Deployment created per sync
- **Impact**: 1-5 minutes for pod scheduling and startup
- **Solution**: Persistent rsync pod pool (StatefulSet)

### 4. Retry Logic
- **Problem**: Fixed 2 retries with 10s backoff
- **Impact**: Poor recovery from transient failures
- **Solution**: Configurable exponential backoff with jitter

### 5. Observability
- **Problem**: Status in PVC annotations, no events
- **Impact**: Hard to monitor sync progress
- **Solution**: Kubernetes events, real-time progress, PVCSync CRD

## Implementation Status

All planned improvements have been implemented as of December 2025.

### Phase 1: Quick Wins ✅ COMPLETED
| Improvement | Impact | Implementation |
|-------------|--------|----------------|
| Mount path caching | -15-45s | `rsync_helpers.go` - cached in PVC annotations |
| Enhanced retry logic | Reliability | `perform_rsync.go` - exponential backoff with jitter |
| Kubernetes events | Observability | `pvc_sync.go` - SyncStarted/Completed/Failed events |

### Phase 2: Core Improvements ✅ COMPLETED
| Improvement | Impact | Implementation |
|-------------|--------|----------------|
| SSH key caching | -30s | Keys cached in Secrets per RemoteCluster |
| Checksum verification | Data integrity | `perform_rsync.go` - configurable verification modes (none/sample/full) |
| Real-time progress | Monitoring | `sync_status.go` - `ParseRsyncOutput()` extracts actual transfer stats |

### Phase 3: Advanced ✅ COMPLETED
| Improvement | Impact | Implementation |
|-------------|--------|----------------|
| Global concurrency control | Resource mgmt | `global_concurrency.go` - configurable limit (1-256, default: 4) |
| Lease-based locking | Race prevention | `pvc_lock.go` - Kubernetes Lease API for distributed locks |
| PVCSyncOperation CRD | kubectl visibility | `api/v1alpha1/pvcsync_types.go` - full sync status tracking |
| Rsync DaemonSet pool | -1-5 min | `rsyncpod/` - pre-deployed pods eliminate startup overhead |

### Phase 4: Architectural ✅ COMPLETED
| Improvement | Impact | Implementation |
|-------------|--------|----------------|
| CSI snapshot integration | Consistency | `snapshot_manager.go` - point-in-time snapshots with fallback |
| Parallel rsync streams | 2-4x throughput | `parallel_rsync.go` - 1-8 concurrent streams per PVC |

### Phase 5: Future Improvements (Planned)
| Improvement | Impact | Complexity |
|-------------|--------|------------|
| Incremental snapshots | Reduced transfer time | High |
| Cross-region optimization | WAN performance | Medium |
| Compression tuning | Bandwidth efficiency | Low |
| PVC priority scheduling | Critical data first | Medium |

## Implementation Patterns

### Rsync Command
```bash
rsync -avz --progress --delete \
  --rsh="ssh -o StrictHostKeyChecking=no \
             -o UserKnownHostsFile=/dev/null \
             -i /root/.ssh/id_rsa \
             -p 2222" \
  root@<node-ip>:<mount-path>/ /data/
```

### PVC Lock Annotation
```json
{
  "dr-syncer.io/lock-owner": "controller-pod-name",
  "dr-syncer.io/lock-timestamp": "2025-12-29T00:00:00Z"
}
```

### Sync Status Annotation
```json
{
  "phase": "Completed",
  "startTime": "2025-12-29T00:00:00Z",
  "completionTime": "2025-12-29T00:05:00Z",
  "bytesTransferred": 1073741824,
  "filesTransferred": 1000,
  "progress": 100,
  "error": ""
}
```

## Testing

### E2E Test Cases
- `test/cases/19_pvc-replication/` - PVC replication tests
- Verifies: sync initiation, data transfer, cleanup

### Manual Testing
```bash
# Watch sync progress
kubectl get pvc <name> -o jsonpath='{.metadata.annotations.dr-syncer\.io/sync-status}' | jq

# Check agent pod logs
kubectl logs -n dr-syncer -l app.kubernetes.io/name=dr-syncer-agent

# Check rsync pod logs
kubectl logs -n <namespace> -l app.kubernetes.io/name=dr-syncer-rsync
```

## Security Considerations

1. **SSH Keys**: 4096-bit RSA, stored in Kubernetes Secrets
2. **Network**: SSH over port 2222, configurable
3. **RBAC**: Agent requires PVC/Pod read access
4. **Host Key Checking**: Disabled for automation (StrictHostKeyChecking=no)
