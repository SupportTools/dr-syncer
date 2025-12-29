#!/bin/bash
# Simple test script for PVC sync verification

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
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case14 --ignore-not-found --wait=false
    kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case14 --ignore-not-found --wait=false

    # Delete NamespaceMapping
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespacemapping pvc-sync-persistent-volumes -n dr-syncer --ignore-not-found

    echo "Waiting for resources to be deleted..."
    sleep 5
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 14 (PVC Sync Verification)..."

    # Clean up any existing resources
    cleanup_resources

    # Deploy resources in production cluster
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/14_pvc-volume-name-preservation/remote.yaml

    # Deploy controller resources
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/14_pvc-volume-name-preservation/controller.yaml

    # Force an immediate sync
    echo "Forcing an immediate sync..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} annotate namespacemapping -n dr-syncer pvc-sync-persistent-volumes dr-syncer.io/sync-now=true --overwrite

    # Wait for initial replication
    echo "Waiting for initial replication (max 60 seconds)..."
    max_attempts=12
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
        print_result "Namespace sync" "fail"
        exit 1
    fi
    print_result "Namespace sync" "pass"

    # Wait a bit more to allow PVCs to be created
    sleep 10

    # Check if PVCs are synced
    echo "Checking if PVCs are synced to DR cluster..."

    # Get PVCs in DR cluster
    pvc_output=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case14 -o custom-columns=NAME:.metadata.name,STORAGE:.spec.resources.requests.storage,CLASS:.spec.storageClassName 2>/dev/null)
    echo "$pvc_output"

    # Count PVCs
    pvc_count=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case14 --no-headers 2>/dev/null | wc -l)
    echo "Found $pvc_count PVCs in DR cluster"

    # Test passes if all 3 PVCs are synced
    if [ "$pvc_count" -ge 3 ]; then
        print_result "PVC sync (3 PVCs)" "pass"
    else
        print_result "PVC sync (expected 3, got $pvc_count)" "fail"
    fi

    # Verify each PVC exists
    for pvc in test-pvc-1 test-pvc-2 test-pvc-3; do
        if kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case14 $pvc > /dev/null 2>&1; then
            print_result "PVC $pvc synced" "pass"
        else
            print_result "PVC $pvc synced" "fail"
        fi
    done

    # Verify ConfigMap synced
    if kubectl --kubeconfig ${DR_KUBECONFIG} get configmap -n dr-sync-test-case14 test-configmap > /dev/null 2>&1; then
        print_result "ConfigMap synced" "pass"
    else
        print_result "ConfigMap synced" "fail"
    fi

    # Verify Deployment synced with scale-to-zero
    if kubectl --kubeconfig ${DR_KUBECONFIG} get deployment -n dr-sync-test-case14 test-deployment > /dev/null 2>&1; then
        dr_replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} get deployment -n dr-sync-test-case14 test-deployment -o jsonpath='{.spec.replicas}')
        if [ "$dr_replicas" = "0" ]; then
            print_result "Deployment synced (scaled to zero)" "pass"
        else
            print_result "Deployment synced (expected 0 replicas, got $dr_replicas)" "fail"
        fi
    else
        print_result "Deployment synced" "fail"
    fi

    # Verify Service synced
    if kubectl --kubeconfig ${DR_KUBECONFIG} get service -n dr-sync-test-case14 test-service > /dev/null 2>&1; then
        print_result "Service synced" "pass"
    else
        print_result "Service synced" "fail"
    fi

    # Print summary
    echo -e "\nTest Summary:"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"

    # Return exit code based on test results
    if [ ${FAILED_TESTS} -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# Execute main function
main
