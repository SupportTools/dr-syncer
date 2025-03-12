---
sidebar_position: 1
---

# PVC Data Migration Techniques

This advanced tutorial covers sophisticated techniques for migrating Persistent Volume Claim (PVC) data between Kubernetes clusters as part of your disaster recovery strategy.

## Overview

Persistent Volume Claims (PVCs) represent storage resources in Kubernetes clusters. When implementing disaster recovery, migrating PVC data is often the most complex part of the process due to:

- Large data volumes
- Continuous data changes
- Storage backend differences
- Network constraints
- Performance considerations

DR-Syncer CLI provides integrated PVC data migration capabilities through the `pv-migrate` tool, but advanced scenarios may require customized approaches.

## Prerequisites

Before diving into advanced PVC migration techniques, ensure you have:

- Completed the [Setting Up Disaster Recovery Environment](../tutorial-basics/setting-up-dr-environment.md) tutorial
- Familiarity with Kubernetes storage concepts (PVs, PVCs, StorageClasses)
- Access to both source and destination clusters with appropriate permissions
- The `pv-migrate` tool installed (used by DR-Syncer CLI)
- Basic understanding of data replication concepts

## Understanding DR-Syncer's PVC Migration

DR-Syncer employs `pv-migrate` for PVC data migration with these flags:

```bash
# During Stage or Cutover
--migrate-pvc-data=true

# During Failback
--reverse-migrate-pvc-data=true
```

Under the hood, DR-Syncer:
1. Identifies matching PVCs in source and destination
2. Uses `pv-migrate` to transfer data
3. Handles retry logic and status tracking

For many scenarios, this built-in functionality is sufficient. For advanced cases, you may need additional controls.

## Migration Strategies

`pv-migrate` supports multiple migration strategies, which can be specified with the `--pv-migrate-flags` parameter:

### Rsync Strategy (Default)

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy rsync"
```

The rsync strategy:
- Creates temporary pods with source and destination PVCs
- Uses rsync over SSH to transfer data
- Works in most scenarios
- Provides good performance balance

### Svc (Service) Strategy

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy svc"
```

The service strategy:
- Uses NodePort services to expose rsync
- May work better in certain network configurations
- Useful when SSH connections are problematic

### Mnt2 (Mount) Strategy

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy mnt2"
```

The mount strategy:
- Mounts both volumes in a single pod
- Performs direct copy between volumes
- Useful for in-cluster migrations
- May not work for cross-cluster scenarios

## Advanced PVC Migration Scenarios

### Customizing Rsync Options

For fine-grained control over rsync behavior:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--rsync-opts='-avz --delete --progress'"
```

Common rsync options:
- `-a` (archive mode): preserves permissions, timestamps, etc.
- `-v` (verbose): provides detailed output
- `-z` (compression): compresses data during transfer
- `--delete`: removes files in destination that don't exist in source
- `--progress`: shows progress during transfer
- `--exclude='path/to/exclude'`: skips specified files/directories

### Handling Extremely Large Volumes

For very large volumes, consider these techniques:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--lbsvc-timeout 60m --rsync-opts='-avz --delete --progress --inplace'"
```

The key flags for large volumes:
- `--lbsvc-timeout`: Increases timeout for service creation
- `--inplace`: Modifies files in-place instead of creating temporary files
- `--partial`: Keeps partially transferred files for resumed transfers
- `--no-cleanup-on-failure`: Retains resources after failure for debugging

### Cross-Provider Migration

When migrating between different cloud providers or storage types:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy svc --skip-ownership"
```

Consider these options:
- `--skip-ownership`: Skips ownership preservation (useful when UID/GID handling differs)
- Using the "svc" strategy to avoid potential SSH compatibility issues
- Verifying storage class compatibility between providers

## Phased Migration Approach

For critical applications with large datasets, consider a phased approach:

### Phase 1: Initial Bulk Transfer

Perform initial data transfer during maintenance window:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true
```

### Phase 2: Incremental Updates

Regularly update with only changed data:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--rsync-opts='-avz --delete'"
```

### Phase 3: Final Sync During Cutover

Perform a final sync during actual DR activation:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Cutover \
  --migrate-pvc-data=true
```

## Application-Specific Considerations

### Databases

For database PVCs, consider:

1. **Quiescing the Database**: Ensure consistent backups

   ```bash
   # Example for PostgreSQL: Put database in backup mode
   KUBECONFIG=/path/to/source/kubeconfig kubectl exec -n my-app postgres-0 -- psql -c "SELECT pg_start_backup('dr-sync');"

   # Run migration
   bin/dr-syncer-cli ... --migrate-pvc-data=true

   # Exit backup mode
   KUBECONFIG=/path/to/source/kubeconfig kubectl exec -n my-app postgres-0 -- psql -c "SELECT pg_stop_backup();"
   ```

2. **Using Database-Specific Tools**: 
   - For some databases, native replication tools may be more effective
   - Consider logical backups for smaller databases

### Log and Cache Volumes

For non-critical data like logs or caches:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--rsync-opts='-avz --size-only'"
```

The `--size-only` flag speeds up the process by only checking file sizes, not checksums.

## Monitoring and Validation

### Monitoring Migration Progress

When running migrations, monitor progress with:

```bash
# Check pv-migrate pods
KUBECONFIG=/path/to/destination/kubeconfig kubectl get pods -n my-app-dr | grep pv-migrate

# Check logs for progress
KUBECONFIG=/path/to/destination/kubeconfig kubectl logs -n my-app-dr pv-migrate-pod-name
```

### Validating Migrated Data

After migration, validate data integrity:

```bash
# Option 1: Check file counts and sizes
KUBECONFIG=/path/to/source/kubeconfig kubectl exec -n my-app app-pod -- find /data -type f | wc -l
KUBECONFIG=/path/to/destination/kubeconfig kubectl exec -n my-app-dr app-pod -- find /data -type f | wc -l

# Option 2: For critical data, calculate checksums
KUBECONFIG=/path/to/source/kubeconfig kubectl exec -n my-app app-pod -- find /data -type f -exec md5sum {} \; > source-checksums.txt
KUBECONFIG=/path/to/destination/kubeconfig kubectl exec -n my-app-dr app-pod -- find /data -type f -exec md5sum {} \; > dest-checksums.txt
diff source-checksums.txt dest-checksums.txt
```

## Troubleshooting

### Common Issues and Solutions

#### Timeout During Migration

```bash
# Extend timeout
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--lbsvc-timeout 60m"
```

#### Permission Errors

```bash
# Skip ownership preservation
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--skip-ownership"
```

#### Network Connectivity Issues

Try different strategies:

```bash
# Try service strategy
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy svc"
```

### Debugging Failed Migrations

For detailed troubleshooting:

```bash
# Preserve resources on failure
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--no-cleanup-on-failure"

# Check pod logs
KUBECONFIG=/path/to/destination/kubeconfig kubectl logs -n my-app-dr pv-migrate-pod-name

# Check events
KUBECONFIG=/path/to/destination/kubeconfig kubectl get events -n my-app-dr
```

## Beyond PV-Migrate: Alternative Approaches

For scenarios where `pv-migrate` doesn't meet requirements, consider:

### 1. Storage-Level Replication

Many storage providers offer native replication:
- AWS EBS snapshots and cross-region copy
- GCP Persistent Disk snapshots
- Azure Disk snapshots
- Storage array-level replication

### 2. Database-Specific Tools

For database workloads:
- PostgreSQL: pg_basebackup, WAL shipping
- MySQL/MariaDB: Logical replication, XtraBackup
- MongoDB: Replica sets with geographically distributed members

### 3. Application-Level Replication

Some applications have built-in replication:
- Elasticsearch cross-cluster replication
- Kafka MirrorMaker
- Custom application synchronization protocols

## Best Practices

1. **Test Migrations**: Always test PVC migration processes in non-production before actual DR
2. **Iterate and Improve**: Refine migration approaches based on test results
3. **Document Volumes**: Maintain documentation about each PVC's purpose, size, and criticality
4. **Prioritize Volumes**: Migrate critical data first, non-critical data later
5. **Monitor Performance**: Track migration speeds and adjust strategies for efficiency
6. **Validate Data**: Always verify data integrity after migration
7. **Automate Where Possible**: Create scripts for repeatable processes

## Next Steps

Now that you understand advanced PVC migration techniques, you might want to explore:

- [Automating DR Processes](automating-dr-processes.md) - Create scripts for automated operations
- [Working with Custom Resources](../tutorial-basics/custom-resources.md) - Handle application-specific resources

By mastering these advanced PVC migration techniques, you can ensure that even your most data-intensive applications are properly protected in your disaster recovery strategy.
