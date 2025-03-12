---
sidebar_position: 7
---

# Examples

This section provides practical examples of DR-Syncer configurations for various use cases.

## Basic Resource Replication

This example sets up a basic replication of resources from a production namespace to a DR namespace:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
  namespace: default
spec:
  kubeconfigSecret:
    name: dr-cluster-kubeconfig
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: basic-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - Ingress
  mode:
    type: Continuous  # Continuous sync
```

## Scheduled Replication with PVC Sync

This example configures scheduled replication with PVC data synchronization:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
  namespace: default
spec:
  kubeconfigSecret:
    name: dr-cluster-kubeconfig
  pvcSync:
    enabled: true
    agent:
      resources:
        limits:
          cpu: "500m"
          memory: "512Mi"
        requests:
          cpu: "100m"
          memory: "128Mi"
    storageClassMapping:
      "standard": "standard-dr"
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: scheduled-pvc-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: database
  destinationNamespace: database-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - Ingress
    - PersistentVolumeClaim
  mode:
    type: Scheduled
    schedule: "0 1 * * *"  # Daily at 1 AM
  pvcReplication:
    enabled: true
    includeNames:
      - data-pvc
      - logs-pvc
    schedule: "0 2 * * *"  # Daily at 2 AM
```

## Label-Based Resource Selection

This example uses labels to select specific resources for replication:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: label-based-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  # Only replicate resources with this label
  labelSelector:
    matchLabels:
      dr-sync: "true"
  # Resources with this label will be excluded
  excludeLabels:
    dr-syncer.io/ignore: "true"
  mode:
    type: Continuous
```

## Multi-Namespace Mapping

This example shows how to map multiple source namespaces to different destination namespaces:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: ClusterMapping
metadata:
  name: multi-namespace-mapping
spec:
  remoteCluster:
    name: dr-cluster
  namespaceMappings:
    - sourceNamespace: frontend
      destinationNamespace: frontend-dr
    - sourceNamespace: backend
      destinationNamespace: backend-dr
    - sourceNamespace: database
      destinationNamespace: database-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  mode:
    type: Scheduled
    schedule: "0 2 * * *"  # Daily at 2 AM
```

## Deployment Scale Control

This example demonstrates how to control deployment scaling in DR environments:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: scale-control-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  mode:
    type: Continuous
```

With this configuration:

1. By default, all deployments will have 0 replicas in the DR cluster to save resources.
2. To maintain specific replicas in DR, use the `dr-syncer.io/scale-override` label:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: critical-service
  namespace: production
  labels:
    dr-syncer.io/scale-override: "true"  # This deployment will maintain its replicas in DR
spec:
  replicas: 2
  # ... rest of deployment spec
```

## Storage Class Mapping

This example shows how to map storage classes between different clusters:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
  namespace: default
spec:
  kubeconfigSecret:
    name: dr-cluster-kubeconfig
  pvcSync:
    enabled: true
    # Storage class mapping
    storageClassMapping:
      "prod-fast-ssd": "dr-standard-ssd"
      "prod-high-iops": "dr-premium-ssd"
      "prod-standard": "dr-standard"
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: storage-class-mapping
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: database
  destinationNamespace: database-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - PersistentVolumeClaim
  pvcReplication:
    enabled: true
```

## Ingress Configuration

This example demonstrates advanced ingress handling configurations:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: ingress-handling
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: web-app
  destinationNamespace: web-app-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - Ingress
  ingressConfig:
    preserveAnnotations: true  # Maintain annotations in DR
    preserveTLS: true          # Keep TLS configuration
    preserveBackends: false    # Update backend services for DR
  mode:
    type: Continuous
```

## Manual Replication Mode

This example configures manual replication for controlled DR testing:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: manual-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - Ingress
  mode:
    type: Manual  # Only sync when triggered manually
```

To trigger a manual replication:

```bash
kubectl annotate replication manual-replication dr-syncer.io/sync=$(date +%s) --overwrite
```

## Multiple DR Clusters

This example shows how to configure replication to multiple DR clusters:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster-east
  namespace: default
spec:
  kubeconfigSecret:
    name: dr-cluster-east-kubeconfig
---
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster-west
  namespace: default
spec:
  kubeconfigSecret:
    name: dr-cluster-west-kubeconfig
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: east-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster-east
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  mode:
    type: Continuous
---
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: west-replication
  namespace: default
spec:
  remoteCluster:
    name: dr-cluster-west
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  mode:
    type: Continuous
