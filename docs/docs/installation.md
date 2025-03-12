---
sidebar_position: 4
---

# Installation & Configuration

This guide will walk you through the installation and configuration of DR-Syncer. There are multiple installation methods available depending on your requirements.

> **Note:** For details on how DR-Syncer works and its architecture, see [Architecture & Principles](./architecture.md).

## Prerequisites

Before installing DR-Syncer, ensure you have the following:

- Kubernetes clusters (both source and destination)
- Helm 3.x installed
- kubectl configured with access to both clusters
- Adequate permissions to create resources in both clusters (namespace-admin or cluster-admin)

## Installation Options

### Option 1: Using Helm (Recommended)

DR-Syncer can be installed using Helm, which is the recommended installation method for most users:

```bash
# Add the DR-Syncer Helm repository
helm repo add supporttools https://charts.support.tools

# Update Helm repositories
helm repo update

# Install DR-Syncer
helm install dr-syncer supporttools/dr-syncer \
  --namespace dr-syncer \
  --create-namespace \
  --values values.yaml
```

### Option 2: Using Make (For Development)

The project includes a comprehensive Makefile that simplifies building, deployment, and management of DR-Syncer. This method is particularly useful during development or when you need to customize the installation.

```bash
# Clone the repository
git clone https://github.com/supporttools/dr-syncer.git
cd dr-syncer

# Deploy to your current cluster context
make deploy-local

# Or deploy to a specific cluster
make deploy-dr      # Deploy to DR cluster
make deploy-prod    # Deploy to Production cluster
```

The `deploy-local` target performs the following steps:
- Builds the controller, agent, and rsync Docker images
- Pushes the images to the configured Docker registry
- Installs CRDs
- Deploys the Helm chart with proper values

#### Customizing the Deployment

You can customize the deployment using environment variables:

```bash
# Use a specific namespace
make deploy-local HELM_NAMESPACE=my-custom-namespace

# Use custom Docker registry
make deploy-local DOCKER_REGISTRY=registry.example.com

# Specify a kubeconfig file
make deploy-local KUBECONFIG=/path/to/kubeconfig
```

#### Build Only Images

If you only want to build the Docker images without deploying:

```bash
# Build all images
make docker-build-all

# Or build individual components
make docker-build        # Controller only
make docker-build-agent  # Agent only
make docker-build-rsync  # Rsync only
```

### Option 3: Install CRDs Only

If you only want to install the Custom Resource Definitions:

```bash
# Using make
make install-crds

# Or using kubectl
kubectl apply -f config/crd/bases/
```

### Configuration Values

Create a `values.yaml` file to customize the DR-Syncer installation:

```yaml
crd:
  install: true

image:
  re

controller:
  logLevel: "info"  # Options: debug, info, warn, error
  leaderElect: true
  
resources:
  limits:
    cpu: "1"
    memory: 1Gi
  requests:
    cpu: "200m"
    memory: 256Mi

# Agent image configuration
agent:
  image:
    repository: supporttools/dr-syncer-agent
    tag: latest
  resources:
    limits:
      cpu: "500m"
      memory: "512Mi"
    requests:
      cpu: "100m"
      memory: "128Mi"

# Rsync pod configuration
rsyncPod:
  image:
    repository: supporttools/dr-syncer-rsync
    tag: latest
```

For a complete list of configuration options, refer to the [values.yaml template](https://github.com/supporttools/dr-syncer/blob/main/charts/dr-syncer/values.yaml.template).

## Remote Cluster Configuration

### Creating Remote Cluster Resources

After installing DR-Syncer, you need to configure Remote Cluster resources:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
  namespace: dr-syncer  # Important: Use the same namespace as DR-Syncer
spec:
  # Reference to a secret containing the kubeconfig
  kubeconfigSecret:
    name: dr-cluster-kubeconfig
  
  # PVC sync configuration (optional)
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
    # Storage class mapping
    storageClassMapping:
      "source-storage-class": "destination-storage-class"
```

### Setting Up Kubeconfig Secrets

The remote cluster configuration requires a valid kubeconfig file that allows access to the destination cluster. This kubeconfig must be stored as a Kubernetes secret.

#### Important Kubeconfig Requirements

- **Standard Authentication Only**: DR-Syncer requires a standard kubeconfig that uses static credentials like client certificates, tokens, or basic auth.
- **No External Authentication Providers**: Kubeconfigs that use external authentication providers (like AWS IAM for EKS, Azure AD, GCP Auth, OIDC) are NOT supported.
- **Long-lived Credentials**: Ensure the credentials in the kubeconfig do not expire, or implement a rotation mechanism.

#### Converting EKS Kubeconfig

If you're using EKS, the default kubeconfig uses AWS IAM authentication which isn't supported. Convert it to a token-based kubeconfig:

```bash
# Generate a service account token-based kubeconfig for EKS
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: dr-syncer-remote
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: dr-syncer-remote-admin
subjects:
- kind: ServiceAccount
  name: dr-syncer-remote
  namespace: kube-system
roleRef:
  kind: ClusterRole
  name: cluster-admin
  apiGroup: rbac.authorization.k8s.io
EOF

# Get the service account token
TOKEN=$(kubectl -n kube-system get secret $(kubectl -n kube-system get serviceaccount dr-syncer-remote -o jsonpath='{.secrets[0].name}') -o jsonpath='{.data.token}' | base64 --decode)
CLUSTER_NAME=$(kubectl config view --minify -o jsonpath='{.clusters[0].name}')
CLUSTER_CA=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
CLUSTER_SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')

# Create a new kubeconfig file with the token
cat > dr-remote-kubeconfig.yaml <<EOF
apiVersion: v1
kind: Config
clusters:
- name: ${CLUSTER_NAME}
  cluster:
    certificate-authority-data: ${CLUSTER_CA}
    server: ${CLUSTER_SERVER}
contexts:
- name: dr-syncer-remote
  context:
    cluster: ${CLUSTER_NAME}
    user: dr-syncer-remote
current-context: dr-syncer-remote
users:
- name: dr-syncer-remote
  user:
    token: ${TOKEN}
EOF
```

#### Example Standard Kubeconfig

Here's an example of a valid standard kubeconfig file:

```yaml
apiVersion: v1
kind: Config
clusters:
- name: remote-cluster
  cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0t...
    server: https://remote-api.example.com:6443
contexts:
- name: remote-context
  context:
    cluster: remote-cluster
    user: remote-user
current-context: remote-context
users:
- name: remote-user
  user:
    # Option 1: Using client certificate/key
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0t...
    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVkt...
    
    # Option 2: Using static token (use only one authentication method)
    # token: eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9...
```

#### Creating the Kubeconfig Secret

After preparing a valid kubeconfig, create a Kubernetes secret containing this file:

```bash
kubectl -n dr-syncer create secret generic dr-cluster-kubeconfig \
  --from-file=kubeconfig=/path/to/remote-kubeconfig.yaml
```

## Cluster Configuration

### Cluster Mapping Setup

Before setting up replication, you need to define the relationship between your clusters using a ClusterMapping resource:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: ClusterMapping
metadata:
  name: prod-dr-mapping
  namespace: dr-syncer  # Important: Use the same namespace as DR-Syncer
spec:
  # Define source and target clusters
  sourceCluster: production-cluster
  targetCluster: dr-cluster
  
  # SSH key secret reference for secure communication between clusters
  sshKeySecretRef:
    name: cluster-ssh-keys
    namespace: dr-syncer
```

Create the SSH key secret for secure communication:

```bash
# Generate SSH keys if you don't have them
ssh-keygen -t rsa -b 4096 -f cluster-keys -N ""

# Create the secret
kubectl -n dr-syncer create secret generic cluster-ssh-keys \
  --from-file=id_rsa=cluster-keys \
  --from-file=id_rsa.pub=cluster-keys.pub
```

## Namespace Mapping Configuration

### Basic Namespace Mapping

Create a NamespaceMapping resource to configure which resources should be synchronized:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr
  namespace: dr-syncer  # Important: Use the same namespace as DR-Syncer
spec:
  # Reference to the ClusterMapping
  clusterMappingRef:
    name: prod-dr-mapping
  
  # Source namespace
  sourceNamespace: production
  
  # Destination namespace in target cluster
  destinationNamespace: production-dr
  
  # Resource type filtering
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - Ingress
  
  # Synchronization schedule (cron format)
  schedule: "0 1 * * *"  # Daily at 1 AM
  
  # Replication mode
  replicationMode: Scheduled
```

### Label-based Filtering

You can use labels to include or exclude specific resources:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr-with-labels
  namespace: dr-syncer  # Important: Use the same namespace as DR-Syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  
  # Include resources with these labels
  labelSelector:
    matchLabels:
      dr-sync: "true"
```

### PVC Configuration

To enable PVC replication and data synchronization:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr-with-pvc
  namespace: dr-syncer  # Important: Use the same namespace as DR-Syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
    - PersistentVolumeClaim
  
  # PVC configuration
  pvcConfig:
    syncData: true  # Enable data synchronization
    # Storage class mappings
    storageClassMappings:
      - from: "source-storage-class"
        to: "destination-storage-class"
    # Access mode mappings (optional)
    accessModeMappings:
      - from: "ReadWriteOnce"
        to: "ReadWriteMany"
    # Data sync configuration
    dataSyncConfig:
      concurrentSyncs: 2
      timeout: "30m"
```

### Continuous Replication Mode

For real-time replication with change detection:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr-continuous
  namespace: dr-syncer  # Important: Use the same namespace as DR-Syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
  
  # Set continuous replication mode
  replicationMode: Continuous
  
  # Continuous mode configuration
  continuous:
    watchResources: true
    backgroundSyncInterval: "1h"
```

### Manual Replication Mode

For on-demand replication (triggered manually):

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr-manual
  namespace: dr-syncer
spec:
  # Set manual replication mode
  replicationMode: Manual
  
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - Service
```

To trigger a manual sync:

```bash
# Annotate the NamespaceMapping to trigger sync
kubectl -n dr-syncer annotate namespacemappings production-dr-manual dr-syncer.io/sync-now="true"
```

## Advanced Configuration

### Label-based Resource Exclusion

You can exclude specific resources from replication by adding the `dr-syncer.io/ignore: "true"` label:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: database-deployment
  namespace: production
  labels:
    app: database
    dr-syncer.io/ignore: "true"  # This deployment will be excluded from replication
spec:
  replicas: 3
  # ... rest of deployment spec
```

### Wildcard Resource Selection

You can use wildcards to replicate all resource types:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr-all-resources
  namespace: dr-syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  
  # Use wildcard to replicate all resource types
  resourceTypes:
    - "*"
```

### PVC Data Synchronization

For PVCs that need their data synchronized between clusters:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr-with-data-sync
  namespace: dr-syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - PersistentVolumeClaim
    - Deployment
    - StatefulSet
  
  # Advanced PVC configuration
  pvcConfig:
    # Enable data synchronization
    syncData: true
    
    # Detailed data sync configuration
    dataSyncConfig:
      # Limit bandwidth usage (KB/s)
      bandwidthLimit: 5000
      
      # Configure rsync options
      rsyncOptions:
        - --archive
        - --delete
        - --checksum
      
      # Exclude paths from synchronization
      excludePaths:
        - tmp/**
        - cache/**
      
      # Maximum concurrent PVC syncs
      concurrentSyncs: 2
      
      # Timeout for sync operations
      timeout: "2h"
```

### Immutable Resource Handling

Configure how immutable resources are handled during replication:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr
  namespace: dr-syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - ConfigMap
    - Secret
    - Deployment
    - StatefulSet
  
  # Immutable resource configuration
  immutableResourceConfig:
    # Options: NoChange, Recreate, RecreateWithPodDrain, PartialUpdate, ForceUpdate
    defaultHandling: Recreate
    
    # Timeout for pod draining when using RecreateWithPodDrain
    drainTimeout: 5m
    
    # Timeout for force deletion operations
    forceDeleteTimeout: 2m
    
    # Resource-specific handling overrides
    resourceOverrides:
      "statefulsets.apps": RecreateWithPodDrain
```

### Namespace Configuration

Control how namespaces are managed:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr
  namespace: dr-syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  
  # Namespace handling configuration
  namespaceConfig:
    # Create destination namespace if it doesn't exist
    createNamespace: true
    
    # Preserve namespace labels from source
    preserveLabels: true
    
    # Preserve namespace annotations from source
    preserveAnnotations: true
```

### Scale Control

Control the scaling of deployments in the target cluster:

```yaml
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: production-dr
  namespace: dr-syncer
spec:
  clusterMappingRef:
    name: prod-dr-mapping
  sourceNamespace: production
  destinationNamespace: production-dr
  resourceTypes:
    - Deployment
    - StatefulSet
  
  # Scale deployments to zero in target cluster
  scaleToZero: true
```

## Verification

After installation, verify that DR-Syncer is running:

```bash
# Check if the pod is running
kubectl get pods -n dr-syncer

# Check the controller logs
kubectl logs -n dr-syncer -l app=dr-syncer -c controller

# Check created CRDs
kubectl get crds | grep dr-syncer.io

# To verify a specific deployment
kubectl get deployments -n dr-syncer
```

If you installed with make, you can also check the deployment status:

```bash
# View Helm release status
helm status dr-syncer -n dr-syncer
```

## Troubleshooting

### Common Issues

1. **Authentication Errors**:
   - Ensure the kubeconfig secret is correctly formatted
   - Verify the kubeconfig has sufficient permissions

2. **Resource Synchronization Failures**:
   - Check the controller logs for specific error messages
   - Verify network connectivity between clusters
   - Check resource permissions in destination cluster

3. **PVC Sync Issues**:
   - Ensure the agent is running on nodes with PVC access
   - Verify SSH connectivity between clusters
   - Check volume mount permissions

### Checking Status

You can check the status of namespace mappings and cluster mappings:

```bash
# Check namespace mappings
kubectl get namespacemappings -n dr-syncer
kubectl get namespacemapping <name> -n dr-syncer -o yaml

# Check cluster mappings
kubectl get clustermappings -n dr-syncer
kubectl describe clustermapping <name> -n dr-syncer

# View detailed status
kubectl get namespacemappings -n dr-syncer -o custom-columns=NAME:.metadata.name,SOURCE:.spec.sourceNamespace,DESTINATION:.spec.destinationNamespace,PHASE:.status.phase,LAST_SYNC:.status.lastSyncTime
```

Look for the status conditions and phase information to diagnose issues.

## Uninstallation

To remove DR-Syncer from your cluster:

```bash
# Using make
make undeploy

# Or using Helm directly
helm uninstall dr-syncer -n dr-syncer

# To also remove CRDs (optional, be careful as this will delete all associated resources)
make uninstall
# Or
kubectl delete -f config/crd/bases/
```
