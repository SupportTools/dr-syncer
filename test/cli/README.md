# DR-Syncer CLI Tests

This directory contains tests for the DR-Syncer CLI tool.

## Test CLI Script

The `test-cli.sh` script tests the three main operation modes of the DR-Syncer CLI:

1. **Stage Mode**: Tests resource synchronization with deployments scaled to 0 in the destination.
2. **Cutover Mode**: Tests resource synchronization with source deployments scaled to 0 and destination deployments scaled up.
3. **Failback Mode**: Tests scaling down destination deployments and scaling up source deployments.

## Prerequisites

- Kubernetes clusters configured with kubeconfig files in `./kubeconfig/` directory
  - Source cluster kubeconfig (`./kubeconfig/prod`)
  - Destination cluster kubeconfig (`./kubeconfig/dr`)
- The `dr-syncer-cli` binary must be built (`make build-cli` or `make build`)

## Usage

```bash
# Run the test script
./test/cli/test-cli.sh
```

## What the Test Does

1. Sets up test namespaces in source and destination clusters
2. Creates test resources in source namespace:
   - Deployment (nginx, 3 replicas)
   - Service
   - ConfigMap
   - Secret
3. Tests Stage Mode:
   - Syncs resources to destination
   - Verifies resources exist in destination
   - Verifies deployments are scaled to 0 in destination
4. Tests Cutover Mode:
   - Syncs resources to destination
   - Scales down source deployments to 0
   - Scales up destination deployments to original replica count (3)
5. Tests Failback Mode:
   - Scales down destination deployments to 0
   - Scales up source deployments to original replica count (3)
6. Cleans up test resources

## Customization

If needed, you can modify the following variables at the top of the script:

```bash
SOURCE_KUBECONFIG="./kubeconfig/prod"  # Path to source kubeconfig
DEST_KUBECONFIG="./kubeconfig/dr"      # Path to destination kubeconfig
SOURCE_NAMESPACE="test-app"            # Source namespace
DEST_NAMESPACE="test-app-dr"           # Destination namespace
CLI_BIN="./bin/dr-syncer-cli"          # Path to CLI binary
