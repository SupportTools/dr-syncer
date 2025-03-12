---
sidebar_position: 6
---

# Troubleshooting

This guide provides troubleshooting steps for common issues encountered when working with DR-Syncer.

## Diagnosing Issues

When troubleshooting DR-Syncer, start by gathering information about the current state:

### Check Controller Status

```bash
# Check if the controller pods are running
kubectl get pods -n dr-syncer

# View controller logs
kubectl logs -n dr-syncer -l app=dr-syncer -c controller --tail=100
```

### Check Resource Status

```bash
# Check RemoteCluster status
kubectl get remoteclusters -A
kubectl describe remotecluster <name> -n <namespace>

# Check Replication status
kubectl get replications -A
kubectl describe replication <name> -n <namespace>
```

### Check Agent Status

If you're using PVC synchronization, check the agent status:

```bash
# Check agent pods
kubectl get pods -n <namespace> -l app=dr-syncer-agent

# View agent logs
kubectl logs -n <namespace> -l app=dr-syncer-agent -c agent --tail=100
```

## Common Issues

### Controller Not Starting

**Symptoms:**
- Controller pod status is Error or CrashLoopBackOff
- Controller logs show startup errors

**Possible Causes and Solutions:**

1. **Missing RBAC permissions:**
   - Check for RBAC errors in logs
   - Ensure the controller ServiceAccount has the necessary permissions
   ```bash
   kubectl describe clusterrole dr-syncer-manager-role
   kubectl describe clusterrolebinding dr-syncer-manager-rolebinding
   ```
   - Ensure permissions include all resource types being synchronized

2. **Configuration errors:**
   - Check for configuration validation errors in logs
   - Verify Helm values are correctly formatted
   - Ensure CRDs are properly installed:
   ```bash
   kubectl get crds | grep dr-syncer.io
   ```

3. **Resource constraints:**
   - Check if the controller is hitting resource limits
   - Increase CPU/memory limits if necessary
   ```bash
   kubectl describe pod <controller-pod> -n dr-syncer
   ```

### Remote Cluster Connection Issues

**Symptoms:**
- RemoteCluster status shows "ConnectionFailed"
- Controller logs contain "unable to connect to remote cluster"

**Possible Causes and Solutions:**

1. **Invalid kubeconfig:**
   - Verify the kubeconfig secret exists and is properly formatted
   ```bash
   kubectl get secret <kubeconfig-secret> -n <namespace>
   ```
   - Check if the kubeconfig has expired credentials or tokens
   - Ensure the kubeconfig has the correct API server address

2. **Network connectivity:**
   - Ensure the controller can reach the remote cluster's API server
   - Check for network policies that might block connections
   - Verify DNS resolution works correctly

3. **Authentication issues:**
   - Check if service accounts or tokens used in kubeconfig are valid
   - Verify certificate expiration dates
   - Ensure proper authentication configuration

### Replication Failures

**Symptoms:**
- Replication status shows "SyncFailed"
- Resources are not appearing in the destination cluster
- Controller logs show errors during synchronization

**Possible Causes and Solutions:**

1. **Resource conflicts:**
   - Check for resource version conflicts in logs
   - Verify resources don't already exist in the destination cluster
   - Look for validation errors in logs:
   ```bash
   kubectl logs -n dr-syncer -l app=dr-syncer -c controller | grep "conflict\|validation"
   ```

2. **Permission issues:**
   - Ensure the controller has permissions to read resources in source cluster
   - Verify permissions to create/update resources in destination cluster
   - Check for specific RBAC errors in logs

3. **Resource filtering problems:**
   - Verify the resource types configured in Replication are correct
   - Check if excluded resources are being processed incorrectly
   - Ensure label selectors are working as expected

### PVC Synchronization Issues

**Symptoms:**
- PVC data is not being synchronized
- Rsync operations fail
- Agent logs show connection or mounting errors

**Possible Causes and Solutions:**

1. **SSH connectivity:**
   - Check if SSH keys are properly generated and distributed
   ```bash
   kubectl describe remotecluster <name> -n <namespace> | grep SSH
   ```
   - Verify SSH connections between controller and agent pods
   - Check network policies that might block SSH traffic

2. **Volume mounting:**
   - Ensure the agent can mount the PVCs
   - Check for permission issues in volume paths
   - Verify node affinity rules are placing pods correctly

3. **Rsync failures:**
   - Look for rsync-specific errors in agent logs
   ```bash
   kubectl logs -n <namespace> -l app=dr-syncer-agent -c agent | grep rsync
   ```
   - Check storage available in source and destination PVCs
   - Verify storage class compatibility

### Performance Issues

**Symptoms:**
- Slow synchronization operations
- High CPU/memory usage
- Frequent reconciliation without progress

**Possible Causes and Solutions:**

1. **Resource limitations:**
   - Check CPU and memory usage of controller pods
   - Increase resource limits if necessary
   - Consider tuning reconciliation intervals

2. **Large volume of resources:**
   - Use more specific resource type filtering
   - Break down large namespaces into multiple Replication resources
   - Implement label-based filtering to reduce workload

3. **Network bottlenecks:**
   - For PVC sync, check network bandwidth between clusters
   - Consider scheduling PVC syncs during off-peak hours
   - Use incremental synchronization when possible

### CRD and API Errors

**Symptoms:**
- "no matches for kind" errors
- Validation errors when creating resources
- Version compatibility issues

**Possible Causes and Solutions:**

1. **Missing or outdated CRDs:**
   - Check if all required CRDs are installed:
   ```bash
   kubectl get crds | grep dr-syncer.io
   ```
   - Update CRDs if using a newer controller version:
   ```bash
   kubectl apply -f <path-to-crds>
   ```

2. **Schema validation errors:**
   - Verify your CR specs match the CRD schema
   - Look for specific validation errors in logs
   - Check for proper field types and required fields

3. **Version skew:**
   - Ensure controller and CRD versions are compatible
   - Check for deprecated fields in your configurations
   - Update configurations to match current API schema

## Recovery Steps

### Resetting a Failed Replication

If a Replication gets stuck in a failed state:

1. Patch the Replication status to reset its phase:
   ```bash
   kubectl patch replication <name> -n <namespace> --type=merge --patch '{"status":{"phase":"Pending"}}'
   ```

2. Alternatively, delete and recreate the Replication:
   ```bash
   kubectl delete replication <name> -n <namespace>
   kubectl apply -f <replication-yaml>
   ```

### Redeploying the Controller

If the controller needs to be reinstalled:

1. Use Helm to uninstall the current release:
   ```bash
   helm uninstall dr-syncer -n dr-syncer
   ```

2. Reinstall with the correct configuration:
   ```bash
   helm install dr-syncer dr-syncer/dr-syncer \
     --namespace dr-syncer \
     --create-namespace \
     --values values.yaml
   ```

3. Verify the controller starts correctly:
   ```bash
   kubectl get pods -n dr-syncer
   ```

### Regenerating SSH Keys

If PVC synchronization SSH keys need to be regenerated:

1. Delete the SSH key secret:
   ```bash
   kubectl delete secret <ssh-key-secret> -n <namespace>
   ```

2. Trigger a reconciliation of the RemoteCluster:
   ```bash
   kubectl annotate remotecluster <name> -n <namespace> dr-syncer.io/reconcile=$(date +%s) --overwrite
   ```
   
3. The controller will automatically regenerate SSH keys and update the agent.

## Advanced Troubleshooting

### Debug Mode

For more detailed logging:

1. Update the controller deployment to use debug log level:
   ```bash
   kubectl set env deployment/dr-syncer-controller -n dr-syncer LOG_LEVEL=debug
   ```

2. Or, when using Helm, set the log level value:
   ```bash
   helm upgrade dr-syncer dr-syncer/dr-syncer \
     --namespace dr-syncer \
     --set controller.logLevel=debug
   ```

### Manual Status Inspection

To understand the internal state of a resource:

1. Dump the full resource with status:
   ```bash
   kubectl get replication <name> -n <namespace> -o yaml > replication-status.yaml
   ```

2. Examine status conditions, timestamps, and phase information to track progression.

### Network Troubleshooting

To debug network connectivity issues:

1. Deploy a network debugging pod:
   ```bash
   kubectl run netshoot --rm -it --image nicolaka/netshoot -- /bin/bash
   ```

2. Test connectivity to remote cluster API server:
   ```bash
   curl -k https://<api-server-address>:<port>/healthz
   ```

3. Check DNS resolution:
   ```bash
   nslookup <api-server-hostname>
   ```

## Getting Support

If you continue to experience issues:

1. Check the GitHub repository for known issues: https://github.com/supporttools/dr-syncer/issues

2. Gather diagnostic information:
   ```bash
   # Collect controller logs
   kubectl logs -n dr-syncer -l app=dr-syncer -c controller --tail=1000 > controller-logs.txt
   
   # Export resource definitions
   kubectl get remoteclusters -A -o yaml > remoteclusters.yaml
   kubectl get replications -A -o yaml > replications.yaml
   
   # Check CRD versions
   kubectl get crds -o yaml | grep dr-syncer.io > crd-versions.txt
   ```

3. File an issue with complete details, steps to reproduce, and log outputs.
