#!/bin/bash
set -e

# Source common functions
source ../common.sh

# Test name
TEST_NAME="PVC Sync Agent"
start_test "${TEST_NAME}"

# Switch to remote cluster context
info "Switching to remote cluster"
REMOTE_CONTEXT="remote"
kubectl config use-context "${REMOTE_CONTEXT}"

# Create test resources in remote cluster
info "Creating test resources in remote cluster"
kubectl apply -f remote.yaml

# Wait for pods to be ready
info "Waiting for test pods to be ready"
kubectl wait --for=condition=Ready pod/test-pod-1 -n test-pvc-sync --timeout=2m
kubectl wait --for=condition=Ready pod/test-pod-2 -n test-pvc-sync --timeout=2m

# Switch to controller cluster context
info "Switching to controller cluster"
kubectl config use-context kind-dr-syncer

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
kubectl config use-context "${REMOTE_CONTEXT}"

# Verify agent deployment
info "Verifying agent deployment"
if ! kubectl get namespace dr-syncer-agent &>/dev/null; then
    fail "Agent namespace not found"
fi

if ! kubectl get serviceaccount pvc-syncer-agent -n dr-syncer-agent &>/dev/null; then
    fail "ServiceAccount not found"
fi

if ! kubectl get clusterrole pvc-syncer-agent &>/dev/null; then
    fail "ClusterRole not found"
fi

if ! kubectl get clusterrolebinding pvc-syncer-agent &>/dev/null; then
    fail "ClusterRoleBinding not found"
fi

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

# Switch to DR cluster context
info "Switching to DR cluster"
kubectl config use-context kind-dr-syncer-dr

# Wait for PVC sync
info "Waiting for PVC sync (30s)"
sleep 30

# Verify source pod scheduling
info "Verifying source pod scheduling"
pod1_node=$(kubectl get pod test-pod-1 -n test-pvc-sync -o jsonpath='{.spec.nodeName}')
pod2_node=$(kubectl get pod test-pod-2 -n test-pvc-sync -o jsonpath='{.spec.nodeName}')

if [[ "${pod1_node}" != "kind-dr-syncer-prod-worker" ]]; then
    fail "Pod 1 not scheduled on expected node. Expected: kind-dr-syncer-prod-worker, Got: ${pod1_node}"
fi

if [[ "${pod2_node}" != "kind-dr-syncer-prod-worker" ]]; then
    fail "Pod 2 not scheduled on expected node. Expected: kind-dr-syncer-prod-worker, Got: ${pod2_node}"
fi

# Switch to DR cluster context
info "Switching to DR cluster"
kubectl config use-context kind-dr-syncer-dr

# Wait for sync pods to be created
info "Waiting for sync pods to be created"
kubectl wait --for=condition=Ready pod/sync-test-pvc-1 -n test-pvc-sync --timeout=2m
kubectl wait --for=condition=Ready pod/sync-test-pvc-2 -n test-pvc-sync --timeout=2m

# Verify PVC creation and configuration
info "Verifying PVC configuration"

# Verify PVC 1 (ReadWriteOnce)
pvc1_access_mode=$(kubectl get pvc test-pvc-1 -n test-pvc-sync -o jsonpath='{.spec.accessModes[0]}')
pvc1_size=$(kubectl get pvc test-pvc-1 -n test-pvc-sync -o jsonpath='{.spec.resources.requests.storage}')
pvc1_class=$(kubectl get pvc test-pvc-1 -n test-pvc-sync -o jsonpath='{.spec.storageClassName}')

if [[ "${pvc1_access_mode}" != "ReadWriteOnce" ]]; then
    fail "PVC 1 has incorrect access mode. Expected: ReadWriteOnce, Got: ${pvc1_access_mode}"
fi

if [[ "${pvc1_size}" != "1Gi" ]]; then
    fail "PVC 1 has incorrect size. Expected: 1Gi, Got: ${pvc1_size}"
fi

if [[ "${pvc1_class}" != "standard" ]]; then
    fail "PVC 1 has incorrect storage class. Expected: standard, Got: ${pvc1_class}"
fi

# Verify PVC 2 (ReadWriteMany)
pvc2_access_mode=$(kubectl get pvc test-pvc-2 -n test-pvc-sync -o jsonpath='{.spec.accessModes[0]}')
pvc2_size=$(kubectl get pvc test-pvc-2 -n test-pvc-sync -o jsonpath='{.spec.resources.requests.storage}')
pvc2_class=$(kubectl get pvc test-pvc-2 -n test-pvc-sync -o jsonpath='{.spec.storageClassName}')

if [[ "${pvc2_access_mode}" != "ReadWriteMany" ]]; then
    fail "PVC 2 has incorrect access mode. Expected: ReadWriteMany, Got: ${pvc2_access_mode}"
fi

if [[ "${pvc2_size}" != "2Gi" ]]; then
    fail "PVC 2 has incorrect size. Expected: 2Gi, Got: ${pvc2_size}"
fi

if [[ "${pvc2_class}" != "standard" ]]; then
    fail "PVC 2 has incorrect storage class. Expected: standard, Got: ${pvc2_class}"
fi

# Verify sync pod scheduling
info "Verifying sync pod scheduling"
sync1_node=$(kubectl get pod sync-test-pvc-1 -n test-pvc-sync -o jsonpath='{.spec.nodeName}')
sync2_node=$(kubectl get pod sync-test-pvc-2 -n test-pvc-sync -o jsonpath='{.spec.nodeName}')

# RWO PVC should be on the matching node
if [[ "${sync1_node}" != "kind-dr-syncer-dr-worker" ]]; then
    fail "Sync pod 1 (RWO) not scheduled on expected node. Expected: kind-dr-syncer-dr-worker, Got: ${sync1_node}"
fi

# RWX PVC can be on any node
if [[ -z "${sync2_node}" ]]; then
    fail "Sync pod 2 (RWX) not scheduled on any node"
fi

# Verify PVC data through sync pods
info "Verifying PVC data through sync pods"
sync1_data=$(kubectl exec sync-test-pvc-1 -n test-pvc-sync -- cat /data/test.txt)
sync2_data=$(kubectl exec sync-test-pvc-2 -n test-pvc-sync -- cat /data/test.txt)

if [[ "${sync1_data}" != "Test data 1" ]]; then
    fail "PVC 1 data not synced correctly. Expected: 'Test data 1', Got: '${sync1_data}'"
fi

if [[ "${sync2_data}" != "Test data 2" ]]; then
    fail "PVC 2 data not synced correctly. Expected: 'Test data 2', Got: '${sync2_data}'"
fi

# Wait for sync pods to be deleted
info "Waiting for sync pods to be deleted"
kubectl wait --for=delete pod/sync-test-pvc-1 -n test-pvc-sync --timeout=2m
kubectl wait --for=delete pod/sync-test-pvc-2 -n test-pvc-sync --timeout=2m

# Switch back to controller cluster context
info "Switching back to controller cluster"
kubectl config use-context kind-dr-syncer

# Test cleanup
info "Cleaning up test resources"
kubectl delete -f controller.yaml

# Switch to remote cluster and cleanup
info "Cleaning up remote cluster resources"
kubectl config use-context "${REMOTE_CONTEXT}"
kubectl delete -f remote.yaml

# Test completion
success "Test completed successfully"
