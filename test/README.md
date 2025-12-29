# DR Syncer Test Environment

This directory contains test cases and utilities for testing the DR Syncer controller.

## Quick Start - E2E Testing with k3d

The fastest way to run E2E tests locally is using k3d (Kubernetes in Docker):

```bash
# Run full E2E suite (setup, deploy, test, cleanup)
make e2e

# Or step-by-step:
make e2e-setup      # Create k3d clusters
make e2e-deploy     # Build and deploy controller
make e2e-test       # Run tests
make e2e-cleanup    # Delete clusters
```

### Prerequisites for k3d E2E Testing

- Docker (running)
- k3d v5.x (`curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash`)
- kubectl
- Helm 3.x

### E2E Test Options

```bash
# Run with debug output
./test/e2e/run-e2e.sh --debug

# Run specific test case
./test/e2e/run-e2e.sh --test 00

# Keep clusters after tests (for debugging)
./test/e2e/run-e2e.sh --no-cleanup

# Skip cluster creation (use existing)
./test/e2e/run-e2e.sh --skip-setup

# Skip controller deployment (use existing)
./test/e2e/run-e2e.sh --skip-deploy
```

### E2E Architecture

The E2E infrastructure creates three k3d clusters on a shared Docker network:

```
Docker Network: k3d-dr-syncer
├── k3d-controller (API: localhost:6443)
│   ├── DR-Syncer Controller
│   ├── RemoteCluster CRDs
│   └── Kubeconfig Secrets
│
├── k3d-prod (API: localhost:6444)
│   ├── Source resources
│   └── Test workloads
│
└── k3d-dr (API: localhost:6445)
    ├── Destination resources
    └── Synced workloads
```

Kubeconfigs are exported to `kubeconfig/controller`, `kubeconfig/prod`, and `kubeconfig/dr`.

### E2E Scripts

| Script | Purpose |
|--------|---------|
| `test/e2e/k3d-setup.sh` | Create k3d clusters and Docker network |
| `test/e2e/k3d-teardown.sh` | Delete k3d clusters and cleanup |
| `test/e2e/deploy-controller.sh` | Build images and deploy controller via Helm |
| `test/e2e/run-e2e.sh` | Main orchestrator for full E2E suite |

---

## Testing with External Clusters

If you prefer to test against external Kubernetes clusters (e.g., production-like environments), follow the setup below.

### Prerequisites

1. Three Kubernetes clusters:
   - Controller cluster: Where the DR Syncer controller runs
   - Production cluster (nyc3): The source cluster for replication
   - DR cluster (sfo3): The destination cluster for replication

2. Kubeconfig files for each cluster placed in the `kubeconfig` directory:
   - `kubeconfig/controller`: Kubeconfig for the controller cluster
   - `kubeconfig/prod`: Kubeconfig for the production cluster (nyc3)
   - `kubeconfig/dr`: Kubeconfig for the DR cluster (sfo3)

### Setting Up Test Clusters

Run the setup script to configure the test clusters:

```bash
./test/setup-test-clusters.sh
```

This script will:
1. Verify the kubeconfig files exist and the clusters are accessible
2. Create the `dr-syncer` namespace in the controller cluster if it doesn't exist
3. Create kubeconfig secrets for the remote clusters:
   - `dr-syncer-nyc3-kubeconfig` using the prod kubeconfig (with key "kubeconfig")
   - `dr-syncer-sfo3-kubeconfig` using the dr kubeconfig (with key "kubeconfig")
4. Apply the `remote-clusters.yaml` configuration to set up the RemoteCluster and ClusterMapping resources
5. Verify the setup was successful

Note: SSH key generation for PVC synchronization is handled by the controller itself. The controller will create and manage SSH key secrets for each RemoteCluster (e.g., `dr-syncer-nyc3-ssh-keys` and `dr-syncer-sfo3-ssh-keys`) in the `dr-syncer` namespace and push the keys to the remote clusters. Each RemoteCluster must have a unique SSH key secret name to avoid conflicts.

### Running Tests (External Clusters)

To run all test cases:

```bash
./test/run-tests.sh
```

To run a specific test case:

```bash
./test/run-tests.sh --test <test_number>
```

For example, to run test case 00:

```bash
./test/run-tests.sh --test 00
```

Additional options:
- `--no-cleanup`: Skip cleanup of resources after tests
- `--debug`: Enable debug output

---

## Test Cases

Each test case is in a subdirectory under `test/cases/` and includes:
- `README.md`: Description of the test case
- `controller.yaml`: Replication resource for the controller cluster
- `remote.yaml`: Resources to create in the source cluster
- `test.sh`: Test script that verifies the replication works correctly

### Test Case List

| Test | Description |
|------|-------------|
| 00-basic-configmap | Basic ConfigMap synchronization |
| 01-basic-secret | Basic Secret synchronization |
| 02-basic-deployment | Deployment synchronization with scaling |
| 03-basic-service | Service synchronization |
| ... | See `test/cases/` for complete list |

---

## CI/CD Integration

E2E tests run automatically in GitHub Actions on:
- Push to `main` branch
- Pull requests to `main`
- Manual dispatch via workflow

The workflow (`.github/workflows/e2e-tests.yml`) supports:
- Debug mode via workflow dispatch
- Running specific test cases
- Artifact upload for test logs on failure

---

## Remote Clusters Configuration

The `remote-clusters.yaml` file defines:
1. Two RemoteCluster resources (nyc3 and sfo3) in the `dr-syncer` namespace
2. A ClusterMapping between them in the `dr-syncer` namespace

This configuration is used by the test cases to verify replication between clusters. All resources are created in the `dr-syncer` namespace, which is where the controller looks for them.

---

## Troubleshooting

### k3d Clusters Not Starting

```bash
# Check Docker is running
docker ps

# Remove stale clusters
k3d cluster delete controller prod dr

# Recreate with debug
./test/e2e/k3d-setup.sh --debug
```

### Controller Logs

```bash
# View controller logs
kubectl --kubeconfig kubeconfig/controller -n dr-syncer logs -l app.kubernetes.io/name=dr-syncer -f

# Check controller events
kubectl --kubeconfig kubeconfig/controller -n dr-syncer get events --sort-by='.lastTimestamp'
```

### Test Failures

```bash
# Check RemoteCluster status
kubectl --kubeconfig kubeconfig/controller -n dr-syncer get remoteclusters -o yaml

# Check NamespaceMapping status
kubectl --kubeconfig kubeconfig/controller -n dr-syncer get namespacemappings -o yaml

# View collected logs
ls -la test/e2e/logs/
```

### Cleanup Stuck Resources

```bash
# Force cleanup all k3d resources
./test/e2e/k3d-teardown.sh --force

# Or manually
k3d cluster delete controller prod dr
docker network rm k3d-dr-syncer
```
