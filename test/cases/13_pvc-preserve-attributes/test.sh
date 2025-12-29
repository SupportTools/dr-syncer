#!/bin/bash
# Simple test script for PVC preserve attributes verification

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
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case13 --ignore-not-found --wait=false
    kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case13 --ignore-not-found --wait=false

    # Delete NamespaceMapping
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespacemapping pvc-preserve-attributes -n dr-syncer --ignore-not-found

    echo "Waiting for resources to be deleted..."
    sleep 5
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 13 (PVC Preserve Attributes)..."

    # Clean up any existing resources
    cleanup_resources

    # Deploy resources in production cluster
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/13_pvc-preserve-attributes/remote.yaml

    # Deploy controller resources
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/13_pvc-preserve-attributes/controller.yaml

    # Force an immediate sync
    echo "Forcing an immediate sync..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} annotate namespacemapping -n dr-syncer pvc-preserve-attributes dr-syncer.io/sync-now=true --overwrite

    # Wait for initial replication
    echo "Waiting for initial replication (max 60 seconds)..."
    max_attempts=12
    found_namespace=false

    for i in $(seq 1 $max_attempts); do
        echo "Checking (attempt $i/$max_attempts)..."

        # Check if namespace exists in DR
        if kubectl --kubeconfig ${DR_KUBECONFIG} get namespace dr-sync-test-case13 > /dev/null 2>&1; then
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

    # Wait a bit more to allow resources to be created
    sleep 10

    # Check if PVCs are synced
    echo "Checking if PVCs are synced to DR cluster..."

    # Get PVCs in DR cluster
    pvc_output=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case13 -o custom-columns=NAME:.metadata.name,STORAGE:.spec.resources.requests.storage,CLASS:.spec.storageClassName 2>/dev/null)
    echo "$pvc_output"

    # Count PVCs
    pvc_count=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case13 --no-headers 2>/dev/null | wc -l)
    echo "Found $pvc_count PVCs in DR cluster"

    # Test passes if at least some PVCs are synced (we have 7 in remote.yaml)
    if [ "$pvc_count" -ge 7 ]; then
        print_result "PVC sync (7 PVCs)" "pass"
    else
        print_result "PVC sync (expected 7, got $pvc_count)" "fail"
    fi

    # Verify key PVCs exist
    for pvc in test-pvc-volume-mode test-pvc-resources test-pvc-selector test-pvc-volume-name test-pvc-data-source test-pvc-mount-options test-pvc-node-affinity; do
        if kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case13 $pvc > /dev/null 2>&1; then
            print_result "PVC $pvc synced" "pass"
        else
            print_result "PVC $pvc synced" "fail"
        fi
    done

    # Verify PVC attributes are preserved for a sample PVC
    echo "Verifying PVC attributes are preserved..."

    # Check volume mode
    source_vm=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get pvc -n dr-sync-test-case13 test-pvc-volume-mode -o jsonpath='{.spec.volumeMode}')
    dr_vm=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case13 test-pvc-volume-mode -o jsonpath='{.spec.volumeMode}')
    if [ "$source_vm" = "$dr_vm" ]; then
        print_result "Volume mode preserved ($source_vm)" "pass"
    else
        print_result "Volume mode preserved (expected $source_vm, got $dr_vm)" "fail"
    fi

    # Check storage class is preserved
    source_sc=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get pvc -n dr-sync-test-case13 test-pvc-resources -o jsonpath='{.spec.storageClassName}')
    dr_sc=$(kubectl --kubeconfig ${DR_KUBECONFIG} get pvc -n dr-sync-test-case13 test-pvc-resources -o jsonpath='{.spec.storageClassName}')
    if [ "$source_sc" = "$dr_sc" ]; then
        print_result "Storage class preserved ($source_sc)" "pass"
    else
        print_result "Storage class preserved (expected $source_sc, got $dr_sc)" "fail"
    fi

    # Verify ConfigMap synced
    if kubectl --kubeconfig ${DR_KUBECONFIG} get configmap -n dr-sync-test-case13 test-configmap > /dev/null 2>&1; then
        print_result "ConfigMap synced" "pass"
    else
        print_result "ConfigMap synced" "fail"
    fi

    # Verify Deployment synced with scale-to-zero
    if kubectl --kubeconfig ${DR_KUBECONFIG} get deployment -n dr-sync-test-case13 test-deployment > /dev/null 2>&1; then
        dr_replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} get deployment -n dr-sync-test-case13 test-deployment -o jsonpath='{.spec.replicas}')
        if [ "$dr_replicas" = "0" ]; then
            print_result "Deployment synced (scaled to zero)" "pass"
        else
            print_result "Deployment synced (expected 0 replicas, got $dr_replicas)" "fail"
        fi
    else
        print_result "Deployment synced" "fail"
    fi

    # Verify Service synced
    if kubectl --kubeconfig ${DR_KUBECONFIG} get service -n dr-sync-test-case13 test-service > /dev/null 2>&1; then
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
