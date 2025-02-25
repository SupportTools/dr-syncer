#!/bin/bash
#set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Test tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Function to check required environment variables
check_environment() {
    local missing_vars=()
    
    if [ -z "${PROD_KUBECONFIG}" ]; then
        missing_vars+=("PROD_KUBECONFIG")
    fi
    
    if [ -z "${DR_KUBECONFIG}" ]; then
        missing_vars+=("DR_KUBECONFIG")
    fi
    
    if [ -z "${CONTROLLER_KUBECONFIG}" ]; then
        missing_vars+=("CONTROLLER_KUBECONFIG")
    fi
    
    if [ ${#missing_vars[@]} -ne 0 ]; then
        echo -e "${RED}Error: Missing required environment variables:${NC}"
        printf '%s\n' "${missing_vars[@]}"
        echo
        echo "Please set the following environment variables:"
        echo "  PROD_KUBECONFIG: Path to production cluster kubeconfig"
        echo "  DR_KUBECONFIG: Path to DR cluster kubeconfig"
        echo "  CONTROLLER_KUBECONFIG: Path to controller cluster kubeconfig"
        exit 1
    fi
    
    # Verify kubeconfig files exist
    for config in "${PROD_KUBECONFIG}" "${DR_KUBECONFIG}" "${CONTROLLER_KUBECONFIG}"; do
        if [ ! -f "${config}" ]; then
            echo -e "${RED}Error: Kubeconfig file not found: ${config}${NC}"
            exit 1
        fi
    done
    
    # Verify cluster access
    echo "Verifying cluster access..."
    
    if ! kubectl --kubeconfig "${PROD_KUBECONFIG}" cluster-info &>/dev/null; then
        echo -e "${RED}Error: Cannot access production cluster${NC}"
        exit 1
    fi
    
    if ! kubectl --kubeconfig "${DR_KUBECONFIG}" cluster-info &>/dev/null; then
        echo -e "${RED}Error: Cannot access DR cluster${NC}"
        exit 1
    fi
    
    if ! kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" cluster-info &>/dev/null; then
        echo -e "${RED}Error: Cannot access controller cluster${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ Environment verified${NC}"
}

# Function to run a single test case
run_test() {
    local test_num=$1
    local test_name=$2
    local test_script="test/cases/${test_name}/test.sh"
    
    echo -e "\n${YELLOW}Running Test Case ${test_num}: ${test_name}${NC}"
    
    if [ ! -f "${test_script}" ]; then
        echo -e "${RED}Error: Test script not found: ${test_script}${NC}"
        return 1
    fi
    
    if [ ! -x "${test_script}" ]; then
        chmod +x "${test_script}"
    fi
    
    if "${test_script}"; then
        echo -e "${GREEN}✓ Test Case ${test_num} passed${NC}"
        ((PASSED_TESTS++))
    else
        echo -e "${RED}✗ Test Case ${test_num} failed${NC}"
        ((FAILED_TESTS++))
    fi
    
    ((TOTAL_TESTS++))
}

# Function to run all tests
run_all_tests() {
    # Test cases in order
    local test_cases=(
        "00_standard-resources-test"
        "01_standard-resources-wildcard"
        "02_ignore-label"
        "03_scale-down"
        "04_scale-override"
        "05_resource-filtering"
        "06_service-recreation"
        "07_ingress-handling"
        "08_namespace-mapping"
        "09_pvc-handling"
        "10_pvc-basic-sync"
        "11_pvc-storage-class-mapping"
        "12_pvc-access-mode-mapping"
        "13_pvc-preserve-attributes"
        "14_pvc-sync-persistent-volumes"
        "15_pvc-combined-features"
        "16_replication_modes"
    )
    
    # Run each test case
    for test_case in "${test_cases[@]}"; do
        run_test "${test_case:0:2}" "${test_case}"
    done
}

# Function to run a specific test
run_specific_test() {
    local test_num=$1
    local test_name
    
    # Find test case directory
    for dir in test/cases/*; do
        if [[ "${dir}" =~ ^test/cases/[0-9]{2}.* ]]; then
            local dir_num="${dir:11:2}"
            if [ "${dir_num}" = "${test_num}" ]; then
                test_name=$(basename "${dir}")
                break
            fi
        fi
    done
    
    if [ -z "${test_name}" ]; then
        echo -e "${RED}Error: Test case ${test_num} not found${NC}"
        exit 1
    fi
    
    run_test "${test_num}" "${test_name}"
}

# Function to clean up resources after tests
cleanup_resources() {
    if [ "${SKIP_CLEANUP}" = "true" ]; then
        echo -e "${YELLOW}Skipping cleanup as requested${NC}"
        return 0
    fi
    
    echo -e "${YELLOW}Cleaning up resources...${NC}"
    
    # Clean up resources in controller cluster
    echo "Cleaning up controller resources..."
    kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer delete replication --all 2>/dev/null || true
    
    # Clean up resources in production cluster
    echo "Cleaning up production resources..."
    for ns in $(kubectl --kubeconfig "${PROD_KUBECONFIG}" get ns -o name | grep "dr-sync-test" 2>/dev/null); do
        kubectl --kubeconfig "${PROD_KUBECONFIG}" delete "${ns}" --wait=false 2>/dev/null || true
    done
    
    # Clean up resources in DR cluster
    echo "Cleaning up DR resources..."
    for ns in $(kubectl --kubeconfig "${DR_KUBECONFIG}" get ns -o name | grep "dr-sync-test" 2>/dev/null); do
        kubectl --kubeconfig "${DR_KUBECONFIG}" delete "${ns}" --wait=false 2>/dev/null || true
    done
    
    echo -e "${GREEN}✓ Cleanup completed${NC}"
}

# Main function
main() {
    # Parse command line arguments
    local test_num=""
    SKIP_CLEANUP="false"
    DEBUG="false"
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --test)
                test_num="$2"
                shift 2
                ;;
            --no-cleanup)
                SKIP_CLEANUP="true"
                shift
                ;;
            --debug)
                DEBUG="true"
                shift
                ;;
            *)
                echo "Usage: $0 [--test <test_number>] [--no-cleanup] [--debug]"
                exit 1
                ;;
        esac
    done
    
    # Export DEBUG flag for test scripts
    export DEBUG
    
    # Check environment
    check_environment
    
    # Run tests
    if [ -n "${test_num}" ]; then
        # Format test number with leading zero if needed
        test_num=$(printf "%02d" "${test_num}")
        run_specific_test "${test_num}"
    else
        run_all_tests
    fi
    
    # Print summary
    echo -e "\n${YELLOW}Test Summary:${NC}"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    
    # Clean up resources
    cleanup_resources
    
    # Return exit code based on test results
    if [ ${FAILED_TESTS} -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# Execute main function
main "$@"
