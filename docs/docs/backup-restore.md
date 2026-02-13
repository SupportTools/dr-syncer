# Snapshot-Based Backup/Restore (Kopia + S3)

## Overview

DR-Syncer supports two complementary PVC data synchronization methods:

1. **Rsync-based** (existing) — Direct file-level sync over SSH between clusters
2. **Snapshot-based** (new) — CSI snapshot → Kopia → S3 → restore to DR standby PVC

Users choose per-NamespaceMapping which method to use. Both methods are fully supported and can be used simultaneously across different NamespaceMappings.

## When to Use Snapshot-Based Sync

| Scenario | Recommended Method |
|----------|-------------------|
| Small PVCs (< 10GB), simple setup | Rsync |
| Large PVCs (> 50GB), frequent backups | Snapshot-based |
| Cross-region DR (clusters can't directly connect) | Snapshot-based (S3 bridges clusters) |
| Need point-in-time recovery from S3 | Snapshot-based |
| Instant failover requirement (< 1min RTO) | Snapshot-based (warm standby PVCs) |
| No S3 infrastructure available | Rsync |

## Architecture

```
SOURCE CLUSTER                     S3 (Kopia Repo)              DR CLUSTER
┌──────────┐  CSI snap  ┌────────┐                ┌────────┐  ┌──────────┐
│ Source   │──────────>│ Temp   │  Kopia backup   │ Kopia  │  │ Standby  │
│ PVC      │           │ PVC    │──────────────>  │ Repo   │──│ PVC      │
└──────────┘           └────────┘  (deduplicated) │ in S3  │  │ (warm)   │
                                                   └────────┘  └──────────┘
```

### Data Access Strategies

Not all storage classes support CSI snapshots. The backup workflow supports two strategies:

- **Snapshot** (preferred): CSI VolumeSnapshot → temp PVC → Kopia reads from temp PVC. Point-in-time consistent.
- **Live** (fallback): Kopia pod reads directly from PVC mount via hostPath. No snapshot overhead, but files may change during backup.

Configured via `snapshotMode`: `auto` (default), `snapshot-only`, or `live-only`.

### Incrementality

Kopia uses content-defined chunking (~4MB blocks) with deduplication:
- **Backup**: Only new/changed chunks are uploaded to S3
- **Restore**: Only changed files are overwritten on the standby PVC
- **Storage**: Full deduplication across all snapshots, typically 70-90% savings for incremental backups

## Custom Resource Definitions

### BackupRepository

Defines an S3-backed Kopia repository for storing deduplicated, encrypted backups.

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: BackupRepository
metadata:
  name: prod-backup-repo
  namespace: dr-syncer-system
spec:
  s3Config:
    endpoint: "s3.amazonaws.com"
    bucket: "dr-syncer-backups"
    region: "us-west-2"
    pathPrefix: "prod/"
    credentialsSecretRef:
      name: s3-credentials
      namespace: dr-syncer-system
  kopiaConfig:
    encryptionSecretRef:
      name: kopia-repo-password
      namespace: dr-syncer-system
    compressionAlgorithm: "zstd"
  maintenanceSchedule: "0 2 * * *"  # Daily at 2 AM
  retentionPolicy:
    keepLatest: 10
    keepDaily: 7
    keepWeekly: 4
    keepMonthly: 12
```

**Required Secrets:**

```yaml
# S3 credentials
apiVersion: v1
kind: Secret
metadata:
  name: s3-credentials
  namespace: dr-syncer-system
type: Opaque
data:
  accessKeyID: <base64>
  secretAccessKey: <base64>
---
# Kopia repository password (encrypts all data at rest)
apiVersion: v1
kind: Secret
metadata:
  name: kopia-repo-password
  namespace: dr-syncer-system
type: Opaque
data:
  password: <base64>
```

### VolumeBackupOperation

Tracks individual backup or restore operations (created automatically by the controller).

```bash
kubectl get vbo -n dr-syncer-system
NAME                              TYPE     PVC         PHASE      PROGRESS   DURATION
myapp-data-backup-20260213-1000   Backup   myapp-data  Completed  100%       5m30s
myapp-data-restore-20260213-1005  Restore  myapp-data  Completed  100%       4m15s
```

## Configuration

### Enabling Snapshot-Based Sync on a NamespaceMapping

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: prod-to-dr
  namespace: dr-syncer-system
spec:
  sourceCluster: prod-cluster
  destinationCluster: dr-cluster
  sourceNamespace: production
  destinationNamespace: production
  replicationMode: Scheduled
  schedule: "0 */6 * * *"  # Every 6 hours
  resourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
    - persistentvolumeclaims
  pvcConfig:
    syncData: true
    dataSyncConfig:
      backupConfig:
        enabled: true
        backupRepositoryRef:
          name: prod-backup-repo
          namespace: dr-syncer-system
        snapshotMode: auto  # auto | snapshot-only | live-only
        standbyPVCConfig:
          enabled: true
          storageClassName: "gp3"
        kopiaOptions:
          parallelUploads: 4
          parallelDownloads: 4
```

### Snapshot Mode Options

| Mode | Behavior |
|------|----------|
| `auto` (default) | Use CSI snapshot if storage class supports it; fall back to live access |
| `snapshot-only` | Require CSI snapshot; fail if VolumeSnapshotClass not available |
| `live-only` | Always read live PVC data; skip snapshot (faster, no temp PVC storage cost) |

## Standby PVCs

When `standbyPVCConfig.enabled: true` (default), the controller maintains warm standby PVCs on the DR cluster:

- **Same name** as source PVC (in destination namespace)
- **Continuously updated** with latest backup data
- **Labels**: `dr-syncer.io/standby-pvc: "true"`
- **Ready to mount** instantly on failover

### Failover

During cutover, workloads on the DR cluster mount the standby PVCs directly — no data transfer needed at failover time. This enables sub-minute RTO for PVC data.

## S3-Compatible Backends

The `s3Config` section supports any S3-compatible storage:

| Provider | Endpoint Example |
|----------|-----------------|
| AWS S3 | `s3.amazonaws.com` |
| MinIO | `minio.example.com:9000` |
| Google Cloud Storage (S3 compat) | `storage.googleapis.com` |
| DigitalOcean Spaces | `nyc3.digitaloceanspaces.com` |
| Backblaze B2 | `s3.us-west-004.backblazeb2.com` |

## Monitoring

### kubectl Commands

```bash
# Check repository health
kubectl get br -n dr-syncer-system

# Watch backup operations
kubectl get vbo -n dr-syncer-system -w

# Check standby PVC status
kubectl get pvc -l dr-syncer.io/standby-pvc=true -n production
```

### Prometheus Metrics

| Metric | Description |
|--------|-------------|
| `dr_syncer_backup_duration_seconds` | Backup operation duration |
| `dr_syncer_restore_duration_seconds` | Restore operation duration |
| `dr_syncer_backup_bytes_total` | Total bytes uploaded to S3 |
| `dr_syncer_backup_errors_total` | Backup errors by type |
