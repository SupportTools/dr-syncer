# Test Case 25: Backup-Based PVC Sync

## Overview

Tests the Kopia backup-based PVC data synchronization path. This is the alternative
to rsync-based sync that uses S3 as an intermediary via the Kopia backup tool.

## What is Tested

1. **BackupRepository** CR creation and readiness
2. **Backup operation**: Source PVC data is backed up via Kopia to S3
3. **VolumeBackupOperation** CRs are created for backup and restore
4. **Standby PVC** creation on DR cluster with correct labels
5. **Data restore**: Test data is correctly restored to standby PVCs
6. **Resource sync**: Deployment and ConfigMap are synced alongside PVC data
7. **Scale-to-zero**: DR deployment is scaled to 0 replicas
8. **NamespaceMapping status**: Phase reaches Completed

## Prerequisites

- Three clusters available (prod, dr, controller) with kubeconfigs
- MinIO running in controller cluster at `minio.minio-system.svc.cluster.local:9000`
- `local-path` StorageClass available on both clusters
- dr-syncer controller running with BackupRepository and VolumeBackupOperation CRDs installed

## Files

| File | Purpose |
|------|---------|
| `backup-setup.yaml` | S3 credentials, Kopia password, BackupRepository CR |
| `remote.yaml` | Source namespace, PVC, data-writer pod, deployment, configmap |
| `controller.yaml` | NamespaceMapping with backupConfig enabled |
| `test.sh` | Test script |

## Running

```bash
./test/run-tests.sh --test 25
./test/run-tests.sh --test 25 --debug      # Verbose output
./test/run-tests.sh --test 25 --no-cleanup  # Keep resources for inspection
```

## Backup Configuration

The NamespaceMapping uses `dataAccessStrategy: Live` (direct PVC mount) rather than
`Snapshot` (CSI VolumeSnapshot) for compatibility with k3d test infrastructure that
does not support CSI snapshots.
