---
sidebar_position: 3
---

# Performing a DR Cutover

This tutorial guides you through the process of performing a disaster recovery (DR) cutover using the DR-Syncer CLI. A cutover is the process of switching your production workload from the primary environment to the DR environment.

## Overview

During a DR cutover, the DR-Syncer CLI:

1. Synchronizes all resources from source to destination
2. Preserves replica counts by annotating source deployments
3. Scales down deployments in the source namespace to 0 replicas
4. Scales up deployments in the destination namespace to the original replica counts
5. Optionally migrates PVC data if enabled

This effectively shifts your workloads from the source cluster to the DR cluster.

## Prerequisites

Before performing a cutover, ensure you have:

- Completed the [Setting Up Disaster Recovery Environment](setting-up-dr-environment.md) tutorial
- Access to both source and destination clusters with appropriate permissions
- Current backup of critical data (always have a backup before performing DR operations)
- A documented plan for post-cutover validation
- Communication plan for stakeholders

## Pre-Cutover Checklist

Before starting the cutover process, verify:

1. **DR Environment Readiness**:
   ```bash
   KUBECONFIG=/path/to/destination/kubeconfig kubectl get all -n my-app-dr
   ```

2. **Resource Sync Status**:
   ```bash
   bin/dr-syncer-cli \
     --source-kubeconfig=/path/to/source/kubeconfig \
     --dest-kubeconfig=/path/to/destination/kubeconfig \
     --source-namespace=my-app \
     --dest-namespace=my-app-dr \
     --mode=Stage \
     --log-level=debug
   ```

3. **Current Replica Counts (to verify later)**:
   ```bash
   KUBECONFIG=/path/to/source/kubeconfig kubectl get deployments -n my-app -o=jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.replicas}{"\n"}{end}'
   ```

4. **PVC Status**:
   ```bash
   KUBECONFIG=/path/to/source/kubeconfig kubectl get pvc -n my-app
   KUBECONFIG=/path/to/destination/kubeconfig kubectl get pvc -n my-app-dr
   ```

## Step 1: Communicate the Cutover

Before proceeding, inform all stakeholders according to your communication plan. Set clear expectations about potential service disruption during the cutover process.

## Step 2: Perform the Cutover

To initiate the cutover, run the DR-Syncer CLI with the Cutover mode:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Cutover \
  --log-level=debug
```

### Data Migration During Cutover

If your application uses PVCs and you need to migrate data as part of the cutover:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Cutover \
  --migrate-pvc-data=true \
  --log-level=debug
```

This is particularly important for applications with constantly changing data, as it ensures the most recent data is transferred during the cutover.

## Step 3: Verify the Cutover

After the cutover completes, verify that:

### 1. Source Deployments Are Scaled Down

```bash
KUBECONFIG=/path/to/source/kubeconfig kubectl get deployments -n my-app -o=jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.replicas}{"\n"}{end}'
```

All deployments should show 0 replicas.

### 2. Destination Deployments Are Scaled Up

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl get deployments -n my-app-dr -o=jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.replicas}{"\n"}{end}'
```

Deployments should show their original replica counts from the source.

### 3. Pods Are Running in Destination

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl get pods -n my-app-dr
```

Verify that pods are in Running state.

### 4. Services Are Available

Test service endpoints to verify they are functioning correctly:

```bash
# Get service endpoints
KUBECONFIG=/path/to/destination/kubeconfig kubectl get svc -n my-app-dr

# For services with NodePort or LoadBalancer type
KUBECONFIG=/path/to/destination/kubeconfig kubectl get svc my-service -n my-app-dr -o=jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

Use appropriate tools (curl, browser, etc.) to verify services are responding.

## Step 4: Post-Cutover Activities

Once the cutover is complete and verified, conduct these post-cutover activities:

### 1. Update External Dependencies

Update any external systems that interact with your application to point to the new environment:

- DNS records
- Load balancers
- API gateways
- Monitoring systems
- Backup systems

### 2. Verify External Access

Confirm that external users and systems can access your application in its new location.

### 3. Monitor Application Health

Closely monitor the application in the DR environment for any issues:

```bash
# Check logs
KUBECONFIG=/path/to/destination/kubeconfig kubectl logs deployment/my-deployment -n my-app-dr

# Check events
KUBECONFIG=/path/to/destination/kubeconfig kubectl get events -n my-app-dr
```

### 4. Document the Cutover

Record details about the cutover process:

- Timestamp of cutover execution
- Any issues encountered and their resolutions
- Verification results
- Current state of both environments

## Common Issues and Troubleshooting

### Issue: Deployments Not Scaling Up in Destination

If deployments don't scale up in the destination cluster:

```bash
# Check for deployment events
KUBECONFIG=/path/to/destination/kubeconfig kubectl describe deployment my-deployment -n my-app-dr

# Check for available resources
KUBECONFIG=/path/to/destination/kubeconfig kubectl get nodes
KUBECONFIG=/path/to/destination/kubeconfig kubectl describe nodes | grep -A 5 "Allocated resources"
```

Common causes include resource constraints or missing dependencies.

### Issue: Application Not Functioning Correctly

If the application isn't functioning properly after cutover:

```bash
# Check application logs
KUBECONFIG=/path/to/destination/kubeconfig kubectl logs deployment/my-deployment -n my-app-dr

# Check ConfigMaps and Secrets are correct
KUBECONFIG=/path/to/destination/kubeconfig kubectl get configmap my-config -n my-app-dr -o yaml
```

You may need to adjust configuration specific to the DR environment.

### Issue: PVC Data Not Available

If PVC data migration issues occur:

```bash
# Check PVC status
KUBECONFIG=/path/to/destination/kubeconfig kubectl get pvc -n my-app-dr

# Check for PV binding
KUBECONFIG=/path/to/destination/kubeconfig kubectl get pv

# Check pod access to PVCs
KUBECONFIG=/path/to/destination/kubeconfig kubectl describe pod my-pod -n my-app-dr | grep -A 5 "Volumes"
```

## Rollback Procedure

If the cutover doesn't go as planned and you need to roll back:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Failback
```

This will:
1. Scale down deployments in the destination namespace to 0 replicas
2. Scale up deployments in the source namespace to their original replica counts

For more details on the failback process, see the [Failback Operations](failback-operations.md) tutorial.

## Best Practices

1. **Practice First**: Perform test cutovers before an actual disaster to validate your process
2. **Monitor Closely**: Keep a close eye on the system during and after cutover
3. **Document Everything**: Keep detailed records of the cutover process
4. **Communicate Clearly**: Ensure all stakeholders are informed throughout the process
5. **Have a Rollback Plan**: Always be prepared to roll back if necessary
6. **Validate Thoroughly**: Verify all aspects of application functionality after cutover

## Next Steps

After completing a successful cutover, you may want to:

- [Learn about Failback Operations](failback-operations.md) - How to return to your primary environment
- [Working with Custom Resources](custom-resources.md) - Handle application-specific resources
- [Automating DR Processes](../tutorial-extras/automating-dr-processes.md) - Create scripts for automated operations

Remember that a DR cutover is a significant operation. With proper planning, testing, and execution using DR-Syncer CLI, you can minimize disruption and ensure business continuity.
