#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
CONTROLLER_KUBECONFIG="/home/mmattox/.kube/mattox/a1-rancher-prd_fqdn"
PROD_KUBECONFIG="/home/mmattox/.kube/mattox/dr-syncer-nyc3-kubeconfig.yaml"
DR_KUBECONFIG="/home/mmattox/.kube/mattox/dr-syncer-sfo3-kubeconfig.yaml"

# Test status tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Function to print section headers
print_header() {
    echo -e "\n${BLUE}=== $1 ===${NC}"
}

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

# Function to verify resource existence
verify_resource() {
    local namespace=$1
    local resource_type=$2
    local resource_name=$3
    local kubeconfig=$4
    
    if kubectl --kubeconfig ${kubeconfig} -n ${namespace} get ${resource_type} ${resource_name} &> /dev/null; then
        return 0
    else
        return 1
    fi
}

# Setup phase
setup() {
    print_header "Setting up test environment"
    
    # Ensure dr-syncer namespace exists
    echo "Ensuring dr-syncer namespace exists..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} create namespace dr-syncer --dry-run=client -o yaml | \
        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -
    
    echo "Creating secrets in controller cluster..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer create secret generic dr-syncer-nyc3-kubeconfig \
        --from-file=kubeconfig=${PROD_KUBECONFIG} --dry-run=client -o yaml | \
        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -
    
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer create secret generic dr-syncer-sfo3-kubeconfig \
        --from-file=kubeconfig=${DR_KUBECONFIG} --dry-run=client -o yaml | \
        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -

    echo "Creating RemoteClusters in controller cluster..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer create -f test/remote-clusters.yaml \
        --dry-run=client -o yaml | kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -
        
    # Verify DR-Syncer controller is running
    echo "Verifying DR-Syncer controller is running..."
    local max_attempts=30
    local attempt=1
    local sleep_time=10
    
    while [ $attempt -le $max_attempts ]; do
        if kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get pods -l app.kubernetes.io/name=dr-syncer -o jsonpath='{.items[*].status.phase}' | grep -q "Running"; then
            echo "DR-Syncer controller is running"
            return 0
        fi
        echo "Attempt $attempt/$max_attempts: DR-Syncer controller not ready yet, waiting ${sleep_time}s..."
        sleep $sleep_time
        ((attempt++))
    done
    
    echo "Error: DR-Syncer controller is not running"
    exit 1
}

# Test case 00
test_case_00() {
    print_header "Testing Basic Resource Synchronization (Case 00)"
    
    # Export required variables for test script
    export CONTROLLER_KUBECONFIG DR_KUBECONFIG PROD_KUBECONFIG
    
    # Run the test script
    if test/cases/00_standard-resources-test/test.sh; then
        print_result "Test case 00" "pass"
    else
        print_result "Test case 00" "fail"
    fi
}

# Deploy test case 01
deploy_case_01() {
    print_header "Deploying test case 01"
    
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/01_standard-resources-wildcard/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/01_standard-resources-wildcard/controller.yaml

    echo "Waiting for initial replication to complete..."
    sleep 30
}

# Test standard resources with wildcard
test_case_01() {
    print_header "Testing Wildcard Namespace Selection (Case 01)"
    
    # Wait for resources to be synced
    echo "Waiting for resources to be synced..."
    sleep 30
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case01" "${DR_KUBECONFIG}"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap
    if verify_resource "dr-sync-test-case01" "configmap" "test-configmap" "${DR_KUBECONFIG}"; then
        print_result "ConfigMap synced" "pass"
    else
        print_result "ConfigMap synced" "fail"
    fi
    
    # Verify Secret
    if verify_resource "dr-sync-test-case01" "secret" "test-secret" "${DR_KUBECONFIG}"; then
        print_result "Secret synced" "pass"
    else
        print_result "Secret synced" "fail"
    fi
    
    # Verify Deployment
    if verify_resource "dr-sync-test-case01" "deployment" "test-deployment" "${DR_KUBECONFIG}"; then
        # Check if replicas are set to 0 in DR cluster
        REPLICAS=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case01 get deployment test-deployment -o jsonpath='{.spec.replicas}')
        if [ "$REPLICAS" = "0" ]; then
            print_result "Deployment synced with 0 replicas" "pass"
        else
            print_result "Deployment replicas" "fail"
        fi
    else
        print_result "Deployment synced" "fail"
    fi
    
    # Verify Service
    if verify_resource "dr-sync-test-case01" "service" "test-service" "${DR_KUBECONFIG}"; then
        print_result "Service synced" "pass"
    else
        print_result "Service synced" "fail"
    fi
    
    # Verify Ingress
    if verify_resource "dr-sync-test-case01" "ingress" "test-ingress" "${DR_KUBECONFIG}"; then
        print_result "Ingress synced" "pass"
    else
        print_result "Ingress synced" "fail"
    fi
    
    # Verify CRD status
    REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication standard-resources-test -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}')
    if [ "$REPLICATION_STATUS" = "True" ]; then
        print_result "CRD status updated" "pass"
    else
        print_result "CRD status updated" "fail"
    fi
}

# Deploy test case 02
deploy_case_02() {
    print_header "Deploying test case 02"
    
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/02_ignore-label/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/02_ignore-label/controller.yaml

    echo "Waiting for initial replication to complete..."
    sleep 30
}

# Deploy test case 03
deploy_case_03() {
    print_header "Deploying test case 03"
    
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/03_scale-down/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/03_scale-down/controller.yaml

    echo "Waiting for initial replication to complete..."
    sleep 30
}

# Test scale down functionality
test_case_03() {
    print_header "Testing Scale Down Functionality (Case 03)"
    
    # Wait for resources to be synced
    echo "Waiting for resources to be synced..."
    sleep 30
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case03" "${DR_KUBECONFIG}"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap
    if verify_resource "dr-sync-test-case03" "configmap" "test-configmap" "${DR_KUBECONFIG}"; then
        print_result "ConfigMap synced" "pass"
    else
        print_result "ConfigMap synced" "fail"
    fi
    
    # Verify Secret
    if verify_resource "dr-sync-test-case03" "secret" "test-secret" "${DR_KUBECONFIG}"; then
        print_result "Secret synced" "pass"
    else
        print_result "Secret synced" "fail"
    fi
    
    # Verify Deployment
    if verify_resource "dr-sync-test-case03" "deployment" "test-deployment" "${DR_KUBECONFIG}"; then
        # Check if replicas are set to 0 in DR cluster
        REPLICAS=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case03 get deployment test-deployment -o jsonpath='{.spec.replicas}')
        if [ "$REPLICAS" = "0" ]; then
            print_result "Deployment synced with 0 replicas (scaled down from 3)" "pass"
        else
            print_result "Deployment replicas" "fail"
        fi
    else
        print_result "Deployment synced" "fail"
    fi
    
    # Verify Service
    if verify_resource "dr-sync-test-case03" "service" "test-service" "${DR_KUBECONFIG}"; then
        print_result "Service synced" "pass"
    else
        print_result "Service synced" "fail"
    fi
    
    # Verify Ingress
    if verify_resource "dr-sync-test-case03" "ingress" "test-ingress" "${DR_KUBECONFIG}"; then
        print_result "Ingress synced" "pass"
    else
        print_result "Ingress synced" "fail"
    fi
    
    # Verify CRD status
    REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication standard-resources-test -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}')
    if [ "$REPLICATION_STATUS" = "True" ]; then
        print_result "CRD status updated" "pass"
    else
        print_result "CRD status updated" "fail"
    fi
}

# Test standard resources with wildcard
test_case_02() {
    print_header "Testing Resource Exclusion via Ignore Label (Case 02)"
    
    # Wait for resources to be synced
    echo "Waiting for resources to be synced..."
    sleep 30
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case02" "${DR_KUBECONFIG}"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap
    if verify_resource "dr-sync-test-case02" "configmap" "test-configmap" "${DR_KUBECONFIG}"; then
        print_result "ConfigMap synced" "pass"
    else
        print_result "ConfigMap synced" "fail"
    fi
    
    # Verify Secret
    if verify_resource "dr-sync-test-case02" "secret" "test-secret" "${DR_KUBECONFIG}"; then
        print_result "Secret synced" "pass"
    else
        print_result "Secret synced" "fail"
    fi
    
    # Verify Deployment 1 (should be synced)
    if verify_resource "dr-sync-test-case02" "deployment" "test-deployment-1" "${DR_KUBECONFIG}"; then
        # Check if replicas are set to 0 in DR cluster
        REPLICAS=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case02 get deployment test-deployment-1 -o jsonpath='{.spec.replicas}')
        if [ "$REPLICAS" = "0" ]; then
            print_result "Non-ignored deployment synced with 0 replicas" "pass"
        else
            print_result "Non-ignored deployment replicas" "fail"
        fi
    else
        print_result "Non-ignored deployment synced" "fail"
    fi
    
    # Verify Deployment 2 (should NOT be synced due to ignore label)
    if ! verify_resource "dr-sync-test-case02" "deployment" "test-deployment-2" "${DR_KUBECONFIG}"; then
        print_result "Ignored deployment not synced" "pass"
    else
        print_result "Ignored deployment incorrectly synced" "fail"
    fi
    
    # Verify Service
    if verify_resource "dr-sync-test-case02" "service" "test-service" "${DR_KUBECONFIG}"; then
        print_result "Service synced" "pass"
    else
        print_result "Service synced" "fail"
    fi
    
    # Verify Ingress
    if verify_resource "dr-sync-test-case02" "ingress" "test-ingress" "${DR_KUBECONFIG}"; then
        print_result "Ingress synced" "pass"
    else
        print_result "Ingress synced" "fail"
    fi
    
    # Verify CRD status
    REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication standard-resources-test -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}')
    if [ "$REPLICATION_STATUS" = "True" ]; then
        print_result "CRD status updated" "pass"
    else
        print_result "CRD status updated" "fail"
    fi
}


# Cleanup function
cleanup() {
    print_header "Cleaning up test environment"
    
    # Clean up based on which test was run
    case $TEST_CASE in
        "00")
            echo "Removing test case 00..."
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/00_standard-resources-test/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/00_standard-resources-test/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case00 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case00 --ignore-not-found
            ;;
        "01")
            echo "Removing test case 01..."
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/01_standard-resources-wildcard/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/01_standard-resources-wildcard/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case01 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case01 --ignore-not-found
            ;;
        "02")
            echo "Removing test case 02..."
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/02_ignore-label/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/02_ignore-label/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case02 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case02 --ignore-not-found
            ;;
        "03")
            echo "Removing test case 03..."
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/03_scale-down/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/03_scale-down/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case03 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case03 --ignore-not-found
            ;;
        *)
            # Clean up all test cases
            echo "Removing all test cases..."
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/00_standard-resources-test/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/00_standard-resources-test/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/01_standard-resources-wildcard/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/01_standard-resources-wildcard/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/02_ignore-label/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/02_ignore-label/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete -f test/cases/03_scale-down/remote.yaml --ignore-not-found
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete -f test/cases/03_scale-down/controller.yaml --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case00 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case00 --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case01 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case01 --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case02 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case02 --ignore-not-found
            kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace dr-sync-test-case03 --ignore-not-found
            kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace dr-sync-test-case03 --ignore-not-found
            ;;
    esac

    echo "Removing RemoteClusters..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer delete -f test/remote-clusters.yaml --ignore-not-found

    echo "Removing secrets..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer delete secret dr-syncer-nyc3-kubeconfig --ignore-not-found
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer delete secret dr-syncer-sfo3-kubeconfig --ignore-not-found
}

# Print final test summary
print_summary() {
    print_header "Test Summary"
    echo -e "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    
    if [ ${FAILED_TESTS} -eq 0 ]; then
        echo -e "\n${GREEN}All tests passed successfully!${NC}"
        exit 0
    else
        echo -e "\n${RED}Some tests failed!${NC}"
        exit 1
    fi
}

# Main execution
main() {
    # Handle command line arguments
    local skip_cleanup=false
    TEST_CASE=""
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --skip-cleanup)
                skip_cleanup=true
                shift
                ;;
            --test)
                TEST_CASE="$2"
                shift 2
                ;;
            *)
                echo "Unknown option: $1"
                exit 1
                ;;
        esac
    done

    # Trap for cleanup
    if [ "$skip_cleanup" = false ]; then
        trap cleanup EXIT
    fi

    # Setup environment
    setup

    # Run tests based on input
    case $TEST_CASE in
        "00")
            test_case_00
            ;;
        "01")
            deploy_case_01
            test_case_01
            ;;
        "02")
            deploy_case_02
            test_case_02
            ;;
        "03")
            deploy_case_03
            test_case_03
            ;;            
        *)
            # Run all tests
            deploy_case_00
            test_case_00
            deploy_case_01
            test_case_01
            deploy_case_02
            test_case_02
            deploy_case_03
            test_case_03            
            ;;
    esac

    # Print test summary
    print_summary
}

# Execute main function with all arguments
main "$@"
