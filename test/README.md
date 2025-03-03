# DR Syncer Test Environment

This directory contains test cases and utilities for testing the DR Syncer controller.

## Setup

Before running tests, you need to set up the test environment with the required kubeconfig files and cluster configurations.

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

### Running Tests

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

## Test Cases

Each test case is in a subdirectory under `test/cases/` and includes:
- `README.md`: Description of the test case
- `controller.yaml`: Replication resource for the controller cluster
- `remote.yaml`: Resources to create in the source cluster
- `test.sh`: Test script that verifies the replication works correctly

## Remote Clusters Configuration

The `remote-clusters.yaml` file defines:
1. Two RemoteCluster resources (nyc3 and sfo3) in the `dr-syncer` namespace
2. A ClusterMapping between them in the `dr-syncer` namespace

This configuration is used by the test cases to verify replication between clusters. All resources are created in the `dr-syncer` namespace, which is where the controller looks for them.
