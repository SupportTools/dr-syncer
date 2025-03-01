# DR-Syncer

DR-Syncer is a Kubernetes operator for disaster recovery that synchronizes resources between Kubernetes clusters.

## Overview

This controller synchronizes Kubernetes resources across clusters enabling disaster recovery and multi-cluster operations. It supports continuous sync mode and scheduled sync operations for PVCs, deployments, and other cluster resources.

## Features

- Cross-cluster resource synchronization
- Persistent Volume Claim (PVC) data replication
- Scheduled and continuous sync modes
- Resource filtering and transformation
- Automatic backoff and retry mechanisms

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

### Architectural Notes

- The controller implements exponential backoff with jitter to prevent API server flooding during failure conditions
- Concurrent operations are managed via a configurable worker pool
- Thread-safe mechanisms are used to handle status updates and prevent race conditions

### Build and Deploy

Build the container:

```bash
make docker-build
```

Deploy the controller:

```bash
make deploy
```

## Troubleshooting

### Common Issues

**Log Spamming**: 
If the remote cluster API calls seem excessive, check the logs for rapid cycling between states. The controller implements exponential backoff to reduce API load during failures.

**Authentication Errors**:
When running locally, ensure you're using the proper kubeconfig with the necessary permissions:
```
./run-local.sh /path/to/controller/kubeconfig
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for details on contributing to DR-Syncer.

## License

This project is licensed under the terms of the [LICENSE](LICENSE) file.
