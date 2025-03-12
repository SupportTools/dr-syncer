---
sidebar_position: 4
---

# Failback Operations

This tutorial guides you through the process of performing a failback operation after a disaster recovery (DR) cutover. Failback is the process of returning workloads from the DR environment back to the original source environment once the primary system is operational again.

## Overview

During a failback, the DR-Syncer CLI:

1. Optionally migrates PVC data from destination back to source (if enabled)
2. Scales down deployments in the destination namespace to 0 replicas
3. Scales up deployments in the source namespace to their original replica counts

This effectively returns your workloads to their original environment in a controlled manner.

## Prerequisites

Before performing a failback, ensure you have:

- Previously executed a DR cutover using the [Performing a DR Cutover](performing-dr-cutover.md) tutorial
- Verified that the source environment is ready to resume operations
- Access to both source and destination clusters with appropriate permissions
- Current backup of data in the DR environment (always back up before failback operations)
- A documented plan for post-failback validation
- Communication plan for stakeholders

## Planning Your Failback

### When to Failback

Consider failback when:
- The original issue affecting the source environment has been resolved
- The source environment has been tested and validated
- You're within a suitable maintenance window
- Your recovery time objective (RTO) can be met with the failback process
- Business requirements necessitate returning to the original environment

### Failback Strategy

There are two primary failback strategies:

1. **Cold Failback**: Complete shutdown of services in the DR environment before starting in the source environment (sequential, minimal data loss risk but longer downtime)
2. **Warm Failback**: Gradual transition of workload with both environments running during the transition (parallel, shorter downtime but higher complexity)

DR-Syncer CLI uses the cold failback approach by default, as it's more straightforward and has less risk of data conflicts.

## Pre-Failback Checklist

Before initiating failback, verify:

1. **Source Environment Readiness**:
   ```bash
   # Verify the source environment is accessible and healthy
   KUBECONFIG=/path/to/source/kubeconfig kubectl get nodes
   KUBECONFIG=/path/to/source/kubeconfig kubectl get ns
   ```

2. **Current State of Destination**:
   ```bash
   # Check current deployments and their replica counts (for verification later)
   KUBECONFIG=/path/to/destination/kubeconfig kubectl get deployments -n my-app-dr -o=jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.replicas}{"\n"}{end}'
   ```

3. **PVC Status** (if applicable):
   ```bash
   KUBECONFIG=/path/to/source/kubeconfig kubectl get pvc -n my-app
   KUBECONFIG=/path/to/destination/kubeconfig kubectl get pvc -n my-app-dr
   ```

## Step 1: Communicate the Failback

Before proceeding, inform all stakeholders according to your communication plan. Set clear expectations about potential service disruption during the failback process.

## Step 2: Perform the Failback

To initiate the failback, run the DR-Syncer CLI with the Failback mode:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Failback \
  --log-level=debug
```

### Data Migration During Failback

If your application uses PVCs and you need to migrate data back to the source as part of the failback:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Failback \
  --reverse-migrate-pvc-data=true \
  --log-level=debug
```

For large volumes of data, you may want to add custom options for the PVC migration:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Failback \
  --reverse-migrate-pvc-data=true \
  --pv-migrate-flags="--lbsvc-timeout 30m --strategy rsync" \
  --log-level=debug
```

## Step 3: Verify the Failback

After the failback completes, verify that:

### 1. Destination Deployments Are Scaled Down

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl get deployments -n my-app-dr -o=jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.replicas}{"\n"}{end}'
```

All deployments should show 0 replicas.

### 2. Source Deployments Are Scaled Up

```bash
KUBECONFIG=/path/to/source/kubeconfig kubectl get deployments -n my-app -o=jsonpath='{range .items[*]}{.metadata.name}{": "}{.spec.replicas}{"\n"}{end}'
```

Deployments should show their original replica counts.

### 3. Pods Are Running in Source

```bash
KUBECONFIG=/path/to/source/kubeconfig kubectl get pods -n my-app
```

Verify that pods are in Running state.

### 4. Services Are Available

Test service endpoints to verify they are functioning correctly:

```bash
# Get service endpoints
KUBECONFIG=/path/to/source/kubeconfig kubectl get svc -n my-app

# For services with NodePort or LoadBalancer type
KUBECONFIG=/path/to/source/kubeconfig kubectl get svc my-service -n my-app -o=jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

Use appropriate tools (curl, browser, etc.) to verify services are responding.

## Step 4: Post-Failback Activities

Once the failback is complete and verified, conduct these post-failback activities:

### 1. Update External Dependencies

Update any external systems that were redirected during the cutover to point back to the original environment:

- DNS records
- Load balancers
- API gateways
- Monitoring systems
- Backup systems

### 2. Verify External Access

Confirm that external users and systems can access your application in its original location.

### 3. Monitor Application Health

Closely monitor the application in the source environment for any issues:

```bash
# Check logs
KUBECONFIG=/path/to/source/kubeconfig kubectl logs deployment/my-deployment -n my-app

# Check events
KUBECONFIG=/path/to/source/kubeconfig kubectl get events -n my-app
```

### 4. Document the Failback

Record details about the failback process:

- Timestamp of failback execution
- Any issues encountered and their resolutions
- Verification results
- Current state of both environments

### 5. Review and Update DR Plan

Use this opportunity to review and improve your DR plan based on the experience:

- What worked well?
- What were the challenges?
- How could the process be improved?
- Are there any automation opportunities?

## Common Issues and Troubleshooting

### Issue: Deployments Not Scaling Up in Source

If deployments don't scale up in the source cluster:

```bash
# Check for deployment events
KUBECONFIG=/path/to/source/kubeconfig kubectl describe deployment my-deployment -n my-app

# Verify annotation with original replica count exists
KUBECONFIG=/path/to/source/kubeconfig kubectl get deployment my-deployment -n my-app -o=jsonpath='{.metadata.annotations.dr-syncer\.io/original-replicas}'

# Check for available resources
KUBECONFIG=/path/to/source/kubeconfig kubectl get nodes
KUBECONFIG=/path/to/source/kubeconfig kubectl describe nodes | grep -A 5 "Allocated resources"
```

### Issue: Application Not Functioning Correctly After Failback

If the application isn't functioning properly after failback:

```bash
# Check application logs
KUBECONFIG=/path/to/source/kubeconfig kubectl logs deployment/my-deployment -n my-app

# Check ConfigMaps and Secrets are correct
KUBECONFIG=/path/to/source/kubeconfig kubectl get configmap my-config -n my-app -o yaml
```

You may need to update configuration specific to the source environment.

### Issue: PVC Data Not Available After Failback

If PVC data migration issues occur:

```bash
# Check PVC status
KUBECONFIG=/path/to/source/kubeconfig kubectl get pvc -n my-app

# Check for PV binding
KUBECONFIG=/path/to/source/kubeconfig kubectl get pv

# Check pod access to PVCs
KUBECONFIG=/path/to/source/kubeconfig kubectl describe pod my-pod -n my-app | grep -A 5 "Volumes"
```

### Issue: Error During PVC Data Migration

If you encounter errors during the reverse PVC migration:

```bash
# Run with extended timeout
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Failback \
  --reverse-migrate-pvc-data=true \
  --pv-migrate-flags="--lbsvc-timeout 30m --no-cleanup-on-failure" \
  --log-level=debug
```

The `--no-cleanup-on-failure` flag helps preserve the migration state for debugging.

## Maintaining the DR Environment

After a successful failback, consider how to maintain your DR environment for future use:

### 1. Return to Regular Sync Operations

Resume regular sync operations to keep your DR environment up-to-date:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage
```

### 2. Schedule Regular DR Tests

Set up a schedule for testing your DR capability, including both cutover and failback procedures.

## Best Practices

1. **Test Your Failback Process**: Practice failbacks in a test environment before performing them in production
2. **Backup Before Failback**: Always create backups before failback operations
3. **Document the Process**: Keep detailed records of your failback procedure
4. **Monitor Closely**: Keep a close eye on systems during and after failback
5. **Communicate Clearly**: Ensure all stakeholders are informed throughout the process
6. **Verify Every Step**: Thoroughly test each component after failback

## Next Steps

After completing a successful failback, you may want to explore:

- [Working with Custom Resources](custom-resources.md) - Handle application-specific resources
- [PVC Data Migration Techniques](../tutorial-extras/pvc-data-migration.md) - Advanced data migration options
- [Automating DR Processes](../tutorial-extras/automating-dr-processes.md) - Create scripts for automated operations

By mastering the failback process, you complete the DR lifecycle and ensure that your organization can effectively respond to and recover from disruptions.
