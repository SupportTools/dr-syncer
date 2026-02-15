---
sidebar_position: 10
---

# Helm Values Reference

Complete reference for all configurable values in the DR-Syncer Helm chart.

## Quick Start

```bash
# Install with defaults
helm install dr-syncer oci://ghcr.io/supporttools/dr-syncer \
  --namespace dr-syncer \
  --create-namespace

# Install with custom values
helm install dr-syncer oci://ghcr.io/supporttools/dr-syncer \
  --namespace dr-syncer \
  --create-namespace \
  --values values.yaml
```

---

## Controller Configuration

### Core Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of controller replicas | `1` |
| `controller.logLevel` | Log level (debug, info, warn, error) | `"debug"` |
| `controller.logVerbosity` | Kubernetes client-go verbosity (0-9) | `9` |
| `controller.metricsAddr` | Prometheus metrics bind address | `":8080"` |
| `controller.probeAddr` | Health probe bind address | `":8081"` |
| `controller.enableLeaderElection` | Enable leader election for HA | `true` |
| `controller.leaderElectionID` | Leader election lock name | `"dr-syncer.io"` |
| `controller.syncInterval` | Interval between sync operations | `"5m"` |
| `controller.resyncPeriod` | Full resource resync period | `"1h"` |
| `controller.ignoreCert` | Skip TLS verification for remote clusters | `true` |

```yaml
# Example: Production configuration
controller:
  logLevel: "info"
  logVerbosity: 0
  enableLeaderElection: true
  syncInterval: "10m"
  resyncPeriod: "2h"
  ignoreCert: false  # Enforce TLS verification
```

### Watch Configuration

Controls behavior for Continuous replication mode.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.watch.bufferSize` | Watch event buffer size | `1024` |
| `controller.watch.maxConcurrentReconciles` | Maximum concurrent reconciliations | `5` |
| `controller.watch.backgroundSyncInterval` | Background sync interval for continuous mode | `"1h"` |

```yaml
# Example: High-throughput configuration
controller:
  watch:
    bufferSize: 2048
    maxConcurrentReconciles: 10
    backgroundSyncInterval: "30m"
```

### Default Replication Settings

Default values used when not specified in CRDs.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.replication.defaultMode` | Default replication mode | `"Scheduled"` |
| `controller.replication.defaultSchedule` | Default cron schedule | `"*/5 * * * *"` |
| `controller.replication.defaultScaleToZero` | Scale deployments to 0 in DR | `true` |
| `controller.replication.defaultResourceTypes` | Default resource types to sync | See below |

Default resource types:
```yaml
controller:
  replication:
    defaultResourceTypes:
      - configmaps
      - secrets
      - deployments
      - services
      - ingresses
      - persistentvolumeclaims
```

---

## Image Configuration

### Controller Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Controller image repository | `docker.io/supporttools/dr-syncer` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag (defaults to chart appVersion) | `"latest"` |

### Agent Image

The agent runs as a DaemonSet on remote clusters for PVC synchronization.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.image.repository` | Agent image repository | `docker.io/supporttools/dr-syncer-agent` |
| `agent.image.pullPolicy` | Agent image pull policy | `IfNotPresent` |
| `agent.image.tag` | Agent image tag | `"latest"` |

### Rsync Pod Image

Used for rsync operations during PVC synchronization.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rsyncPod.image.repository` | Rsync pod image repository | `docker.io/supporttools/dr-syncer-rsync` |
| `rsyncPod.image.pullPolicy` | Rsync pod image pull policy | `IfNotPresent` |
| `rsyncPod.image.tag` | Rsync pod image tag | `"latest"` |

```yaml
# Example: Using private registry
image:
  repository: registry.example.com/dr-syncer/controller
  pullPolicy: Always
  tag: "v0.1.0"

agent:
  image:
    repository: registry.example.com/dr-syncer/agent
    tag: "v0.1.0"

rsyncPod:
  image:
    repository: registry.example.com/dr-syncer/rsync
    tag: "v0.1.0"

imagePullSecrets:
  - name: registry-credentials
```

---

## Rsync Configuration

Controls PVC data synchronization behavior.

### SSH Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rsync.ssh.port` | SSH port for rsync connections | `2222` |
| `rsync.concurrency` | Concurrent rsync operations | `3` |

### Retry Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rsync.retryConfig.maxRetries` | Maximum retry attempts | `5` |
| `rsync.retryConfig.initialDelay` | Initial retry delay | `"5s"` |
| `rsync.retryConfig.maxDelay` | Maximum retry delay | `"5m"` |

### Health Check Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rsync.healthCheck.interval` | Health check interval | `"5m"` |
| `rsync.healthCheck.sshTimeout` | SSH connection timeout | `"10s"` |
| `rsync.healthCheck.retryAttempts` | Health check retry attempts | `3` |
| `rsync.healthCheck.retryInterval` | Interval between retry attempts | `"30s"` |

```yaml
# Example: Aggressive retry configuration
rsync:
  ssh:
    port: 2222
  concurrency: 5
  retryConfig:
    maxRetries: 10
    initialDelay: "10s"
    maxDelay: "10m"
  healthCheck:
    interval: "2m"
    sshTimeout: "30s"
    retryAttempts: 5
    retryInterval: "1m"
```

---

## PVC Mount Configuration

Controls temporary pods used for PVC volume access.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `pvcMount.pauseImage` | Pause container image for mount pods | `k8s.gcr.io/pause:3.6` |
| `pvcMount.podRunningTimeout` | Timeout waiting for mount pods | `"3m"` |
| `pvcMount.resources.requests.cpu` | CPU request for mount pods | `"10m"` |
| `pvcMount.resources.requests.memory` | Memory request for mount pods | `"32Mi"` |
| `pvcMount.resources.limits.cpu` | CPU limit for mount pods | `"50m"` |
| `pvcMount.resources.limits.memory` | Memory limit for mount pods | `"64Mi"` |

```yaml
# Example: Air-gapped environment
pvcMount:
  pauseImage: "registry.example.com/pause:3.6"
  podRunningTimeout: "5m"
  resources:
    requests:
      cpu: "20m"
      memory: "64Mi"
    limits:
      cpu: "100m"
      memory: "128Mi"
```

---

## Resource Configuration

### Controller Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |

```yaml
# Example: Production sizing
resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi
```

See [Performance & Capacity Planning](./performance.md) for sizing recommendations.

---

## Pod Scheduling

### Node Selector

```yaml
nodeSelector:
  kubernetes.io/os: linux
  node-role.kubernetes.io/infra: "true"
```

### Tolerations

```yaml
tolerations:
  - key: "node-role.kubernetes.io/infra"
    operator: "Exists"
    effect: "NoSchedule"
```

### Affinity

```yaml
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: dr-syncer
          topologyKey: kubernetes.io/hostname
```

---

## Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `"dr-syncer"` |

```yaml
# Example: With IAM role annotation (AWS)
serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/dr-syncer-role
  name: "dr-syncer"
```

---

## RBAC Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rbac.create` | Create cluster-wide RBAC resources | `true` |
| `namespaceRBAC.create` | Create namespace-scoped RBAC resources | `true` |

---

## CRD Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs | `true` |
| `crds.keepOnUninstall` | Keep CRDs on chart uninstall | `true` |

---

## Security Configuration

### Pod Security Context

```yaml
podSecurityContext:
  fsGroup: 2000
  runAsNonRoot: true
```

### Container Security Context

```yaml
securityContext:
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 1000
```

---

## Full Example: Production Configuration

```yaml
# Production values.yaml
replicaCount: 1

image:
  repository: docker.io/supporttools/dr-syncer
  pullPolicy: IfNotPresent
  tag: "v0.1.0"

agent:
  image:
    repository: docker.io/supporttools/dr-syncer-agent
    tag: "v0.1.0"

rsyncPod:
  image:
    repository: docker.io/supporttools/dr-syncer-rsync
    tag: "v0.1.0"

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi

controller:
  logLevel: "info"
  logVerbosity: 0
  enableLeaderElection: true
  syncInterval: "10m"
  resyncPeriod: "2h"
  ignoreCert: false
  watch:
    bufferSize: 2048
    maxConcurrentReconciles: 10
    backgroundSyncInterval: "1h"
  replication:
    defaultMode: "Scheduled"
    defaultSchedule: "*/15 * * * *"
    defaultScaleToZero: true

rsync:
  ssh:
    port: 2222
  concurrency: 5
  retryConfig:
    maxRetries: 10
    initialDelay: "10s"
    maxDelay: "10m"
  healthCheck:
    interval: "5m"
    sshTimeout: "30s"
    retryAttempts: 5
    retryInterval: "1m"

pvcMount:
  pauseImage: "k8s.gcr.io/pause:3.6"
  podRunningTimeout: "5m"

serviceAccount:
  create: true
  name: "dr-syncer"

rbac:
  create: true

crds:
  install: true
  keepOnUninstall: true

podSecurityContext:
  fsGroup: 2000
  runAsNonRoot: true

securityContext:
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 1000

nodeSelector:
  kubernetes.io/os: linux

tolerations: []

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: dr-syncer
          topologyKey: kubernetes.io/hostname
```

---

## Upgrade Procedures

### Pre-Upgrade Checklist

1. **Backup CRDs**: Export current CRD definitions
   ```bash
   kubectl get crds -o yaml | grep dr-syncer.io > crds-backup.yaml
   ```

2. **Document current values**:
   ```bash
   helm get values dr-syncer -n dr-syncer > current-values.yaml
   ```

3. **Check release notes**: Review changelog for breaking changes

### Upgrade Steps

```bash
# 1. Update Helm repository
helm repo update

# 2. Preview changes
helm diff upgrade dr-syncer oci://ghcr.io/supporttools/dr-syncer \
  --namespace dr-syncer \
  --values values.yaml

# 3. Perform upgrade
helm upgrade dr-syncer oci://ghcr.io/supporttools/dr-syncer \
  --namespace dr-syncer \
  --values values.yaml \
  --wait

# 4. Verify upgrade
kubectl get pods -n dr-syncer
kubectl get crds | grep dr-syncer.io
```

### Rollback Procedures

```bash
# List release history
helm history dr-syncer -n dr-syncer

# Rollback to previous version
helm rollback dr-syncer <revision> -n dr-syncer

# Verify rollback
kubectl get pods -n dr-syncer
```

---

## Downgrade Procedures

Downgrades require careful handling of CRDs to avoid data loss.

### Pre-Downgrade Checklist

1. **Export all DR-Syncer resources**:
   ```bash
   kubectl get remoteclusters -A -o yaml > remoteclusters-backup.yaml
   kubectl get replications -A -o yaml > replications-backup.yaml
   kubectl get namespacemappings -A -o yaml > namespacemappings-backup.yaml
   kubectl get pvcsyncoperations -A -o yaml > pvcsyncops-backup.yaml
   ```

2. **Check compatibility**: Verify target version supports your CRD schemas

### Downgrade Steps

```bash
# 1. Scale down controller
kubectl scale deployment dr-syncer -n dr-syncer --replicas=0

# 2. Wait for termination
kubectl wait --for=delete pod -l app.kubernetes.io/name=dr-syncer -n dr-syncer --timeout=60s

# 3. Downgrade Helm release
helm upgrade dr-syncer oci://ghcr.io/supporttools/dr-syncer \
  --namespace dr-syncer \
  --version <target-version> \
  --values values.yaml

# 4. Verify CRDs are compatible
kubectl get crds | grep dr-syncer.io

# 5. Verify resources still exist
kubectl get remoteclusters -A
kubectl get replications -A
```

### Troubleshooting Downgrades

If resources fail validation after downgrade:

```bash
# Remove problematic fields from resources
kubectl patch remotecluster <name> -n <namespace> --type=json \
  -p='[{"op": "remove", "path": "/spec/newField"}]'

# Or restore from backup
kubectl apply -f remoteclusters-backup.yaml
```

---

## Related Documentation

- [Installation](./installation.md) - Initial installation guide
- [Performance](./performance.md) - Capacity planning and tuning
- [Troubleshooting](./troubleshooting.md) - Diagnosing issues
