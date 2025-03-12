---
sidebar_position: 2
---

# Setting Up Disaster Recovery Environment

This tutorial walks you through the process of setting up a complete disaster recovery (DR) environment using the DR-Syncer CLI. We'll explore how to properly configure, stage, and validate your DR setup.

## Overview

Setting up a proper DR environment involves more than just copying resources. You need to consider:

- Which resources to include and exclude
- How to handle sensitive data like Secrets
- Strategies for PersistentVolumeClaims (PVCs)
- Validation and testing procedures

This guide builds on the [Getting Started with DR-Syncer CLI](getting-started-cli.md) tutorial and assumes you already have the CLI installed.

## Prerequisites

- DR-Syncer CLI installed and working
- Access to both source and destination clusters
- Kubeconfig files for both clusters
- The application you want to replicate running in the source cluster

## Planning Your DR Environment

### Resource Selection Strategy

Before starting, determine which resources should be included in your DR setup:

1. **Critical Resources**: ConfigMaps, Secrets, Deployments, StatefulSets, Services
2. **Additional Resources**: Ingresses, NetworkPolicies, ServiceAccounts, Roles, RoleBindings
3. **Data Resources**: PersistentVolumeClaims (PVCs)
4. **Custom Resources**: CRDs specific to your application

For most DR scenarios, you'll want to include all standard Kubernetes resources, but there might be specific resources you want to exclude or handle differently.

### Namespace Strategy

For DR, you typically have two approaches:

1. **Same-name namespaces**: Use identical namespace names in both clusters
2. **DR-suffix namespaces**: Append `-dr` or similar to namespace names in the destination cluster

For this tutorial, we'll use the DR-suffix approach for clarity.

## Step 1: Create the Destination Namespace

First, ensure the destination namespace exists:

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl create namespace my-app-dr
```

## Step 2: Perform Initial Resource Sync

Next, perform an initial sync of all resources to establish your DR environment:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage
```

This initial sync will:
- Copy all standard resources from the source namespace to the destination
- Scale down deployments and statefulsets to 0 replicas in the destination
- Keep PVCs for data but won't sync the actual data (covered later)

## Step 3: Verify Initial Resource Sync

Inspect the resources in your destination namespace:

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl get all -n my-app-dr
```

Check that all expected resources are present:

```bash
# Check specific resource types
KUBECONFIG=/path/to/destination/kubeconfig kubectl get configmaps -n my-app-dr
KUBECONFIG=/path/to/destination/kubeconfig kubectl get secrets -n my-app-dr
KUBECONFIG=/path/to/destination/kubeconfig kubectl get deployments -n my-app-dr
```

Verify that workloads (deployments, statefulsets) are scaled to zero:

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl get deployments -n my-app-dr -o=jsonpath='{.items[*].spec.replicas}'
```

All values should be 0.

## Step 4: Customizing Resource Selection

For more control over which resources are synchronized, use the resource filtering flags:

### Including Only Specific Resources

To synchronize only specific resource types:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --resource-types=configmaps,secrets,deployments,services
```

### Excluding Specific Resources

To exclude specific resource types:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --exclude-resource-types=ingresses,networkpolicies
```

This is useful for resources that might not be needed in your DR environment or might need manual configuration.

## Step 5: Handling PVC Data

For applications with stateful data, you'll need to migrate the data stored in PVCs:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true
```

This requires the `pv-migrate` tool to be installed and available. The CLI will:

1. Find matching PVCs in source and destination
2. Use `pv-migrate` to copy data between them
3. Report progress and completion

For large volumes of data, this process can take time. You can customize the migration with additional flags:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy rsync --lbsvc-timeout 20m"
```

## Step 6: Testing Your DR Environment

To validate your DR environment is properly configured, perform spot checks:

### Check Configuration Data

Compare a ConfigMap between source and destination:

```bash
# Get source ConfigMap
KUBECONFIG=/path/to/source/kubeconfig kubectl get configmap my-config -n my-app -o yaml > source-config.yaml

# Get destination ConfigMap
KUBECONFIG=/path/to/destination/kubeconfig kubectl get configmap my-config -n my-app-dr -o yaml > dest-config.yaml

# Compare (excluding metadata)
diff <(yq 'del(.metadata)' source-config.yaml) <(yq 'del(.metadata)' dest-config.yaml)
```

### Test Deployment Scaling

Verify that a deployment can scale up in the destination:

```bash
# Scale up a deployment
KUBECONFIG=/path/to/destination/kubeconfig kubectl scale deployment my-deployment -n my-app-dr --replicas=1

# Check if pods are running
KUBECONFIG=/path/to/destination/kubeconfig kubectl get pods -n my-app-dr
```

This tests that the deployment can create pods correctly. Remember to scale back to 0 after testing:

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl scale deployment my-deployment -n my-app-dr --replicas=0
```

## Step 7: Regular Updates

Once your DR environment is set up, you'll want to keep it updated. Schedule regular sync operations to keep the DR environment current:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage
```

Consider automating this with a cron job or scheduled pipeline.

## Best Practices

1. **Version Control**: Store your DR configuration and scripts in version control
2. **Documentation**: Document your DR setup, procedures, and any environment-specific adjustments
3. **Regular Testing**: Periodically test your DR environment by scaling up services
4. **Monitoring**: Set up monitoring for both environments to quickly detect issues
5. **Automation**: Automate as much as possible to reduce manual error
6. **Secret Management**: Consider using a dedicated solution for secrets in your DR environment

## Troubleshooting Common Issues

### Issue: Resources Not Synchronizing

Check permissions and resource state:

```bash
# Check for errors in the CLI output with debug logging
bin/dr-syncer-cli --mode=Stage ... --log-level=debug

# Verify permissions to read/create resources
KUBECONFIG=/path/to/source/kubeconfig kubectl auth can-i get deployments -n my-app
KUBECONFIG=/path/to/destination/kubeconfig kubectl auth can-i create deployments -n my-app-dr
```

### Issue: PVC Data Migration Failing

For PVC data migration issues:

```bash
# Verify pv-migrate is installed
pv-migrate version

# Check if PVCs exist in both environments
KUBECONFIG=/path/to/source/kubeconfig kubectl get pvc -n my-app
KUBECONFIG=/path/to/destination/kubeconfig kubectl get pvc -n my-app-dr

# Try with increased timeout
--pv-migrate-flags="--lbsvc-timeout 30m"
```

## Next Steps

Now that you have a properly configured DR environment, you're ready to learn about:

- [Performing a DR Cutover](performing-dr-cutover.md) - Execute a full DR activation
- [Failback Operations](failback-operations.md) - Return to normal operations after DR
- [Working with Custom Resources](custom-resources.md) - Handle application-specific custom resources

For more advanced PVC data handling, see the [PVC Data Migration Techniques](../tutorial-extras/pvc-data-migration.md) advanced tutorial.
