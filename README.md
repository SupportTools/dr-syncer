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
