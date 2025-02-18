# DR Syncer Controller

A Kubernetes controller for managing disaster recovery synchronization between clusters. This controller automatically synchronizes resources from source namespaces to destination namespaces in remote clusters based on configurable cron schedules.

## Architecture

The controller uses two Custom Resources to manage DR synchronization:

1. **RemoteCluster**: Defines a remote cluster configuration including:
   - Kubeconfig reference
   - Default cron schedule
   - Default resource types to sync

2. **NamespaceReplication**: Defines namespace-level sync configuration including:
   - Source namespace
   - Destination namespace
   - Custom cron schedule (overrides RemoteCluster default)
   - Resource types to sync (overrides RemoteCluster defaults)
   - Resources to exclude

This separation allows for:
- Managing multiple remote clusters
- Different sync schedules per namespace
- Flexible resource type selection
- Fine-grained control over what gets synchronized

## Features

- Synchronizes multiple resource types:
  - ConfigMaps
  - Secrets
  - Deployments (with replica management)
  - Services
  - Ingresses
- Supports multiple remote clusters
- Flexible namespace mapping
- Resource type filtering
- Resource exclusion lists
- Cron-based scheduling
- Health and readiness probes
- Metrics endpoint for monitoring

## Installation

### Using Helm (Recommended)

1. Add the Helm repository:
```bash
helm repo add supporttools https://charts.support.tools
helm repo update
```

2. Install the chart:
```bash
helm install dr-syncer supporttools/dr-syncer \
  --namespace dr-syncer \
  --create-namespace
```

You can customize the installation by creating a values.yaml file:
```yaml
# values.yaml
controller:
  logLevel: "debug"  # Set log level to debug
  leaderElect: true  # Enable leader election

resources:
  limits:
    cpu: "1"
    memory: 1Gi
  requests:
    cpu: "200m"
    memory: 256Mi
```

Then install with custom values:
```bash
helm install dr-syncer supporttools/dr-syncer \
  --namespace dr-syncer \
  --create-namespace \
  -f values.yaml
```

### Manual Installation

1. Install the CRDs:
```bash
kubectl apply -f config/crd/bases/dr-syncer.io_remoteclusters.yaml
kubectl apply -f config/crd/bases/dr-syncer.io_namespacereplications.yaml
```

2. Build and deploy the controller:
```bash
go build -o dr-syncer
./dr-syncer
```

## Example: Connecting Two Clusters

Here's an example of setting up bi-directional replication between two clusters (Cluster A and Cluster B).

### 1. Create Kubeconfig Secrets

First, create secrets containing the kubeconfig for each cluster:

On Cluster A:
```bash
# Create secret for accessing Cluster B
kubectl create secret generic cluster-b-kubeconfig \
  --from-file=kubeconfig=/path/to/cluster-b-kubeconfig
```

On Cluster B:
```bash
# Create secret for accessing Cluster A
kubectl create secret generic cluster-a-kubeconfig \
  --from-file=kubeconfig=/path/to/cluster-a-kubeconfig
```

### 2. Create RemoteCluster Resources

On Cluster A:
```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: cluster-b
  namespace: default
spec:
  kubeconfigSecretRef:
    name: cluster-b-kubeconfig
    namespace: default
  defaultSchedule: "*/15 * * * *"  # Every 15 minutes
  defaultResourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
    - ingresses
```

On Cluster B:
```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: cluster-a
  namespace: default
spec:
  kubeconfigSecretRef:
    name: cluster-a-kubeconfig
    namespace: default
  defaultSchedule: "*/15 * * * *"  # Every 15 minutes
  defaultResourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
    - ingresses
```

### 3. Create NamespaceReplication Resources

On Cluster A (replicating to Cluster B):
```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceReplication
metadata:
  name: app1-to-b
  namespace: default
spec:
  remoteClusterRef:
    name: cluster-b
    namespace: default
  sourceNamespace: app1-production
  destinationNamespace: app1-dr
  schedule: "0 */2 * * *"  # Every 2 hours
  resourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
  excludeResources:
    - kind: Secret
      name: tls-secret
```

On Cluster B (replicating to Cluster A):
```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceReplication
metadata:
  name: app2-to-a
  namespace: default
spec:
  remoteClusterRef:
    name: cluster-a
    namespace: default
  sourceNamespace: app2-production
  destinationNamespace: app2-dr
  schedule: "0 */2 * * *"  # Every 2 hours
  resourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
```

This setup will:
1. On Cluster A:
   - Run the dr-syncer controller
   - Connect to Cluster B using the provided kubeconfig
   - Replicate app1-production namespace to app1-dr in Cluster B
   - Sync every 2 hours

2. On Cluster B:
   - Run the dr-syncer controller
   - Connect to Cluster A using the provided kubeconfig
   - Replicate app2-production namespace to app2-dr in Cluster A
   - Sync every 2 hours

### Scheduling

The controller supports standard cron scheduling format:
- `* * * * *` = every minute
- `*/5 * * * *` = every 5 minutes
- `0 * * * *` = every hour
- `0 0 * * *` = every day at midnight
- `0 */2 * * *` = every 2 hours

Schedules can be set at two levels:
1. RemoteCluster level (defaultSchedule) - applies to all NamespaceReplications that don't specify their own schedule
2. NamespaceReplication level (schedule) - overrides the RemoteCluster's default schedule

## Resource Handling

### Deployments
- Deployments are synchronized with 0 replicas in the DR cluster
- Original replica count is stored in the `dr-syncer.io/original-replicas` annotation
- Source namespace is stored in the `dr-syncer.io/source-namespace` annotation
- Sync time is stored in the `dr-syncer.io/synced-at` annotation

### Services
- ClusterIP fields are cleared during synchronization
- The controller handles service recreation in the DR cluster

### ConfigMaps and Secrets
- Synchronized as-is, maintaining the latest versions
- System ConfigMaps (like kube-root-ca.crt) are excluded

### Ingresses
- Synchronized with the same configuration
- May require additional setup depending on the ingress controller in the DR cluster

## Development

### Prerequisites
- Go 1.23+
- Kubernetes cluster for testing
- controller-gen (`go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest`)

### Building
```bash
go build -o dr-syncer
```

### Running Tests
```bash
go test ./...
```

### Generating CRDs
```bash
controller-gen crd paths="./..." output:crd:artifacts:config=config/crd/bases
```

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a new Pull Request

## License

This project is licensed under the terms of the LICENSE file included in the repository.
