# DR Syncer

DR Syncer is a Kubernetes controller designed to automate and simplify disaster recovery synchronization between Kubernetes clusters. It provides automated, scheduled synchronization of resources from source namespaces to destination namespaces in remote clusters.

## Features

- Multiple replication modes:
  - **Scheduled Mode**: Traditional cron-based scheduling for periodic synchronization
  - **Continuous Mode**: Real-time replication using resource watchers with background sync
  - **Manual Mode**: On-demand synchronization triggered via CRD updates
- Multi-cluster support
- Flexible resource filtering
- Namespace mapping
- PVC and storage class handling
- Deployment replica management
- Service and Ingress handling
- Immutable resource handling

## Installation

### Using Helm

```bash
helm repo add dr-syncer https://supporttools.github.io/dr-syncer
helm install dr-syncer dr-syncer/dr-syncer
```

## Usage

### Basic Configuration

1. Create a RemoteCluster resource for each cluster:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: source-cluster
  namespace: dr-syncer
spec:
  kubeconfigSecretRef:
    name: source-cluster-kubeconfig
    namespace: dr-syncer
```

2. Create a Replication resource to define synchronization:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: app-replication
  namespace: dr-syncer
spec:
  replicationMode: Scheduled  # Scheduled, Continuous, or Manual
  sourceCluster: source-cluster
  destinationCluster: destination-cluster
  sourceNamespace: app-namespace
  destinationNamespace: app-namespace-dr
  schedule: "*/15 * * * *"  # Every 15 minutes (for Scheduled mode)
  scaleToZero: true
  resourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
```

### Replication Modes

#### Scheduled Mode
Traditional cron-based scheduling for periodic synchronization:

```yaml
spec:
  replicationMode: Scheduled
  schedule: "*/15 * * * *"  # Required for Scheduled mode
```

#### Continuous Mode
Real-time replication using resource watchers with background sync:

```yaml
spec:
  replicationMode: Continuous
  continuous:
    watchResources: true
    backgroundSyncInterval: "1h"  # Optional background full sync
```

#### Manual Mode
On-demand synchronization triggered via CRD updates:

```yaml
spec:
  replicationMode: Manual
```

To trigger a manual sync, add the annotation:
```bash
kubectl annotate replication <name> dr-syncer.io/trigger-sync="true" -n dr-syncer
```

## Configuration

### Helm Values

Key configuration options in `values.yaml`:

```yaml
controller:
  # Watch configuration for continuous mode
  watch:
    bufferSize: 1024
    maxConcurrentReconciles: 5
    backgroundSyncInterval: "1h"

  # Default replication mode configuration
  replication:
    defaultMode: "Scheduled"
    defaultSchedule: "*/5 * * * *"
    defaultScaleToZero: true
    defaultResourceTypes:
      - configmaps
      - secrets
      - deployments
      - services
      - ingresses
      - persistentvolumeclaims
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
