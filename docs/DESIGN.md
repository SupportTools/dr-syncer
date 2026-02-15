# DR-Syncer Backup/Restore Architecture

*Last updated: 2026-02-15*

## Overview

DR-Syncer implements a **dual-path PVC synchronization architecture** for disaster recovery:

1. **Rsync Path** (Direct): Cluster-to-cluster data transfer via SSH tunnels and rsync pods. See [memory-bank/pvcReplication.md](../memory-bank/pvcReplication.md) for details.
2. **Backup Path** (Kopia): S3-mediated backup/restore using [Kopia](https://kopia.io/) with warm standby PVCs on the DR cluster.

The backup path introduces pre-provisioned standby PVCs that are continuously restored from S3, enabling near-instant failover during cutover operations.

## Dual-Path Routing

### Decision Point

The routing decision is made in `pkg/controllers/syncer/pvc_handler.go`:

```go
// isBackupPathEnabled checks if the backup path should be used
func isBackupPathEnabled(pvcConfig *drv1alpha1.PVCConfig) bool {
    return pvcConfig != nil &&
        pvcConfig.DataSyncConfig != nil &&
        pvcConfig.DataSyncConfig.BackupConfig != nil &&
        pvcConfig.DataSyncConfig.BackupConfig.Enabled
}
```

**Flow:**
- If `BackupConfig.Enabled == true` -> `syncPVCsViaBackup()` (Kopia path)
- Otherwise -> `syncPersistentVolumeClaimsWithMounting()` (rsync path)

### When to Use Each Path

| Criteria | Backup Path (Kopia) | Rsync Path |
|----------|---------------------|------------|
| **Network** | S3-accessible (no direct cluster link) | Direct cluster-to-cluster |
| **Cutover RTO** | Near-instant (standby PVCs pre-restored) | Minutes to hours (PVCs empty until sync) |
| **Storage Cost** | S3 storage + egress fees | None |
| **RPO** | Last backup interval | Last rsync completion |
| **Audit Trail** | Full snapshot history with retention | No history |
| **Multi-Region** | Excellent (via S3 replication) | Poor (network latency) |
| **Complexity** | Higher (BackupRepository, secrets, CRDs) | Lower |
| **Best For** | Air-gapped DR, compliance, instant cutover | Same-VPC, low-cost, frequent large changes |

## CRD Data Structures

### BackupConfig (in NamespaceMapping)

Defined in `api/v1alpha1/types.go`:

```yaml
spec:
  pvcConfig:
    dataSyncConfig:
      backupConfig:
        enabled: true                        # Master switch for backup path
        backupRepositoryRef:                 # Reference to BackupRepository CR
          name: my-backup-repo
          namespace: dr-syncer-system
        dataAccessStrategy: Auto             # Auto | Snapshot | Live
        parallelism: 4                       # Kopia parallel streams (1-16)
        compressionAlgorithm: zstd           # zstd | s2 | gzip | none
        standbyPVCConfig:
          enabled: true                      # Create warm standby PVCs (default: true)
          storageClassName: "gp3"            # Override destination storage class
          accessMode: "ReadWriteOnce"        # Override destination access mode
        scheduleOverride: "0 */6 * * *"      # Optional cron override
```

### BackupRepository CRD

Defined in `api/v1alpha1/backuprepository_types.go`:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: BackupRepository
metadata:
  name: my-backup-repo
  namespace: dr-syncer-system
spec:
  s3Config:
    endpoint: s3.amazonaws.com
    bucket: dr-syncer-backups
    region: us-east-1
    pathPrefix: cluster-prod/
    credentialsSecretRef:
      name: s3-credentials          # Secret with AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
      namespace: dr-syncer-system
  kopiaConfig:
    encryptionSecretRef:
      name: kopia-encryption        # Secret with KOPIA_PASSWORD
      namespace: dr-syncer-system
    compressionAlgorithm: zstd
  retentionPolicy:
    keepLatest: 10
    keepDaily: 7
    keepWeekly: 4
    keepMonthly: 12
status:
  state: Ready                       # Initializing | Ready | Error | Maintenance
```

### VolumeBackupOperation CRD

Defined in `api/v1alpha1/volumebackupoperation_types.go`. Represents a single backup or restore operation:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: VolumeBackupOperation
metadata:
  name: my-mapping-postgres-data-backup
  labels:
    dr-syncer.io/mapping: my-mapping
    dr-syncer.io/pvc: postgres-data
    dr-syncer.io/operation: backup     # "backup" or "restore"
spec:
  operationType: Backup                # Backup | Restore
  sourcePVC:
    name: postgres-data
    namespace: production
  destinationPVC:                      # Only for Restore
    name: postgres-data-standby
    namespace: production-dr
  backupRepositoryRef:
    name: my-backup-repo
    namespace: dr-syncer-system
  snapshotID: "abc123def456"           # Required for Restore
  dataAccessStrategy: Auto
status:
  phase: BackupComplete                # See phase progression below
  kopiaSnapshotID: "abc123def456"
  bytesTransferred: 1073741824
  progressPercentage: 100
```

**Phase Progression:**

```
Backup:  Pending -> SnapshotCreated -> DataAccessReady -> BackupInProgress -> BackupComplete
Restore: Pending -> DataAccessReady -> RestoreInProgress -> RestoreComplete
Either:  ... -> Failed (on error at any stage)
```

## StandbyPVC Pattern

### Concept

Standby PVCs are pre-provisioned PersistentVolumeClaims on the DR cluster that are continuously restored from Kopia snapshots. This enables near-instant cutover because the data is already on the DR cluster.

### Naming Convention

Source PVC `postgres-data` -> Standby PVC `postgres-data-standby`

Constant: `standbyPVCSuffix = "-standby"` (defined in `pkg/controller/backup/standby_pvc_manager.go` and `pkg/cli/standby_pvc.go`)

### Labels and Annotations

**Labels** applied to every standby PVC:

| Label | Value | Purpose |
|-------|-------|---------|
| `dr-syncer.io/managed-by` | `dr-syncer` | Identifies DR-Syncer-managed PVCs |
| `dr-syncer.io/standby` | `true` | Marks PVC as a standby |
| `dr-syncer.io/source-pvc` | `<source-name>` | Links to source PVC |
| `dr-syncer.io/source-namespace` | `<source-ns>` | Links to source namespace |
| `dr-syncer.io/mapping` | `<mapping-name>` | Links to NamespaceMapping |

**Annotations** updated during lifecycle:

| Annotation | Value | Purpose |
|------------|-------|---------|
| `dr-syncer.io/created-at` | RFC3339 timestamp | Creation time |
| `dr-syncer.io/last-restore-time` | RFC3339 timestamp | Last successful restore |
| `dr-syncer.io/last-restore-snapshot-id` | Kopia snapshot ID | Last restored snapshot |

### StandbyPVCManager Interface

Defined in `pkg/controller/backup/restore_workflow.go`:

```go
type StandbyPVCManager interface {
    EnsureStandbyPVC(ctx, sourcePVC, destNamespace, sourcePVCSpec) (*PVCReference, bool, error)
    UpdateStandbyPVCStatus(ctx, pvcRef, snapshotID) error
}
```

Implementation in `pkg/controller/backup/standby_pvc_manager.go`.

The concrete implementation (`StandbyPVCManagerImpl`) also provides `CleanupStandbyPVCs(ctx, destNamespace) error` for cleanup during NamespaceMapping deletion. This method is not part of the interface because it is used only by the finalizer cleanup path (injected via `StandbyPVCCleanupFunc` to avoid import cycles), not by the sync workflow.

### Storage Class Resolution

When creating a standby PVC, storage class is resolved with this priority:

1. **StandbyPVCConfig.StorageClassName** (highest priority - explicit override)
2. **PVCConfig.StorageClassMappings** (mapped from source class)
3. **Source PVC storage class** (fallback)

Access modes follow the same priority pattern using `StandbyPVCConfig.AccessMode` and `PVCConfig.AccessModeMappings`.

### Lifecycle

**Sync cycle:**
```
1. Controller sync cycle triggers
2. VolumeBackupSyncer.SyncPVCsWithBackup() iterates PVCs
3. For each PVC:
   a. Backup operation runs on source cluster (Kopia snapshot to S3)
   b. StandbyPVCManager.EnsureStandbyPVC() creates standby PVC if needed
   c. Restore operation runs on DR cluster (Kopia restore from S3 to standby PVC)
   d. StandbyPVCManager.UpdateStandbyPVCStatus() annotates with restore metadata
4. Repeat on schedule
```

**Cleanup on NamespaceMapping deletion:**
```
1. NamespaceMapping deletion triggers finalizer (handleDeletion)
2. If backup-based sync is configured (PVCConfig.DataSyncConfig.BackupConfig != nil):
   a. Create controller-runtime client for destination cluster
   b. Create StandbyPVCManager with mapping name for label filtering
   c. Call CleanupStandbyPVCs() — deletes standby PVCs with matching
      dr-syncer.io/mapping label, skips PVCs mounted by pods
   d. Errors are logged but do not block finalizer removal (graceful degradation)
3. CleanupResources() deletes other synced resources
4. Finalizer removed
```

## VolumeBackupSyncer Orchestration

Entry point: `VolumeBackupSyncer.SyncPVCsWithBackup()` in `pkg/controller/backup/volume_backup_syncer.go`.

### Overall Flow

```
SyncPVCsWithBackup()
├── 1. Validate BackupConfig and BackupRepository
├── 2. Construct StandbyPVCManager (if standby enabled)
├── 3. For each PVC:
│   └── syncSinglePVC()
│       ├── a. Acquire backup concurrency slot
│       ├── b. Create backup VolumeBackupOperation on source cluster
│       ├── c. Wait for phase = BackupComplete (poll every 10s)
│       ├── d. EnsureStandbyPVC on DR cluster
│       ├── e. Create restore VolumeBackupOperation on DR cluster
│       ├── f. Wait for phase = RestoreComplete
│       └── g. UpdateStandbyPVCStatus with snapshot metadata
├── 4. Record metrics (RecordLastSuccessfulBackup)
└── 5. Update NamespaceMapping annotations with sync status
```

### Concurrency

Backup operations use a separate `BackupConcurrencyManager` (not shared with rsync concurrency). Each PVC sync acquires a slot before starting and releases it on completion.

## 8-Step RestoreWorkflow

Defined in `pkg/controller/backup/restore_workflow.go`, method `ExecuteRestore()`.

### Steps

| Step | Name | Implementation | Description |
|------|------|---------------|-------------|
| 1 | Verify Repository | `verifyRepository()` | Check BackupRepository state is `Ready`, verify S3 and Kopia encryption secrets exist on DR cluster |
| 2 | Resolve Snapshot ID | `resolveSnapshotID()` | Use provided snapshot ID or find latest completed VolumeBackupOperation with matching labels |
| 3 | Ensure Standby PVC | `ensureStandbyPVC()` | Create standby PVC on DR cluster if it doesn't exist, using StandbyPVCManager |
| 4 | Create Restore Operation | `createRestoreOperation()` | Create VolumeBackupOperation CR with `OperationType=Restore` and resolved snapshot ID |
| 5 | Execute Kopia Restore | `BackupWorkflow.Execute()` | Build and run Kopia restore pod that pulls data from S3 to standby PVC |
| 6 | Progress Monitoring | (concurrent) | Poll pod status every 5s inside `waitForPodCompletion()` |
| 7 | Cleanup | `cleanupRestoreResources()` | Kopia pod cleanup (deferred), placeholder for future temp resource cleanup |
| 8 | Update Status | `updateCompletionStatus()` | Annotate standby PVC with restore time and snapshot ID, return `RestoreResult` |

### Snapshot Resolution (Step 2)

When no explicit snapshot ID is provided, the workflow queries the source cluster for the latest `VolumeBackupOperation` matching:
- Label `dr-syncer.io/mapping: <mapping-name>`
- Label `dr-syncer.io/pvc: <pvc-name>`
- Label `dr-syncer.io/operation: backup`
- Phase `BackupComplete`
- Non-empty `KopiaSnapshotID`

## Backup Execution (BackupWorkflow)

Defined in `pkg/controller/backup/backup_workflow.go`.

### Data Access Strategies

| Strategy | Behavior |
|----------|----------|
| **Auto** | Detect CSI snapshot support. If available, use Snapshot; otherwise fall back to Live |
| **Snapshot** | Create CSI VolumeSnapshot -> temp PVC from snapshot -> Kopia backup from temp PVC. Provides point-in-time consistency |
| **Live** | Mount source PVC directly in Kopia pod. Pins pod to same node as PVC consumer. Faster but no consistency guarantee |

### Snapshot Backup Path

```
1. Create VolumeSnapshot via SnapshotManager
2. Wait for snapshot ready (timeout: 5 min)
3. Create temporary PVC from snapshot
4. Build Kopia backup pod mounting temp PVC
5. Run backup, extract snapshot ID from pod output
6. Cleanup: delete VolumeSnapshot and temp PVC
```

### Live Backup Path

```
1. Find node where source PVC is mounted
2. Build Kopia backup pod pinned to same node
3. Run backup mounting live PVC
4. Extract snapshot ID from pod output
```

### Kopia Pod Construction

Defined in `pkg/controller/backup/kopia_pod.go`.

**Backup command:**
```bash
kopia repository connect s3 \
  --bucket=<bucket> --endpoint=<endpoint> --region=<region> --prefix=<pathPrefix> \
  --override-hostname=dr-syncer --override-username=dr-syncer && \
kopia snapshot create /data \
  --parallel=<parallelism> --compression=<algorithm> --json
```

**Restore command:**
```bash
kopia repository connect s3 \
  --bucket=<bucket> --endpoint=<endpoint> --region=<region> --prefix=<pathPrefix> \
  --override-hostname=dr-syncer --override-username=dr-syncer && \
kopia snapshot restore <snapshot-id> /data \
  --parallel=<parallelism>
```

**Pod security:**
- `RestartPolicy: Never`
- `ActiveDeadlineSeconds: 3600` (1 hour)
- Seccomp profile: RuntimeDefault
- `allowPrivilegeEscalation: false`, drop all capabilities
- Volumes: PVC at `/data`, EmptyDir for Kopia cache at `/home/kopia`, EmptyDir for temp at `/tmp/kopia`

**Environment variables** (from Secrets):
- `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` from S3 credentials secret
- `KOPIA_PASSWORD` from Kopia encryption secret

**Input validation:**
- S3 fields validated against `s3FieldPattern` (alphanumeric, hyphens, dots, slashes)
- Snapshot IDs validated against `snapshotIDPattern` (alphanumeric + hyphens)
- Compression algorithm checked against allowlist

## CLI Integration

The CLI (`pkg/cli/`) uses standby PVCs during DR operations.

### Stage Mode (`pkg/cli/mode.go`)

```
1. Sync resources from source to destination
2. Scale down destination deployments to 0
3. If UseStandbyPVCs:
   - List standby PVCs in destination
   - Report readiness status
4. Else: run pv-migrate for data transfer
```

Stage mode does NOT migrate PVC data when using standby PVCs - the backup path handles this continuously.

### Cutover Mode (`pkg/cli/mode.go`)

```
1. Sync resources from source to destination
2. If UseStandbyPVCs:
   a. Validate standby PVCs (all Bound, all restored at least once)
   b. Rewrite workload PVC references: source PVC -> standby PVC
3. Store original replica counts in annotations
4. Scale down source to 0
5. Scale up destination to original counts
```

**Cutover validation** (`pkg/cli/standby_pvc.go:validateStandbyPVCsForCutover`):
- All standby PVCs must be `Bound`
- All must have a non-nil `LastRestoreTime` (have been restored at least once)
- Returns errors blocking cutover if validation fails

**Workload PVC rewrite** (`pkg/cli/standby_pvc.go:updateWorkloadPVCReferences`):
- Scans Deployments and StatefulSets for PVC volume mounts
- Rewrites `volume.persistentVolumeClaim.claimName` from source to standby name
- Logs warning for StatefulSet `volumeClaimTemplates` (immutable, cannot be rewritten)

### Failback Mode (`pkg/cli/mode.go`)

```
1. If UseStandbyPVCs:
   - Revert workload PVC references: standby PVC -> source PVC
2. Scale down destination to 0
3. Scale up source to original counts
```

**Workload PVC revert** (`pkg/cli/standby_pvc.go:revertWorkloadPVCReferences`):
- Builds reverse mapping: standby name -> source name
- Updates Deployment and StatefulSet volume claims back to originals
- Logs warning for volumeClaimTemplates (require manual intervention)

## Architecture Diagram

```
Source Cluster                     S3 (Object Storage)              DR Cluster
┌──────────────────┐              ┌──────────────────┐              ┌──────────────────┐
│                  │              │                  │              │                  │
│  Source PVC      │   Kopia      │  Kopia Repo      │   Kopia      │  Standby PVC     │
│  (postgres-data) │──backup──>   │  (snapshots)     │──restore──>  │  (postgres-data  │
│                  │              │                  │              │   -standby)      │
│  ┌────────────┐  │              └──────────────────┘              │  ┌────────────┐  │
│  │ Kopia Pod  │  │                                               │  │ Kopia Pod  │  │
│  │ (backup)   │  │                                               │  │ (restore)  │  │
│  └────────────┘  │                                               │  └────────────┘  │
│                  │                                               │                  │
└──────────────────┘                                               └──────────────────┘

                    Controller Cluster
                    ┌──────────────────────────────┐
                    │  DR-Syncer Controller         │
                    │  VolumeBackupSyncer           │
                    │  - Orchestrates backup/restore│
                    │  - Manages standby PVCs       │
                    │  - Handles CLI operations     │
                    └──────────────────────────────┘
```

## Key Files Reference

| File | Purpose |
|------|---------|
| `api/v1alpha1/types.go` | BackupConfig, StandbyPVCConfig types |
| `api/v1alpha1/backuprepository_types.go` | BackupRepository CRD |
| `api/v1alpha1/volumebackupoperation_types.go` | VolumeBackupOperation CRD |
| `pkg/controllers/syncer/pvc_handler.go` | Dual-path routing (backup vs rsync) |
| `pkg/controller/backup/volume_backup_syncer.go` | Backup orchestration for all PVCs |
| `pkg/controller/backup/backup_workflow.go` | Backup/restore execution (Kopia pod lifecycle) |
| `pkg/controller/backup/restore_workflow.go` | 8-step RestoreWorkflow |
| `pkg/controller/backup/standby_pvc_manager.go` | Standby PVC creation and status management |
| `pkg/controller/backup/kopia_pod.go` | Kopia pod spec construction and command building |
| `pkg/controller/backup/progress_monitor.go` | Backup/restore progress monitoring |
| `pkg/controller/backup/metrics.go` | Backup metrics recording |
| `pkg/cli/mode.go` | CLI Stage/Cutover/Failback workflows |
| `pkg/cli/standby_pvc.go` | CLI standby PVC helpers (list, validate, rewrite) |
