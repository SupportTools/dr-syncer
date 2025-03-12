---
sidebar_position: 8
---

# Custom Resource Reference

DR-Syncer uses Custom Resource Definitions (CRDs) to configure and control its behavior. This page provides a comprehensive reference for all available fields and their purposes.

## RemoteCluster

The `RemoteCluster` CRD defines connection details and authentication for a remote Kubernetes cluster.

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  kubeconfigSecret: dr-cluster-kubeconfig
  sshKeySecret: dr-cluster-ssh-key
  agentDeployment:
    image: supporttools/dr-syncer-agent:latest
    resources:
      limits:
        cpu: "100m"
        memory: "128Mi"
      requests:
        cpu: "50m"
        memory: "64Mi"
status:
  phase: Ready
  lastConnectionTime: "2025-03-08T15:30:00Z"
  agentStatus:
    deployed: true
    readyReplicas: 3
    observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2025-03-08T15:30:00Z"
      reason: ConnectionEstablished
      message: "Successfully connected to remote cluster"
```

### RemoteCluster Spec Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `kubeconfigSecret` | String | Name of the Secret containing the kubeconfig file for accessing the remote cluster | Yes |
| `sshKeySecret` | String | Name of the Secret containing SSH keys for PVC data replication | No |
| `agentDeployment` | Object | Configuration for the agent DaemonSet deployed on the remote cluster | No |
| `agentDeployment.image` | String | Container image for the agent | No |
| `agentDeployment.resources` | Object | Resource requests and limits for the agent | No |

### RemoteCluster Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | String | Current phase of the remote cluster connection (Pending, Ready, Failed) |
| `lastConnectionTime` | DateTime | Timestamp of the last successful connection |
| `agentStatus` | Object | Status of the agent deployment |
| `agentStatus.deployed` | Boolean | Whether the agent has been deployed |
| `agentStatus.readyReplicas` | Integer | Number of ready agent replicas |
| `agentStatus.observedGeneration` | Integer | The observed generation of the agent DaemonSet |
| `conditions` | Array | List of status conditions |

## Replication

The `Replication` CRD defines synchronization configuration between namespaces.

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: production-to-dr
spec:
  sourceNamespace: production
  destinationNamespace: production-dr
  destinationCluster: dr-cluster
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - Ingress
    - PersistentVolumeClaim
  excludeResources:
    - name: temporary-config
      kind: ConfigMap
  excludeLabels:
    - key: dr-syncer.io/ignore
      value: "true"
  sync:
    mode: Scheduled
    schedule: "0 */6 * * *"
  deploymentConfig:
    scaleToZero: true
  serviceConfig:
    preserveClusterIP: false
  ingressConfig:
    preserveAnnotations: true
    preserveTLS: true
    preserveBackends: true
  pvcConfig:
    includeData: true
    storageClassMapping:
      standard: standard-dr
    accessModeMapping:
      ReadWriteOnce: ReadWriteMany
status:
  phase: Running
  lastSyncTime: "2025-03-08T18:00:00Z"
  nextSyncTime: "2025-03-09T00:00:00Z"
  syncStats:
    totalSyncs: 36
    successfulSyncs: 35
    failedSyncs: 1
    resourcesSynced: 152
    resourcesFailed: 2
  resourceStatus:
    - kind: Deployment
      synced: 10
      failed: 0
    - kind: ConfigMap
      synced: 15
      failed: 0
    - kind: Secret
      synced: 20
      failed: 0
    - kind: Service
      synced: 5
      failed: 0
    - kind: Ingress
      synced: 2
      failed: 0
    - kind: PersistentVolumeClaim
      synced: 5
      failed: 2
  conditions:
    - type: Syncing
      status: "True"
      lastTransitionTime: "2025-03-08T18:00:00Z"
      reason: ScheduledSync
      message: "Synchronization in progress"
```

### Replication Spec Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `sourceNamespace` | String | Source namespace to synchronize resources from | Yes |
| `destinationNamespace` | String | Destination namespace to synchronize resources to | Yes |
| `destinationCluster` | String | Name of the RemoteCluster resource for the destination cluster | Yes |
| `resourceTypes` | Array of Strings | List of Kubernetes resource types to synchronize | Yes |
| `excludeResources` | Array of Objects | List of specific resources to exclude from synchronization | No |
| `excludeResources[].name` | String | Name of the resource to exclude | Yes |
| `excludeResources[].kind` | String | Kind of the resource to exclude | Yes |
| `excludeLabels` | Array of Objects | List of label key/value pairs to exclude resources with matching labels | No |
| `excludeLabels[].key` | String | Label key to match | Yes |
| `excludeLabels[].value` | String | Label value to match | No |
| `sync` | Object | Synchronization configuration | No |
| `sync.mode` | String | Synchronization mode (Manual, Scheduled, Continuous) | No |
| `sync.schedule` | String | Cron expression for scheduled synchronization | No |
| `deploymentConfig` | Object | Configuration for Deployment resources | No |
| `deploymentConfig.scaleToZero` | Boolean | Whether to scale Deployments to zero replicas in the destination | No |
| `serviceConfig` | Object | Configuration for Service resources | No |
| `serviceConfig.preserveClusterIP` | Boolean | Whether to preserve the ClusterIP in Service resources | No |
| `ingressConfig` | Object | Configuration for Ingress resources | No |
| `ingressConfig.preserveAnnotations` | Boolean | Whether to preserve annotations in Ingress resources | No |
| `ingressConfig.preserveTLS` | Boolean | Whether to preserve TLS configurations in Ingress resources | No |
| `ingressConfig.preserveBackends` | Boolean | Whether to preserve backend configurations in Ingress resources | No |
| `pvcConfig` | Object | Configuration for PersistentVolumeClaim resources | No |
| `pvcConfig.includeData` | Boolean | Whether to synchronize PVC data in addition to the resource | No |
| `pvcConfig.storageClassMapping` | Map | Mapping of source storage classes to destination storage classes | No |
| `pvcConfig.accessModeMapping` | Map | Mapping of source access modes to destination access modes | No |

### Replication Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | String | Current phase of replication (Pending, Running, Completed, Failed) |
| `lastSyncTime` | DateTime | Timestamp of the last synchronization |
| `nextSyncTime` | DateTime | Estimated timestamp of the next scheduled synchronization |
| `syncStats` | Object | Synchronization statistics |
| `syncStats.totalSyncs` | Integer | Total number of synchronization attempts |
| `syncStats.successfulSyncs` | Integer | Number of successful synchronizations |
| `syncStats.failedSyncs` | Integer | Number of failed synchronizations |
| `syncStats.resourcesSynced` | Integer | Total number of resources successfully synchronized |
| `syncStats.resourcesFailed` | Integer | Total number of resources that failed to synchronize |
| `resourceStatus` | Array of Objects | Status broken down by resource kind |
| `resourceStatus[].kind` | String | Kind of resource |
| `resourceStatus[].synced` | Integer | Number of successfully synchronized resources of this kind |
| `resourceStatus[].failed` | Integer | Number of failed resources of this kind |
| `conditions` | Array | List of status conditions |

## Resource Labels

DR-Syncer uses the following special labels to control resource behavior:

| Label | Description |
|-------|-------------|
| `dr-syncer.io/ignore` | Set to "true" to exclude a resource from synchronization |
| `dr-syncer.io/scale-override` | Set to "true" on a Deployment to maintain original replica count instead of scaling to zero |

## Spec Field Details

### Sync Modes

The `sync.mode` field can have the following values:

- **Manual**: Synchronization only happens when triggered manually
- **Scheduled**: Synchronization happens according to a cron schedule
- **Continuous**: Synchronization happens immediately when changes are detected

### Storage Class Mapping

The `pvcConfig.storageClassMapping` field allows you to map between different storage classes in source and destination clusters:

```yaml
pvcConfig:
  storageClassMapping:
    standard: standard-dr
    fast-ssd: fast-ssd-dr
    local-storage: remote-storage
```

### Access Mode Mapping

The `pvcConfig.accessModeMapping` field allows you to map between different PVC access modes:

```yaml
pvcConfig:
  accessModeMapping:
    ReadWriteOnce: ReadWriteMany
    ReadOnlyMany: ReadOnlyMany
```

## Common Usage Examples

### Basic Disaster Recovery Setup

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  kubeconfigSecret: dr-cluster-kubeconfig
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: production-to-dr
spec:
  sourceNamespace: production
  destinationNamespace: production-dr
  destinationCluster: dr-cluster
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  sync:
    mode: Scheduled
    schedule: "0 */6 * * *"
  deploymentConfig:
    scaleToZero: true
```

### PVC Data Replication

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  kubeconfigSecret: dr-cluster-kubeconfig
  sshKeySecret: dr-cluster-ssh-key
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: data-replication
spec:
  sourceNamespace: database
  destinationNamespace: database-dr
  destinationCluster: dr-cluster
  resourceTypes:
    - PersistentVolumeClaim
    - Deployment
    - Service
  pvcConfig:
    includeData: true
    storageClassMapping:
      fast-ssd: standard-dr
    accessModeMapping:
      ReadWriteOnce: ReadWriteMany
```

### Multi-Namespace Replication

For each namespace pair you want to replicate, create a separate Replication resource:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: production-to-dr
spec:
  sourceNamespace: production
  destinationNamespace: production-dr
  destinationCluster: dr-cluster
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: staging-to-dr
spec:
  sourceNamespace: staging
  destinationNamespace: staging-dr
  destinationCluster: dr-cluster
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
```

## Best Practices

### Deployment Configuration

- Use `scaleToZero: true` to minimize resource usage in DR environments
- Label critical Deployments with `dr-syncer.io/scale-override: "true"` if they should maintain replicas in DR

### Security

- Create a dedicated ServiceAccount in the remote cluster with the minimum required permissions
- Regularly rotate kubeconfig and SSH key secrets
- Use network policies to restrict connections between clusters

### Monitoring

- Create alerts for Replication resources with a Failed status
- Monitor `syncStats` to track synchronization health over time
- Set up dashboards to visualize replication status across multiple namespaces

### Performance

- Consider the impact of large PVC data replication on network performance
- For large clusters, use multiple Replication resources with specific resource types rather than a single resource with all types
- Schedule synchronizations during off-peak hours for production workloads
