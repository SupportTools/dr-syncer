#!/bin/bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Helper functions
info() {
    echo -e "${YELLOW}[INFO] $1${NC}"
}

fail() {
    echo -e "${RED}[FAIL] $1${NC}"
    exit 1
}

success() {
    echo -e "${GREEN}[SUCCESS] $1${NC}"
}

start_test() {
    echo -e "${GREEN}=== Running Test: $1 ===${NC}"
}

# Wait for a condition to be true
wait_for_condition() {
    local resource=$1
    local condition=$2
    local namespace=${3:-default}
    local timeout=${4:-120}
    local interval=${5:-2}

    info "Waiting for $resource in namespace $namespace to be $condition"
    
    local start_time=$(date +%s)
    while true; do
        if kubectl get "$resource" -n "$namespace" -o jsonpath="{.status.conditions[?(@.type=='$condition')].status}" | grep -q "True"; then
            return 0
        fi

        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        
        if [ $elapsed -gt $timeout ]; then
            fail "Timeout waiting for $resource to be $condition"
            return 1
        fi

        sleep $interval
    done
}

# Set kubeconfig paths if not already set
if [ -z "${PROD_KUBECONFIG}" ] || [ -z "${DR_KUBECONFIG}" ] || [ -z "${CONTROLLER_KUBECONFIG}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
    PROD_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/prod"
    DR_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/dr"
    CONTROLLER_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/controller"
    
    # Export for child processes
    export PROD_KUBECONFIG
    export DR_KUBECONFIG
    export CONTROLLER_KUBECONFIG
fi

# Verify kubeconfig files exist
verify_kubeconfig() {
    local kubeconfig=$1
    if [ ! -f "$kubeconfig" ]; then
        fail "Kubeconfig file not found: $kubeconfig"
    fi
}

# Check required kubeconfig files
check_env_vars() {
    local kubeconfig_files=("${PROD_KUBECONFIG}" "${DR_KUBECONFIG}" "${CONTROLLER_KUBECONFIG}")
    for config in "${kubeconfig_files[@]}"; do
        verify_kubeconfig "${config}"
    done
    info "Kubeconfig files verified"
}

# Initialize test environment
init_test_env() {
    check_env_vars
    info "Test environment initialized"
}
