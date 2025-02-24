# Test Case: PVC Sync Agent

This test case verifies the PVC sync agent functionality:
1. Agent deployment in remote clusters
2. SSH key management
3. RBAC setup
4. Agent status reporting

## Test Steps

1. Create a RemoteCluster with PVC sync enabled
2. Verify agent DaemonSet is created in remote cluster
3. Verify SSH keys are created and mounted
4. Verify RBAC resources are created
5. Verify agent status is reported correctly

## Expected Results

1. RemoteCluster Status:
   - Health: "Healthy"
   - PVCSync Phase: "Running"
   - Agent status shows correct node count

2. Remote Cluster Resources:
   - Namespace: dr-syncer-agent
   - DaemonSet: pvc-syncer-agent
   - ServiceAccount: pvc-syncer-agent
   - ClusterRole: pvc-syncer-agent
   - ClusterRoleBinding: pvc-syncer-agent
   - Secret: pvc-syncer-agent-keys

3. Agent Pods:
   - Running on all nodes
   - SSH service accessible
   - Kubelet path mounted
   - SSH keys mounted
