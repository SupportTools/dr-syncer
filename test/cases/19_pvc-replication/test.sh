#!/bin/bash

# Source common functions
source ../common.sh

# Test case for PVC replication with the rsync deployment controller
echo "Testing PVC replication with rsync deployment"

# Set up environment variables for test case
TEST_NAME="pvc-replication"
SOURCE_NAMESPACE="pvc-source-ns"
DEST_NAMESPACE="pvc-dest-ns"
PVC_NAME="test-pvc"

# Create test environment
function setup_environment() {
  echo "Setting up test environment..."
  
  # Create namespaces in both clusters
  kubectl --context="${CTX_CONTROLLER}" create namespace ${SOURCE_NAMESPACE} || true
  kubectl --context="${CTX_REMOTE}" create namespace ${DEST_NAMESPACE} || true
  
  # Create a test PVC in the source namespace
  cat <<EOF | kubectl --context="${CTX_CONTROLLER}" apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${PVC_NAME}
  namespace: ${SOURCE_NAMESPACE}
  labels:
    app: test-app
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF

  # Create a test PVC in the destination namespace
  cat <<EOF | kubectl --context="${CTX_REMOTE}" apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${PVC_NAME}
  namespace: ${DEST_NAMESPACE}
  labels:
    app: test-app
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF

  # Create a pod that mounts the source PVC
  cat <<EOF | kubectl --context="${CTX_CONTROLLER}" apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: source-pod
  namespace: ${SOURCE_NAMESPACE}
spec:
  containers:
  - name: busybox
    image: busybox
    command: ["/bin/sh", "-c", "while true; do sleep 3600; done"]
    volumeMounts:
    - name: test-data
      mountPath: /data
  volumes:
  - name: test-data
    persistentVolumeClaim:
      claimName: ${PVC_NAME}
EOF

  # Wait for pod to be running
  echo "Waiting for source pod to be running..."
  kubectl --context="${CTX_CONTROLLER}" wait --for=condition=Ready pod/source-pod -n ${SOURCE_NAMESPACE} --timeout=60s
  
  # Create some test data on the source PVC
  kubectl --context="${CTX_CONTROLLER}" exec -n ${SOURCE_NAMESPACE} source-pod -- sh -c "echo 'Test data for replication' > /data/test-file.txt"
  kubectl --context="${CTX_CONTROLLER}" exec -n ${SOURCE_NAMESPACE} source-pod -- sh -c "mkdir -p /data/subdir && echo 'Subdirectory test data' > /data/subdir/another-file.txt"
  
  echo "Test environment setup complete."
}

# Apply the NamespaceMapping CR
function apply_namespace_mapping() {
  echo "Applying NamespaceMapping CR..."
  
  cat <<EOF | kubectl --context="${CTX_REMOTE}" apply -f -
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: test-pvc-replication
spec:
  sourceNamespace: ${SOURCE_NAMESPACE}
  destinationNamespace: ${DEST_NAMESPACE}
  replicationMode: Manual
  pvcConfig:
    enabled: true
EOF

  echo "NamespaceMapping CR applied."
}

# Test the replication status
function verify_replication() {
  echo "Verifying replication status..."
  
  # Wait for a sync pod to appear (may take a moment)
  echo "Waiting for sync process to start..."
  sleep 10
  
  # Look for replication in logs
  kubectl --context="${CTX_REMOTE}" logs -l app.kubernetes.io/name=dr-syncer -n dr-syncer | grep -i 'PVC replication' || true
  
  # Trigger sync manually if needed
  kubectl --context="${CTX_REMOTE}" annotate namespacemapping test-pvc-replication dr-syncer.io/trigger-sync=true
  
  # Wait for sync to complete (timeout after 2 minutes)
  echo "Waiting for sync to complete (timeout 2m)..."
  local start_time=$(date +%s)
  local timeout=120
  
  while true; do
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    
    if [ $elapsed -gt $timeout ]; then
      echo "Timeout waiting for sync completion"
      break
    fi
    
    # Check for successful sync in logs
    if kubectl --context="${CTX_REMOTE}" logs -l app.kubernetes.io/name=dr-syncer -n dr-syncer | grep -i 'PVC replication completed successfully'; then
      echo "Sync completed successfully"
      break
    fi
    
    sleep 5
  done
  
  # Create a verification pod to check data
  cat <<EOF | kubectl --context="${CTX_REMOTE}" apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: verify-pod
  namespace: ${DEST_NAMESPACE}
spec:
  containers:
  - name: busybox
    image: busybox
    command: ["/bin/sh", "-c", "while true; do sleep 3600; done"]
    volumeMounts:
    - name: test-data
      mountPath: /data
  volumes:
  - name: test-data
    persistentVolumeClaim:
      claimName: ${PVC_NAME}
EOF
  
  # Wait for verification pod to be running
  echo "Waiting for verification pod to be running..."
  kubectl --context="${CTX_REMOTE}" wait --for=condition=Ready pod/verify-pod -n ${DEST_NAMESPACE} --timeout=60s || true
  
  # Check if data was replicated
  local test_data=$(kubectl --context="${CTX_REMOTE}" exec -n ${DEST_NAMESPACE} verify-pod -- cat /data/test-file.txt 2>/dev/null || echo "File not found")
  local subdir_data=$(kubectl --context="${CTX_REMOTE}" exec -n ${DEST_NAMESPACE} verify-pod -- cat /data/subdir/another-file.txt 2>/dev/null || echo "File not found")
  
  echo "Results of verification:"
  echo "Main file content: ${test_data}"
  echo "Subdir file content: ${subdir_data}"
  
  if [[ "${test_data}" == *"Test data for replication"* ]] && [[ "${subdir_data}" == *"Subdirectory test data"* ]]; then
    echo "✅ PVC replication test passed!"
    return 0
  else
    echo "❌ PVC replication test failed!"
    return 1
  fi
}

# Clean up test resources
function cleanup() {
  echo "Cleaning up test resources..."
  
  # Delete verify pod
  kubectl --context="${CTX_REMOTE}" delete pod verify-pod -n ${DEST_NAMESPACE} --force --grace-period=0 2>/dev/null || true
  
  # Delete source pod
  kubectl --context="${CTX_CONTROLLER}" delete pod source-pod -n ${SOURCE_NAMESPACE} --force --grace-period=0 2>/dev/null || true
  
  # Delete namespace mapping
  kubectl --context="${CTX_REMOTE}" delete namespacemapping test-pvc-replication 2>/dev/null || true
  
  # Delete PVCs
  kubectl --context="${CTX_CONTROLLER}" delete pvc ${PVC_NAME} -n ${SOURCE_NAMESPACE} 2>/dev/null || true
  kubectl --context="${CTX_REMOTE}" delete pvc ${PVC_NAME} -n ${DEST_NAMESPACE} 2>/dev/null || true
  
  # Delete namespaces
  kubectl --context="${CTX_CONTROLLER}" delete namespace ${SOURCE_NAMESPACE} 2>/dev/null || true
  kubectl --context="${CTX_REMOTE}" delete namespace ${DEST_NAMESPACE} 2>/dev/null || true
  
  echo "Cleanup complete."
}

# Run the test
setup_environment
apply_namespace_mapping
result=0
verify_replication || result=$?
cleanup

exit $result
