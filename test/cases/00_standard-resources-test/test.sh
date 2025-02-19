#!/bin/bash
set -e

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

# Function to verify resource existence and properties
verify_resource() {
    local namespace=$1
    local resource_type=$2
    local resource_name=$3
    
    if ! kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} &> /dev/null; then
        return 1
    fi
    return 0
}

# Function to wait for replication to be ready
wait_for_replication() {
    local max_attempts=30
    local attempt=1
    local sleep_time=10
    
    echo "Waiting for replication to be ready..."
    while [ $attempt -le $max_attempts ]; do
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication standard-resources-test -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        if [ "$REPLICATION_STATUS" = "True" ]; then
            echo "Replication is ready"
            return 0
        fi
        echo "Attempt $attempt/$max_attempts: Replication not ready yet, waiting ${sleep_time}s..."
        sleep $sleep_time
        ((attempt++))
    done
    
    echo "Timeout waiting for replication to be ready"
    return 1
}

# Function to deploy resources
deploy_resources() {
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/00_standard-resources-test/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/00_standard-resources-test/controller.yaml
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 00..."
    
    # Deploy resources
    deploy_resources
    
    echo "Waiting for replication to complete..."
    if ! wait_for_replication; then
        print_result "Replication ready" "fail"
        exit 1
    fi
    print_result "Replication ready" "pass"
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case00"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap and its data
    if verify_resource "dr-sync-test-case00" "configmap" "test-configmap"; then
        local key1_value=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get configmap test-configmap -o jsonpath='{.data.key1}')
        local key2_value=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get configmap test-configmap -o jsonpath='{.data.key2}')
        if [ "$key1_value" = "value1" ] && [ "$key2_value" = "value2" ]; then
            print_result "ConfigMap data verified" "pass"
        else
            print_result "ConfigMap data verification" "fail"
        fi
    else
        print_result "ConfigMap synced" "fail"
    fi
    
    # Verify Secret and its data
    if verify_resource "dr-sync-test-case00" "secret" "test-secret"; then
        local username=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get secret test-secret -o jsonpath='{.data.username}' | base64 -d)
        local password=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get secret test-secret -o jsonpath='{.data.password}' | base64 -d)
        if [ "$username" = "username" ] && [ "$password" = "password" ]; then
            print_result "Secret data verified" "pass"
        else
            print_result "Secret data verification" "fail"
        fi
    else
        print_result "Secret synced" "fail"
    fi
    
    # Verify Deployment and its replicas
    if verify_resource "dr-sync-test-case00" "deployment" "test-deployment"; then
        local replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get deployment test-deployment -o jsonpath='{.spec.replicas}')
        if [ "$replicas" = "0" ]; then
            print_result "Deployment replicas set to 0" "pass"
        else
            print_result "Deployment replicas verification" "fail"
        fi
    else
        print_result "Deployment synced" "fail"
    fi
    
    # Verify Service and its configuration
    if verify_resource "dr-sync-test-case00" "service" "test-service"; then
        local port=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get service test-service -o jsonpath='{.spec.ports[0].port}')
        local selector_app=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get service test-service -o jsonpath='{.spec.selector.app}')
        if [ "$port" = "80" ] && [ "$selector_app" = "test-app" ]; then
            print_result "Service configuration verified" "pass"
        else
            print_result "Service configuration verification" "fail"
        fi
    else
        print_result "Service synced" "fail"
    fi
    
    # Verify Ingress and its rules
    if verify_resource "dr-sync-test-case00" "ingress" "test-ingress"; then
        local host=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get ingress test-ingress -o jsonpath='{.spec.rules[0].host}')
        local service_name=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case00 get ingress test-ingress -o jsonpath='{.spec.rules[0].http.paths[0].backend.service.name}')
        if [ "$host" = "dr-sync-test-case00.example.com" ] && [ "$service_name" = "test-service" ]; then
            print_result "Ingress configuration verified" "pass"
        else
            print_result "Ingress configuration verification" "fail"
        fi
    else
        print_result "Ingress synced" "fail"
    fi
    
    # Verify Replication status
    local replication_status=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication standard-resources-test -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}')
    if [ "$replication_status" = "True" ]; then
        print_result "Replication status verified" "pass"
    else
        print_result "Replication status verification" "fail"
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
