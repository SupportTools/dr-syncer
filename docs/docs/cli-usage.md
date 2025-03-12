---
sidebar_position: 5
---

# CLI Usage

DR-Syncer provides a command-line interface (CLI) that allows you to perform disaster recovery operations directly from the command line without needing to deploy the controller.

## Installation

The CLI binary is built alongside the controller binary:

```bash
make build
```

This will create both the `dr-syncer` and `dr-syncer-cli` binaries in the `bin/` directory.

## Command-Line Flags

| Flag | Description | Required |
|------|-------------|----------|
| `--source-kubeconfig` | Path to the source cluster kubeconfig file | Yes |
| `--dest-kubeconfig` | Path to the destination cluster kubeconfig file | Yes |
| `--source-namespace` | Namespace in the source cluster | Yes |
| `--dest-namespace` | Namespace in the destination cluster | Yes |
| `--mode` | Operation mode: Stage, Cutover, or Failback | Yes |
| `--include-custom-resources` | Include custom resources in synchronization | No (default: false) |
| `--migrate-pvc-data` | Migrate PVC data using pv-migrate | No (default: false) |
| `--reverse-migrate-pvc-data` | Migrate PVC data from destination back to source (for Failback mode) | No (default: false) |
| `--pv-migrate-flags` | Additional flags to pass to pv-migrate (e.g. "--strategy rsync --lbsvc-timeout 10m") | No (default: none) |
| `--resource-types` | Comma-separated list of resource types to include (overrides defaults) | No |
| `--exclude-resource-types` | Comma-separated list of resource types to exclude | No |
| `--log-level` | Log level: debug, info, warn, error | No (default: info) |

## Operation Modes

### Stage Mode

In Stage mode, the CLI:
1. Synchronizes resources from source to destination namespace
2. Scales down deployments in the destination namespace to 0 replicas
3. Optionally migrates PVC data if enabled

This mode is useful for preparing a disaster recovery environment without activating it.

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage
```

### Cutover Mode

In Cutover mode, the CLI:
1. Synchronizes resources from source to destination namespace
2. Preserves original replica counts by annotating source deployments
3. Scales down deployments in the source namespace to 0 replicas
4. Scales up deployments in the destination namespace to the original replica counts
5. Optionally migrates PVC data if enabled

This mode is used to perform an actual disaster recovery cutover.

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Cutover
```

### Failback Mode

In Failback mode, the CLI:
1. Optionally migrates PVC data from destination back to source (if reverse-migrate-pvc-data is set)
2. Scales down deployments in the destination namespace to 0 replicas
3. Scales up deployments in the source namespace to their original replica counts

This mode is used to return to the original source environment after a DR event.

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Failback \
  --reverse-migrate-pvc-data=true
```

## Resource Types

By default, the CLI synchronizes these standard Kubernetes resources:
- ConfigMaps
- Secrets
- Deployments
- StatefulSets
- DaemonSets
- Services
- Ingresses
- ServiceAccounts
- Roles
- RoleBindings
- PersistentVolumeClaims
- HorizontalPodAutoscalers
- NetworkPolicies

### Including Custom Resources

To include custom resources in synchronization:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage \
  --include-custom-resources=true
```

### Specifying Specific Resource Types

To synchronize only specific resource types:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage \
  --resource-types=configmaps,secrets,deployments
```

### Excluding Resource Types

To exclude specific resource types:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage \
  --exclude-resource-types=ingresses,networkpolicies
```

## PVC Data Migration

The CLI supports migrating data from source PVCs to destination PVCs using [pv-migrate](https://github.com/utkuozdemir/pv-migrate).

### Requirements

- `pv-migrate` must be installed and available in your PATH
- PVCs must exist in both source and destination namespaces

### Usage

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage \
  --migrate-pvc-data=true
```

### Passing Additional Flags to pv-migrate

You can pass additional flags directly to pv-migrate for more control over the migration process:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--lbsvc-timeout 10m --strategy rsync"
```

Common pv-migrate flags include:
- `--lbsvc-timeout`: Timeout for load balancer service creation (default: 3m)
- `--strategy`: Migration strategy to use (rsync, svc, or mnt2)
- `--rsync-opts`: Additional rsync options
- `--no-cleanup-on-failure`: Do not clean up temporary resources on failure

For failback:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Failback \
  --reverse-migrate-pvc-data=true \
  --pv-migrate-flags="--lbsvc-timeout 10m"
