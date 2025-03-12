---
sidebar_position: 5
---

# Working with Custom Resources

This tutorial explains how to handle custom resources with the DR-Syncer CLI. While standard Kubernetes resources are synchronized by default, custom resources require specific configuration.

## Overview

Custom Resources (CRs) are extensions of the Kubernetes API that allow you to define your own resource types and use them like built-in resources. Many applications use custom resources for their configuration and state management, making them critical components of a comprehensive disaster recovery strategy.

By default, DR-Syncer handles standard Kubernetes resources like ConfigMaps, Secrets, Deployments, Services, etc. To include custom resources in your DR synchronization, you need to explicitly enable them.

## Prerequisites

Before working with custom resources, ensure you have:

- Completed the [Getting Started with DR-Syncer CLI](getting-started-cli.md) tutorial
- Access to both source and destination clusters with custom resource definitions (CRDs) installed
- Applications using custom resources in your source environment
- Permission to interact with the custom resources in both clusters

## Understanding Custom Resource Handling

### Types of Custom Resources

Custom resources typically fall into these categories:

1. **Application Configuration**: Resources that define how your application should behave
2. **Application State**: Resources that store the state of your application
3. **Operational Resources**: Resources that define operational aspects like scaling, backup schedules, etc.

### Challenges with Custom Resources

Synchronizing custom resources poses several challenges:

1. **CRD Dependencies**: Custom resources require their CRD to be installed in both clusters
2. **Status Fields**: Many custom resources have status fields that shouldn't be synchronized
3. **Cluster-Specific References**: Some fields may contain cluster-specific references
4. **Finalizers**: Custom resources may have finalizers that should be handled carefully

## Step 1: Identify Custom Resources in Your Source Namespace

First, identify what custom resources exist in your source namespace:

```bash
# List API resources available in the cluster
KUBECONFIG=/path/to/source/kubeconfig kubectl api-resources --namespaced=true --verbs=list -o name

# Check for custom resources in your namespace
KUBECONFIG=/path/to/source/kubeconfig kubectl get $(kubectl api-resources --namespaced=true --verbs=list -o name | grep -v "^[^.]*$" | tr "\n" ",") -n my-app
```

Take note of which custom resources your application uses and their importance for disaster recovery.

## Step 2: Verify CRDs in Destination Cluster

Ensure that the Custom Resource Definitions (CRDs) are installed in your destination cluster:

```bash
# List CRDs in source cluster
KUBECONFIG=/path/to/source/kubeconfig kubectl get crd

# List CRDs in destination cluster
KUBECONFIG=/path/to/destination/kubeconfig kubectl get crd
```

If any CRDs are missing in the destination cluster, you need to install them:

```bash
# Export CRD from source cluster
KUBECONFIG=/path/to/source/kubeconfig kubectl get crd my-custom-resource.example.com -o yaml > my-crd.yaml

# Clean up the exported CRD (remove status and other cluster-specific fields)
yq eval 'del(.status, .metadata.creationTimestamp, .metadata.resourceVersion, .metadata.uid, .metadata.generation, .metadata.annotations, .metadata.selfLink)' my-crd.yaml > my-crd-clean.yaml

# Apply CRD to destination cluster
KUBECONFIG=/path/to/destination/kubeconfig kubectl apply -f my-crd-clean.yaml
```

## Step 3: Synchronizing Custom Resources

To include custom resources in your synchronization, use the `--include-custom-resources` flag:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --include-custom-resources=true \
  --log-level=debug
```

With this flag enabled, DR-Syncer will:
1. Discover custom resources in the source namespace
2. Attempt to synchronize them to the destination namespace
3. Log any issues encountered during the process

## Step 4: Verifying Custom Resource Synchronization

After synchronization, verify that your custom resources were properly synchronized:

```bash
# Get a specific custom resource in source
KUBECONFIG=/path/to/source/kubeconfig kubectl get myresource.example.com -n my-app -o yaml > source-cr.yaml

# Get the same custom resource in destination
KUBECONFIG=/path/to/destination/kubeconfig kubectl get myresource.example.com -n my-app-dr -o yaml > dest-cr.yaml

# Compare (excluding metadata and status)
diff <(yq 'del(.metadata, .status)' source-cr.yaml) <(yq 'del(.metadata, .status)' dest-cr.yaml)
```

## Step 5: Handling Custom Resource Challenges

### Excluding Specific Custom Resources

Sometimes you may want to include most custom resources but exclude specific ones. Use the resource exclusion flags:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --include-custom-resources=true \
  --exclude-resource-types=problematic-resource.example.com \
  --log-level=debug
```

### Status and Cluster-Specific Fields

DR-Syncer automatically strips status fields and certain metadata from custom resources during synchronization. However, some custom resources may have spec fields that contain cluster-specific references.

For these cases, you might need to perform manual adjustments after synchronization:

```bash
# Get the custom resource in destination
KUBECONFIG=/path/to/destination/kubeconfig kubectl get myresource.example.com my-instance -n my-app-dr -o yaml > dest-cr.yaml

# Edit the custom resource to update cluster-specific references
# For example, changing a field like spec.clusterRef from "cluster-a" to "cluster-b"
vi dest-cr.yaml

# Apply the updated custom resource
KUBECONFIG=/path/to/destination/kubeconfig kubectl apply -f dest-cr.yaml
```

### Custom Resource Controllers

Remember that synchronizing the custom resource itself doesn't necessarily mean the associated controller will behave identically in both clusters. Consider:

1. **Controller Version**: Ensure controller versions match or are compatible between clusters
2. **Controller Configuration**: Check if controllers have cluster-specific configuration
3. **Dependencies**: Verify dependencies required by the controller are available

## Custom Resource DR Considerations

When planning DR for applications using custom resources, consider these factors:

### 1. Recovery Priority

Not all custom resources are equally important for recovery. Determine which ones are:

- **Critical**: Required for application functionality
- **Important**: Enhance application capabilities but aren't essential
- **Optional**: Can be recreated or aren't needed in DR

### 2. Data vs. Configuration

Distinguish between:
- **Configuration CRs**: Define how the application should behave
- **Data CRs**: Store application state or data

Data CRs often require more careful handling and validation during DR.

### 3. Stateful Custom Resources

For stateful custom resources, consider:
- Whether they represent persistent storage that needs PVC migration
- If they contain pointers to other resources that might have different identities in DR
- Whether their status needs to be artificially manipulated in DR

## DR Strategies for Common Custom Resources

### Database Operators (e.g., PostgreSQL, MongoDB)

For database operator CRs:
1. Synchronize the database CRs with `--include-custom-resources=true`
2. Consider whether you need additional steps for data replication
3. Verify backup/restore mechanisms specific to the database

```bash
# Example: After synchronizing database CRs, you might need to verify storage class settings
KUBECONFIG=/path/to/destination/kubeconfig kubectl get postgresql my-db -n my-app-dr -o yaml | grep storageClass
```

### Service Mesh (e.g., Istio)

For service mesh CRs:
1. Ensure service mesh is installed in both clusters
2. Synchronize the virtual service, destination rule, and gateway CRs
3. Adjust hostnames or endpoints as needed for the DR environment

### Monitoring and Logging

For monitoring CRs:
1. Synchronize the custom resources
2. Update any endpoint references
3. Verify service discovery mechanisms

## Best Practices

1. **Test Custom Resource Synchronization**: Regularly test that your CRs synchronize correctly
2. **Document CR Dependencies**: Keep track of which controllers and CRDs are needed
3. **Version Control**: Store critical CRs in version control as a backup
4. **Automate Post-Sync Actions**: Script any manual adjustments needed after synchronization
5. **Monitor Controller Health**: Verify that controllers in the DR environment are processing CRs

## Troubleshooting Custom Resource Issues

### Issue: Custom Resource Not Found in Destination

If a custom resource exists in source but not in destination after synchronization:

```bash
# Check if the CRD exists in destination
KUBECONFIG=/path/to/destination/kubeconfig kubectl get crd | grep myresource

# Check for errors in the DR-Syncer CLI output
# Run with debug logging for detailed information
bin/dr-syncer-cli ... --include-custom-resources=true --log-level=debug

# Check events in the destination namespace
KUBECONFIG=/path/to/destination/kubeconfig kubectl get events -n my-app-dr
```

### Issue: Custom Resource Exists But Doesn't Work

If the custom resource exists in the destination but doesn't function correctly:

```bash
# Check the custom resource's status field
KUBECONFIG=/path/to/destination/kubeconfig kubectl get myresource.example.com -n my-app-dr -o jsonpath='{.status}'

# Check controller logs
KUBECONFIG=/path/to/destination/kubeconfig kubectl logs -n controller-namespace -l app=my-controller

# Check for missing references
KUBECONFIG=/path/to/destination/kubeconfig kubectl describe myresource.example.com -n my-app-dr
```

## Next Steps

Now that you're familiar with handling custom resources in DR operations, you might want to explore:

- [PVC Data Migration Techniques](../tutorial-extras/pvc-data-migration.md) - Advanced data migration for stateful applications
- [Automating DR Processes](../tutorial-extras/automating-dr-processes.md) - Create scripts for automated operations

With these skills, you can create comprehensive DR plans that cover both standard Kubernetes resources and your application's custom resources.
