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

# Verify kubeconfig files exist
verify_kubeconfig() {
    local kubeconfig=$1
    if [ ! -f "$kubeconfig" ]; then
        fail "Kubeconfig file not found: $kubeconfig"
    fi
}

# Check required environment variables
check_env_vars() {
    local required_vars=("PROD_KUBECONFIG" "DR_KUBECONFIG" "CONTROLLER_KUBECONFIG")
    for var in "${required_vars[@]}"; do
        if [ -z "${!var}" ]; then
            fail "Required environment variable $var is not set"
        fi
        verify_kubeconfig "${!var}"
    done
}

# Initialize test environment
init_test_env() {
    check_env_vars
    info "Test environment initialized"
}
