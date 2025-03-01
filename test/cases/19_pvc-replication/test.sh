#!/bin/bash

# Source common functions
source "$(dirname "$0")/../common.sh"

# Test name
TEST_NAME="PVC Replication"
echo "Running test: $TEST_NAME"

# Setup test environment
setup_test_environment

# Apply the test resources
echo "Applying test resources..."
kubectl --context="$CONTROLLER_CONTEXT" apply -f "$TEST_DIR/controller.yaml"
kubectl --context="$REMOTE_CONTEXT" apply -f "$TEST_DIR/remote.yaml"

# Wait for resources to be created
echo "Waiting for resources to be created..."
sleep 5

# Check if PVCs are created in both clusters
echo "Checking if PVCs are created in both clusters..."
kubectl --context="$CONTROLLER_CONTEXT" get pvc -n test-pvc-replication
kubectl --context="$REMOTE_CONTEXT" get pvc -n test-pvc-replication

# Create a pod to write data to the source PVC
echo "Creating pod to write data to source PVC..."
cat <<EOF | kubectl --context="$CONTROLLER_CONTEXT" apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: source-data-writer
  namespace: test-pvc-replication
spec:
  containers:
  - name: writer
    image: busybox
    command: ["/bin/sh", "-c", "echo 'This is test data for PVC replication' > /data/test-file.txt && sleep 3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: source-pvc
EOF

# Wait for the pod to be ready
echo "Waiting for source data writer pod to be ready..."
kubectl --context="$CONTROLLER_CONTEXT" wait --for=condition=Ready pod/source-data-writer -n test-pvc-replication --timeout=60s

# Create the replication resource
echo "Creating replication resource..."
cat <<EOF | kubectl --context="$CONTROLLER_CONTEXT" apply -f -
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: pvc-replication-test
spec:
  clusterMappingRef:
    name: nyc3-to-sfo3
    namespace: dr-syncer
  sourceNamespace: test-pvc-replication
  destinationNamespace: test-pvc-replication
  replicationMode: Manual
  pvcConfig:
    syncData: true
    dataSyncConfig:
      concurrentSyncs: 1
      timeout: "5m"
EOF

# Wait for the replication to be created
echo "Waiting for replication to be created..."
sleep 5

# Trigger the replication
echo "Triggering PVC data replication..."
kubectl --context="$CONTROLLER_CONTEXT" annotate replication pvc-replication-test dr-syncer.io/trigger-sync="$(date +%s)" --overwrite

# Wait for the replication to complete
echo "Waiting for replication to complete..."
sleep 30

# Create a pod to read data from the destination PVC
echo "Creating pod to read data from destination PVC..."
cat <<EOF | kubectl --context="$REMOTE_CONTEXT" apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: dest-data-reader
  namespace: test-pvc-replication
spec:
  containers:
  - name: reader
    image: busybox
    command: ["/bin/sh", "-c", "cat /data/test-file.txt && sleep 3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: dest-pvc
EOF

# Wait for the pod to be ready
echo "Waiting for destination data reader pod to be ready..."
kubectl --context="$REMOTE_CONTEXT" wait --for=condition=Ready pod/dest-data-reader -n test-pvc-replication --timeout=60s

# Check if the data was replicated
echo "Checking if data was replicated..."
REPLICATED_DATA=$(kubectl --context="$REMOTE_CONTEXT" logs dest-data-reader -n test-pvc-replication)
echo "Replicated data: $REPLICATED_DATA"

# Verify the data
if [[ "$REPLICATED_DATA" == *"This is test data for PVC replication"* ]]; then
  echo "✅ Test passed: Data was successfully replicated"
  TEST_RESULT=0
else
  echo "❌ Test failed: Data was not replicated correctly"
  TEST_RESULT=1
fi

# Clean up
echo "Cleaning up resources..."
kubectl --context="$CONTROLLER_CONTEXT" delete -f "$TEST_DIR/controller.yaml" --ignore-not-found
kubectl --context="$REMOTE_CONTEXT" delete -f "$TEST_DIR/remote.yaml" --ignore-not-found
kubectl --context="$CONTROLLER_CONTEXT" delete pod/source-data-writer -n test-pvc-replication --ignore-not-found
kubectl --context="$REMOTE_CONTEXT" delete pod/dest-data-reader -n test-pvc-replication --ignore-not-found
kubectl --context="$CONTROLLER_CONTEXT" delete replication pvc-replication-test --ignore-not-found

# Exit with test result
exit $TEST_RESULT
