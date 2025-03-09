#!/bin/bash
# Simple test script that won't get stuck

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
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

# Function to clean up existing resources
cleanup_resources() {
    echo "Cleaning up any existing resources..."
    
    # Delete namespaces and all related resources
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case14 --ignore-not-found
    kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case14 --ignore-not-found
    
    # Delete PVs
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete pv test-static-pv-case14 test-local-pv-case14 test-nfs-pv-case14 test-block-pv-case14 --ignore-not-found
    
    # Delete NamespaceMapping
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespacemapping pvc-sync-persistent-volumes -n dr-syncer --ignore-not-found
    
    echo "Waiting for resources to be deleted..."
    sleep 5
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 14 (PVC Volume Name Preservation)..."
    
    # Clean up any existing resources
    cleanup_resources
    
    # Deploy resources in production cluster
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/14_pvc-volume-name-preservation/remote.yaml
    
    # Deploy controller resources
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/14_pvc-volume-name-preservation/controller.yaml
    
    # Check if controller has the right config
    echo "Checking NamespaceMapping configuration for PV sync:"
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get namespacemapping pvc-sync-persistent-volumes -n dr-syncer -o yaml | grep -A5 pvcConfig || echo "No pvcConfig found!"
    
    # Wait for initial replication
    echo "Waiting for initial replication (max 30 seconds)..."
    max_attempts=6
    found_namespace=false
    
    for i in $(seq 1 $max_attempts); do
        echo "Checking (attempt $i/$max_attempts)..."
        
        # Check if namespace exists in DR
        if kubectl --kubeconfig ${DR_KUBECONFIG} get namespace dr-sync-test-case14 > /dev/null 2>&1; then
            found_namespace=true
            echo "Namespace found in DR cluster. Continuing with tests..."
            break
        fi
        
        sleep 5
    done
    
    if ! $found_namespace; then
        echo "ERROR: Namespace not created in DR cluster within timeout"
        print_result "Sync test" "fail"
        exit 1
    fi
    
    # Wait a bit more to allow PVCs to be created
    sleep 10
    
    # Check if PVCs are synced and have volumeName preserved
    echo "Checking if PVCs have volumeName preserved..."
    
    # Get PVCs in DR cluster
    pvc_output=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case14 -o custom-columns=NAME:.metadata.name,VOLUME:.spec.volumeName 2>/dev/null)
    echo "$pvc_output"
    
    # Count PVCs with volume names containing "case14"
    preserved_count=$(echo "$pvc_output" | grep -c "case14" || echo "0")
    
    # Also check if dynamic PVC has its volume name preserved
    dynamic_preserved=$(echo "$pvc_output" | grep "test-dynamic-pvc" | grep -c "pvc-" || echo "0")
    
    # Add counts
    total_preserved=$((preserved_count + dynamic_preserved))
    echo "Found $total_preserved PVCs with preserved volumeName fields"
    
    # Test passes if at least 4 PVCs have preserved volumeName
    if [ "$total_preserved" -ge 4 ]; then
        print_result "PVC volumeName preservation" "pass"
    else
        print_result "PVC volumeName preservation" "fail"
        exit 1
    fi
    
    print_result "Test completed" "pass"
    
    # Print summary
    echo -e "\nTest Summary:"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    
    # Exit 0 for success
    exit 0
}

# Execute main function
main