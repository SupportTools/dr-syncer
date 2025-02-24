#!/bin/bash
set -e

# Source common functions
source ../common.sh

# Test name
TEST_NAME="PVC Sync Agent"
start_test "${TEST_NAME}"

# Apply RemoteCluster
info "Creating RemoteCluster with PVC sync enabled"
kubectl apply -f controller.yaml

# Wait for RemoteCluster to be ready
info "Waiting for RemoteCluster to be ready"
wait_for_condition "test-remote" "Ready" "dr-syncer"

# Verify RemoteCluster status
info "Verifying RemoteCluster status"
rc_status=$(kubectl get remotecluster test-remote -n dr-syncer -o json)
health=$(echo "${rc_status}" | jq -r '.status.health')
pvc_sync_phase=$(echo "${rc_status}" | jq -r '.status.pvcSync.phase')

if [[ "${health}" != "Healthy" ]]; then
    fail "RemoteCluster health is not Healthy: ${health}"
fi

if [[ "${pvc_sync_phase}" != "Running" ]]; then
    fail "PVC sync phase is not Running: ${pvc_sync_phase}"
fi

# Switch to remote cluster context
info "Switching to remote cluster"
REMOTE_CONTEXT="remote"
kubectl config use-context "${REMOTE_CONTEXT}"

# Verify namespace
info "Verifying agent namespace"
if ! kubectl get namespace dr-syncer-agent &>/dev/null; then
    fail "Agent namespace not found"
fi

# Verify RBAC resources
info "Verifying RBAC resources"
if ! kubectl get serviceaccount pvc-syncer-agent -n dr-syncer-agent &>/dev/null; then
    fail "ServiceAccount not found"
fi

if ! kubectl get clusterrole pvc-syncer-agent &>/dev/null; then
    fail "ClusterRole not found"
fi

if ! kubectl get clusterrolebinding pvc-syncer-agent &>/dev/null; then
    fail "ClusterRoleBinding not found"
fi

# Verify DaemonSet
info "Verifying DaemonSet"
if ! kubectl get daemonset pvc-syncer-agent -n dr-syncer-agent &>/dev/null; then
    fail "DaemonSet not found"
fi

# Wait for DaemonSet to be ready
info "Waiting for DaemonSet to be ready"
kubectl rollout status daemonset/pvc-syncer-agent -n dr-syncer-agent --timeout=2m

# Verify SSH keys secret
info "Verifying SSH keys secret"
if ! kubectl get secret pvc-syncer-agent-keys -n dr-syncer-agent &>/dev/null; then
    fail "SSH keys secret not found"
fi

# Verify agent pods
info "Verifying agent pods"
node_count=$(kubectl get nodes --no-headers | wc -l)
pod_count=$(kubectl get pods -n dr-syncer-agent --no-headers | grep "pvc-syncer-agent" | grep "Running" | wc -l)

if [[ "${node_count}" != "${pod_count}" ]]; then
    fail "Not all agent pods are running. Expected: ${node_count}, Found: ${pod_count}"
fi

# Verify SSH service
info "Verifying SSH service"
for pod in $(kubectl get pods -n dr-syncer-agent -l app=pvc-syncer-agent -o name); do
    if ! kubectl exec -n dr-syncer-agent "${pod}" -- nc -z localhost 2222; then
        fail "SSH service not accessible in ${pod}"
    fi
done

# Switch back to controller cluster context
info "Switching back to controller cluster"
kubectl config use-context kind-dr-syncer

# Test cleanup
info "Cleaning up test resources"
kubectl delete -f controller.yaml

# Test completion
success "Test completed successfully"
