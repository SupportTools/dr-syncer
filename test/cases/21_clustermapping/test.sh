#!/bin/bash

# Source common functions
source "$(dirname "$0")/../common.sh"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

# Test status tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Function to print test results
print_result() {
    local test_name=$1
    local result=$2
    if [ "$result" = "pass" ]; then
        echo -e "${GREEN}✓ $test_name${NC}"
        ((PASSED_TESTS++))
    else
        echo -e "${RED}✗ $test_name${NC}"
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))
}

# Create the test namespace
create_test_namespace() {
    echo "Creating test namespace..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} create namespace test-clustermapping
    kubectl --kubeconfig ${PROD_KUBECONFIG} create namespace test-clustermapping
    kubectl --kubeconfig ${DR_KUBECONFIG} create namespace test-clustermapping
    print_result "Create test namespace" "pass"
}

# Verify the global ClusterMapping exists
verify_clustermapping_exists() {
    echo "Verifying global ClusterMapping exists..."
    if kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping nyc3-to-sfo3 -n dr-syncer &>/dev/null; then
        echo "Global ClusterMapping 'nyc3-to-sfo3' found"
        print_result "Global ClusterMapping exists" "pass"
        return 0
    else
        echo "ERROR: Global ClusterMapping 'nyc3-to-sfo3' not found"
        print_result "Global ClusterMapping exists" "fail"
        return 1
    fi
}

# Wait for the ClusterMapping to reach the Connected phase
wait_for_clustermapping() {
    echo "Waiting for ClusterMapping to reach Connected phase..."
    for i in {1..30}; do
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping nyc3-to-sfo3 -n dr-syncer -o jsonpath='{.status.phase}' 2>/dev/null)
        if [ "$PHASE" == "Connected" ]; then
            echo "ClusterMapping is now in Connected phase"
            print_result "ClusterMapping connected" "pass"
            return 0
        fi
        echo "Current phase: $PHASE, waiting..."
        sleep 10
    done
    
    echo "ERROR: ClusterMapping did not reach Connected phase within timeout"
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping nyc3-to-sfo3 -n dr-syncer -o yaml
    print_result "ClusterMapping connected" "fail"
    return 1
}

# Verify agent status on remote clusters
verify_agent_status() {
    echo "Verifying agent status on source cluster (dr-syncer-nyc3)..."
    
    # Check source cluster agent status
    SOURCE_AGENT_STATUS=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get pods -n dr-syncer -l app=dr-syncer-agent -o jsonpath='{.items[0].status.phase}' 2>/dev/null)
    if [ "$SOURCE_AGENT_STATUS" != "Running" ]; then
        echo "ERROR: Source cluster agent is not running (status: $SOURCE_AGENT_STATUS)"
        kubectl --kubeconfig ${PROD_KUBECONFIG} get pods -n dr-syncer -l app=dr-syncer-agent -o yaml
        print_result "Source cluster agent status" "fail"
        return 1
    fi
    print_result "Source cluster agent status" "pass"
    
    echo "Verifying agent status on target cluster (dr-syncer-sfo3)..."
    
    # Check target cluster agent status
    TARGET_AGENT_STATUS=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pods -n dr-syncer -l app=dr-syncer-agent -o jsonpath='{.items[0].status.phase}' 2>/dev/null)
    if [ "$TARGET_AGENT_STATUS" != "Running" ]; then
        echo "ERROR: Target cluster agent is not running (status: $TARGET_AGENT_STATUS)"
        kubectl --kubeconfig ${DR_KUBECONFIG} get pods -n dr-syncer -l app=dr-syncer-agent -o yaml
        print_result "Target cluster agent status" "fail"
        return 1
    fi
    print_result "Target cluster agent status" "pass"
    
    return 0
}

# Create test PVCs
create_test_pvcs() {
    echo "Creating test PVCs..."
    
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: test-pvc
  namespace: test-clustermapping
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF
    
    kubectl --kubeconfig ${DR_KUBECONFIG} apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: test-pvc
  namespace: test-clustermapping
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF
    
    # Wait for PVCs to be bound
    echo "Waiting for PVCs to be bound..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} wait --for=condition=bound --timeout=60s pvc/test-pvc -n test-clustermapping
    kubectl --kubeconfig ${DR_KUBECONFIG} wait --for=condition=bound --timeout=60s pvc/test-pvc -n test-clustermapping
    
    print_result "Create and bind PVCs" "pass"
}

# Create and trigger namespace mapping
create_and_trigger_namespacemapping() {
    echo "Creating namespace mapping using global ClusterMapping..."
    
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f - <<EOF
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: test-namespacemapping
  namespace: test-clustermapping
spec:
  replicationMode: Manual
  sourceNamespace: test-clustermapping
  destinationNamespace: test-clustermapping
  resourceTypes:
    - persistentvolumeclaims
  clusterMappingRef:
    name: nyc3-to-sfo3
    namespace: dr-syncer
EOF
    
    # Trigger the namespace mapping
    echo "Triggering PVC replication..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} annotate namespacemapping test-namespacemapping -n test-clustermapping dr-syncer.io/trigger-sync=true --overwrite
    
    print_result "Create and trigger namespace mapping" "pass"
}

# Wait for namespace mapping to complete
wait_for_namespacemapping() {
    echo "Waiting for namespace mapping to complete..."
    for i in {1..30}; do
        STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get namespacemapping test-namespacemapping -n test-clustermapping -o jsonpath='{.status.phase}' 2>/dev/null)
        if [ "$STATUS" == "Completed" ]; then
            echo "Namespace mapping completed successfully"
            print_result "Namespace mapping completion" "pass"
            return 0
        fi
        echo "Current status: $STATUS, waiting..."
        sleep 10
    done
    
    echo "ERROR: Namespace mapping did not complete within timeout"
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get namespacemapping test-namespacemapping -n test-clustermapping -o yaml
    print_result "Namespace mapping completion" "fail"
    return 1
}

# Clean up resources
cleanup_resources() {
    echo "Cleaning up test resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespacemapping test-namespacemapping -n test-clustermapping --wait=false
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete pvc test-pvc -n test-clustermapping --wait=false
    kubectl --kubeconfig ${DR_KUBECONFIG} delete pvc test-pvc -n test-clustermapping --wait=false
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespace test-clustermapping --wait=false
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace test-clustermapping --wait=false
    kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace test-clustermapping --wait=false
    
    print_result "Resource cleanup" "pass"
}

# Main test function
main() {
    echo "Testing ClusterMapping and direct CSI path access for PVC replication..."
    
    # Create test namespace
    create_test_namespace
    
    # Verify global ClusterMapping exists
    verify_clustermapping_exists || exit 1
    
    # Wait for ClusterMapping to be connected
    wait_for_clustermapping || exit 1
    
    # Verify agent status on remote clusters
    verify_agent_status || exit 1
    
    # Create test PVCs
    create_test_pvcs
    
    # Create and trigger namespace mapping
    create_and_trigger_namespacemapping
    
    # Wait for namespace mapping to complete
    wait_for_namespacemapping || exit 1
    
    # Clean up resources
    cleanup_resources
    
    # Print summary
    echo -e "\nTest Summary:"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    
    # Return exit code based on test results
    if [ ${FAILED_TESTS} -eq 0 ]; then
        echo "Test completed successfully"
        exit 0
    else
        echo "Test failed"
        exit 1
    fi
}

# Execute main function
main
