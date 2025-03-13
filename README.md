<p align="center">
  <img src="https://cdn.support.tools/dr-syncer/logo.svg" alt="DR-Syncer Logo" width="250"/>
</p>

# DR-Syncer

DR-Syncer provides tools designed to automate and simplify disaster recovery synchronization between Kubernetes clusters.

## Introduction and Problem Statement

Organizations running Kubernetes in production face several challenges when establishing and maintaining disaster recovery environments:

1. **Manual Configuration Burden**
   - Time-consuming manual setup of DR environments
   - Error-prone resource copying
   - Inconsistent state between clusters

2. **Resource Management Complexity**
   - Difficult tracking of which resources need replication
   - Challenges with resource version management
   - Selective resource synchronization complications

3. **Operational Overhead**
   - Frequent manual intervention required for updates
   - Lack of automation in DR processes
   - Time-intensive DR maintenance and testing

DR-Syncer addresses these challenges by providing automated synchronization of resources from source namespaces to destination namespaces in remote clusters. It enables reliable disaster recovery setups with minimal operational overhead.

## DR-Syncer Tools

DR-Syncer offers **two distinct tools** to handle disaster recovery synchronization:

1. **Controller**: A Kubernetes operator that runs continuously inside your cluster, providing automated and scheduled synchronization
   - Ideal for ongoing automation and "set it and forget it" scenarios
   - Uses Custom Resource Definitions (CRDs) for configuration
   - Supports continuous, scheduled, and manual synchronization modes

2. **CLI**: A standalone command-line tool for direct, on-demand synchronization operations
   - Does not require deploying anything to your clusters
   - Perfect for manual operations, testing, or one-off scenarios
   - Supports Stage, Cutover, and Failback operations with a single command
   - Ideal for organizations that prefer not to deploy additional controllers

Both tools maintain feature parity for the core synchronization capabilities, but are used in different contexts and deployment models.

## How the Controller Works

DR-Syncer follows the Kubernetes operator pattern:

1. **Custom Resources**
   - `RemoteCluster`: Defines remote cluster configuration and authentication
   - `Replication`: Defines synchronization configuration and resource filtering

2. **Reconciliation Flow**
   - Controller watches for custom resource changes
   - Compares desired state with actual state in remote clusters
   - Executes synchronization operations when discrepancies are found
   - Updates status and metrics

3. **Synchronization Process**
   - Resources are filtered based on type and exclusion rules
   - Resources are transformed as needed (e.g., scaling deployments to zero)
   - Resources are applied to remote clusters
   - Status is updated with synchronization results

## Key Features

### Resource Synchronization
- Synchronizes multiple resource types (ConfigMaps, Secrets, Deployments, Services, Ingresses, PVCs)
- Maintains resource state and metadata across clusters
- Handles immutable fields and resource versions

### Deployment Strategies
- Zero replicas in DR cluster by default (preventing resource consumption)
- Scale override capability via labels (`dr-syncer.io/scale-override`)
- Original replica count preservation (stored in annotations)

### Multiple Synchronization Modes
- Manual sync (on-demand)
- Scheduled sync (cron-based)
- Continuous sync (real-time monitoring)

### Resource Management
- Type-based filtering (include/exclude specific resource types)
- Label-based exclusion (`dr-syncer.io/ignore` label)
- Namespace mapping between source and destination clusters
- Service and Ingress handling with network configuration adaptation

### PVC Data Replication
- Cross-cluster PVC data synchronization using rsync
- Secure SSH-based transfer mechanism
- Storage class mapping for different cluster environments
- Access mode mapping for different storage requirements
- Volume size handling and attribute preservation

### Security and Operations
- Multi-cluster support with secure authentication
- Comprehensive status reporting and metrics
- Health monitoring and readiness checks
- Leader election for high availability

## Architecture Overview

### Controller Components
- **Manager**: Handles controller lifecycle and shared dependencies
- **Reconcilers**: Implement controller business logic for custom resources
- **Clients**: Interact with Kubernetes API in source and remote clusters
- **Custom Resources**: Define configuration and state for synchronization

### PVC Sync Architecture
- **Agent DaemonSet**: Deployed on remote clusters with SSH/rsync capability
- **SSH Security Model**: Secure key management with proper access restrictions
- **Direct Access Pattern**: Agent pod accesses PVCs directly without root access
- **Data Flow**: Secure rsync over SSH between controller and agent

## Controller Quick Start

### Controller Installation with Helm

```bash
# Add the DR-Syncer Helm repository
helm repo add dr-syncer https://supporttools.github.io/dr-syncer/charts

# Update repositories
helm repo update

# Install DR-Syncer
helm install dr-syncer dr-syncer/dr-syncer \
  --namespace dr-syncer-system \
  --create-namespace
```

### Basic Configuration Example

```yaml
# Define a remote cluster
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr-cluster
spec:
  kubeconfigSecret: dr-cluster-kubeconfig
  
---
# Define a replication
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
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
  schedule: "0 */6 * * *"  # Every 6 hours
```

For more comprehensive documentation, visit our [documentation site](https://supporttools.github.io/dr-syncer/).

## CLI Usage

The DR-Syncer CLI provides a simple way to perform disaster recovery operations directly from the command line without needing to deploy the controller.

### CLI Installation

Build the CLI binary:

```bash
make build
```

This will create the `dr-syncer-cli` binary in the `bin/` directory.

### Basic CLI Usage

The CLI supports three primary operation modes:

#### Stage Mode (Prepare DR Environment)

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage
```

Stage mode synchronizes resources and scales down deployments to 0 replicas in the destination.

#### Cutover Mode (Activate DR Environment)

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Cutover
```

Cutover mode synchronizes resources, scales down source deployments, and scales up destination deployments.

#### Failback Mode (Return to Original Environment)

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Failback
```

Failback mode scales down destination deployments and scales up source deployments.

### Additional CLI Options

The CLI supports many additional options for resource filtering, PVC data migration, and more:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-namespace \
  --dest-namespace=my-namespace-dr \
  --mode=Stage \
  --include-custom-resources=true \
  --migrate-pvc-data=true \
  --resource-types=configmaps,secrets,deployments
```

For complete CLI documentation, refer to the [CLI Usage Guide](https://supporttools.github.io/dr-syncer/docs/cli-usage).

## Development

### Local Development Setup

To run the controller locally for development:

1. Use the provided `run-local.sh` script:

```bash
./run-local.sh /path/to/controller/kubeconfig
```

The script handles:
- Scaling down the in-cluster controller deployment to prevent conflicts
- Setting the KUBECONFIG environment variable
- Restoring the original deployment when you exit

### Build and Deploy

Build the container:

```bash
make docker-build
```

Deploy the controller:

```bash
make deploy
```

### Architectural Notes

- The controller implements exponential backoff with jitter to prevent API server flooding during failure conditions
- Concurrent operations are managed via a configurable worker pool
- Thread-safe mechanisms are used to handle status updates and prevent race conditions

### Release Process

The project uses GitHub Actions for CI/CD and follows semantic versioning for releases.

#### Creating a Release

To create a new release:

1. Ensure all changes are committed and pushed to the main branch:
   ```bash
   git checkout main
   git pull
   ```

2. Create an annotated tag following semantic versioning:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   ```

3. Push the tag to GitHub:
   ```bash
   git push origin v1.0.0
   ```

#### CI/CD Pipeline

When a tag is pushed, the GitHub Actions pipeline automatically:

1. Builds all Docker images (controller, agent, rsync) with the version tag
2. Pushes images to DockerHub with both version tag and 'latest' tag
3. Packages the Helm chart with the release version
4. Updates the Helm repository with the new chart
5. Creates a GitHub Release with generated release notes

#### Versioning Guidelines

DR-Syncer follows [Semantic Versioning](https://semver.org/):

- **Major version** (`X` in vX.Y.Z): Incompatible API changes or major functionality changes
- **Minor version** (`Y` in vX.Y.Z): New features that are backward-compatible
- **Patch version** (`Z` in vX.Y.Z): Bug fixes and minor improvements that are backward-compatible

For pre-release versions, use formats like:
- `v1.0.0-alpha.1`
- `v1.0.0-beta.2`
- `v1.0.0-rc.1`

## Troubleshooting

### Common Issues

#### Log Spamming
If the remote cluster API calls seem excessive, check the logs for rapid cycling between states. The controller implements exponential backoff to reduce API load during failures.

#### Authentication Errors
When running locally, ensure you're using the proper kubeconfig with the necessary permissions:
```bash
./run-local.sh /path/to/controller/kubeconfig
```

#### Resource Synchronization Failures
Check the status of the Replication resource for error messages:
```bash
kubectl get replications -n <namespace> -o yaml
```

#### PVC Sync Issues
Verify the agent DaemonSet is running on remote clusters:
```bash
kubectl get daemonset -n dr-syncer-system --context=<remote-context>
```

Check agent logs for rsync or SSH errors:
```bash
kubectl logs -n dr-syncer-system -l app=dr-syncer-agent --context=<remote-context> --tail=100
```

#### SSH Key Management
If experiencing SSH authentication problems, check the secrets containing SSH keys:
```bash
kubectl get secret <remote-cluster>-ssh-key -n dr-syncer-system -o yaml
```

### Viewing Logs Properly
Always use `--tail` flag to limit log output:
```bash
kubectl logs pod-name --tail=100  # Last 100 lines
```

Never use `-f/--follow` in automated scripts or tests as these commands will never return and cause scripts to hang.

## Community and Contributing

We welcome contributions from the community! Here's how you can get involved:

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for details on contributing to DR-Syncer, including:
- Code contribution guidelines
- Pull request process
- Development environment setup
- Testing requirements

### Communication

- GitHub Issues: Bug reports, feature requests, and discussions
- Slack Channel: #dr-syncer on Kubernetes community Slack

## License

This project is licensed under the terms of the [LICENSE](LICENSE) file.
