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

# Function to verify metadata matches between source and DR
verify_metadata() {
    local namespace=$1
    local resource_type=$2
    local resource_name=$3
    local ignore_fields=${4:-"resourceVersion,uid,creationTimestamp,generation"}
    
    # Get metadata from both clusters
    local source_metadata=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} -o jsonpath='{.metadata}')
    local dr_metadata=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} -o jsonpath='{.metadata}')
    
    # Remove ignored fields for comparison
    for field in $(echo $ignore_fields | tr ',' ' '); do
        source_metadata=$(echo "$source_metadata" | jq "del(.${field})")
        dr_metadata=$(echo "$dr_metadata" | jq "del(.${field})")
    done
    
    if [ "$source_metadata" = "$dr_metadata" ]; then
        return 0
    fi
    echo "Metadata mismatch for ${resource_type}/${resource_name}:"
    diff <(echo "$source_metadata" | jq -S .) <(echo "$dr_metadata" | jq -S .)
    return 1
}

# Function to verify ConfigMap
verify_configmap() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "configmap" "$name"; then
        echo "ConfigMap metadata verification failed"
        return 1
    fi
    
    # Compare data
    local source_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get configmap ${name} -o json | jq -S '.data')
    local dr_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get configmap ${name} -o json | jq -S '.data')
    
    if [ "$source_data" != "$dr_data" ]; then
        echo "ConfigMap data mismatch:"
        diff <(echo "$source_data") <(echo "$dr_data")
        return 1
    fi
    return 0
}

# Function to verify Secret
verify_secret() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "secret" "$name"; then
        echo "Secret metadata verification failed"
        return 1
    fi
    
    # Compare data (after decoding)
    local source_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get secret ${name} -o json | jq -S '.data | map_values(@base64d)')
    local dr_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get secret ${name} -o json | jq -S '.data | map_values(@base64d)')
    
    if [ "$source_data" != "$dr_data" ]; then
        echo "Secret data mismatch:"
        diff <(echo "$source_data") <(echo "$dr_data")
        return 1
    fi
    return 0
}

# Function to verify Deployment with scale down
verify_deployment() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "deployment" "$name"; then
        echo "Deployment metadata verification failed"
        return 1
    fi
    
    # Get source replicas to verify original count
    local source_replicas=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get deployment ${name} -o jsonpath='{.spec.replicas}')
    if [ "$source_replicas" != "3" ]; then
        echo "Source deployment should have 3 replicas, got: $source_replicas"
        return 1
    fi
    
    # Get DR replicas to verify scale down
    local dr_replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get deployment ${name} -o jsonpath='{.spec.replicas}')
    if [ "$dr_replicas" != "0" ]; then
        echo "DR deployment should have 0 replicas (scaled down), got: $dr_replicas"
        return 1
    fi
    
    # Compare specs (excluding replicas)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get deployment ${name} -o json | jq -S 'del(.spec.replicas, .status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get deployment ${name} -o json | jq -S 'del(.spec.replicas, .status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Deployment spec mismatch (excluding replicas):"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to verify Service
verify_service() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "service" "$name"; then
        echo "Service metadata verification failed"
        return 1
    fi
    
    # Get specs (excluding clusterIP and status)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get service ${name} -o json | jq -S 'del(.spec.clusterIP, .status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get service ${name} -o json | jq -S 'del(.spec.clusterIP, .status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Service spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to verify Ingress
verify_ingress() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "ingress" "$name"; then
        echo "Ingress metadata verification failed"
        return 1
    fi
    
    # Compare specs
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ingress ${name} -o json | jq -S 'del(.status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ingress ${name} -o json | jq -S 'del(.status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Ingress spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to wait for replication to be ready
wait_for_replication() {
    local max_attempts=300
    local attempt=1
    local sleep_time=1
    
    echo "Waiting for replication to be ready..."
    while [ $attempt -le $max_attempts ]; do
        # Check phase and conditions
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
        if [ "$PHASE" = "Completed" ] && [ "$REPLICATION_STATUS" = "True" ]; then
            echo "Replication is ready"
            return 0
        fi
        
        # Print current status for debugging
        echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Status=$REPLICATION_STATUS"
        echo "Waiting ${sleep_time}s..."
        sleep $sleep_time
        ((attempt++))
    done
    
    echo "Timeout waiting for replication to be ready"
    return 1
}

# Function to verify replication status
verify_replication_status() {
    local phase=$1
    local expected_phase=$2
    local synced_count=$3
    local failed_count=$4
    
    if [ "$phase" != "$expected_phase" ]; then
        echo "Phase mismatch: expected $expected_phase, got $phase"
        return 1
    fi
    
    if [ "$synced_count" -lt 1 ]; then
        echo "No successful syncs recorded"
        return 1
    fi
    
    if [ "$failed_count" -ne 0 ]; then
        echo "Found failed syncs: $failed_count"
        return 1
    fi
    
    return 0
}

# Function to deploy resources
deploy_resources() {
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/03_scale-down/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/03_scale-down/controller.yaml
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 03 (Scale Down)..."
    
    # Deploy resources
    deploy_resources
    
    echo "Waiting for replication to complete..."
    if ! wait_for_replication; then
        print_result "Replication ready" "fail"
        exit 1
    fi
    print_result "Replication ready" "pass"
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case03"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap
    if verify_resource "dr-sync-test-case03" "configmap" "test-configmap"; then
        if verify_configmap "dr-sync-test-case03" "test-configmap"; then
            print_result "ConfigMap fully verified" "pass"
        else
            print_result "ConfigMap verification failed" "fail"
        fi
    else
        print_result "ConfigMap not found" "fail"
    fi
    
    # Verify Secret
    if verify_resource "dr-sync-test-case03" "secret" "test-secret"; then
        if verify_secret "dr-sync-test-case03" "test-secret"; then
            print_result "Secret fully verified" "pass"
        else
            print_result "Secret verification failed" "fail"
        fi
    else
        print_result "Secret not found" "fail"
    fi
    
    # Verify Deployment (with scale down)
    if verify_resource "dr-sync-test-case03" "deployment" "test-deployment"; then
        if verify_deployment "dr-sync-test-case03" "test-deployment"; then
            print_result "Deployment scaled down and fully verified" "pass"
        else
            print_result "Deployment verification failed" "fail"
        fi
    else
        print_result "Deployment not found" "fail"
    fi
    
    # Verify Service
    if verify_resource "dr-sync-test-case03" "service" "test-service"; then
        if verify_service "dr-sync-test-case03" "test-service"; then
            print_result "Service fully verified" "pass"
        else
            print_result "Service verification failed" "fail"
        fi
    else
        print_result "Service not found" "fail"
    fi
    
    # Verify Ingress
    if verify_resource "dr-sync-test-case03" "ingress" "test-ingress"; then
        if verify_ingress "dr-sync-test-case03" "test-ingress"; then
            print_result "Ingress fully verified" "pass"
        else
            print_result "Ingress verification failed" "fail"
        fi
    else
        print_result "Ingress not found" "fail"
    fi
    
    # Verify Replication status fields
    echo "Verifying replication status..."
    
    # Get status fields
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.lastSyncTime}')
    local next_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.nextSyncTime}')
    local sync_duration=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.syncStats.lastSyncDuration}')
    
    # Verify phase and sync counts
    if verify_replication_status "$phase" "Completed" "$synced_count" "$failed_count"; then
        print_result "Replication phase and sync counts" "pass"
    else
        print_result "Replication phase and sync counts" "fail"
    fi
    
    # Verify timestamps
    if [ ! -z "$last_sync" ] && [ ! -z "$next_sync" ]; then
        print_result "Sync timestamps present" "pass"
    else
        print_result "Sync timestamps present" "fail"
    fi
    
    # Verify sync duration
    if [ ! -z "$sync_duration" ]; then
        print_result "Sync duration tracked" "pass"
    else
        print_result "Sync duration tracked" "fail"
    fi
    
    # Verify detailed resource status
    local resource_status_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down -o jsonpath='{.status.resourceStatus[*].status}' | tr ' ' '\n' | grep -c "Synced" || echo "0")
    if [ "$resource_status_count" -ge 5 ]; then
        print_result "Detailed resource status" "pass"
    else
        print_result "Detailed resource status" "fail"
    fi
    
    # Verify printer columns
    local columns=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication scale-down)
    if echo "$columns" | grep -q "Completed" && echo "$columns" | grep -q "[0-9]"; then
        print_result "Printer columns visible" "pass"
    else
        print_result "Printer columns visible" "fail"
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
