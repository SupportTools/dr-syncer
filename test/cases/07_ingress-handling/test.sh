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

# Function to verify resource existence
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

# Function to verify Service
verify_service() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "service" "$name"; then
        echo "Service metadata verification failed"
        return 1
    fi
    
    # Compare specs (excluding clusterIP and status)
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
    
    # Get ingress specs
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    
    # Compare annotations
    local source_annotations=$(echo "$source_spec" | jq -S '.metadata.annotations')
    local dr_annotations=$(echo "$dr_spec" | jq -S '.metadata.annotations')
    if [ "$source_annotations" != "$dr_annotations" ]; then
        echo "Ingress annotations mismatch:"
        diff <(echo "$source_annotations") <(echo "$dr_annotations")
        return 1
    fi
    
    # Compare TLS configuration
    local source_tls=$(echo "$source_spec" | jq -S '.spec.tls')
    local dr_tls=$(echo "$dr_spec" | jq -S '.spec.tls')
    if [ "$source_tls" != "$dr_tls" ]; then
        echo "Ingress TLS configuration mismatch:"
        diff <(echo "$source_tls") <(echo "$dr_tls")
        return 1
    fi
    
    # Compare rules
    local source_rules=$(echo "$source_spec" | jq -S '.spec.rules')
    local dr_rules=$(echo "$dr_spec" | jq -S '.spec.rules')
    if [ "$source_rules" != "$dr_rules" ]; then
        echo "Ingress rules mismatch:"
        diff <(echo "$source_rules") <(echo "$dr_rules")
        return 1
    fi
    
    # Compare default backend if present
    local source_backend=$(echo "$source_spec" | jq -S '.spec.defaultBackend')
    local dr_backend=$(echo "$dr_spec" | jq -S '.spec.defaultBackend')
    if [ "$source_backend" != "$dr_backend" ]; then
        echo "Ingress default backend mismatch:"
        diff <(echo "$source_backend") <(echo "$dr_backend")
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
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
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
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/07_ingress-handling/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/07_ingress-handling/controller.yaml
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 07 (Ingress Handling)..."
    
    # Deploy resources
    deploy_resources
    
    echo "Waiting for replication to complete..."
    if ! wait_for_replication; then
        print_result "Replication ready" "fail"
        exit 1
    fi
    print_result "Replication ready" "pass"
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case07"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap
    if verify_resource "dr-sync-test-case07" "configmap" "test-configmap"; then
        if verify_configmap "dr-sync-test-case07" "test-configmap"; then
            print_result "ConfigMap synced and verified" "pass"
        else
            print_result "ConfigMap verification failed" "fail"
        fi
    else
        print_result "ConfigMap not found" "fail"
    fi
    
    # Verify Services
    for service in "test-service-1" "test-service-2"; do
        if verify_resource "dr-sync-test-case07" "service" "$service"; then
            if verify_service "dr-sync-test-case07" "$service"; then
                print_result "Service $service synced and verified" "pass"
            else
                print_result "Service $service verification failed" "fail"
            fi
        else
            print_result "Service $service not found" "fail"
        fi
    done
    
    # Verify Basic Ingress
    if verify_resource "dr-sync-test-case07" "ingress" "test-ingress-basic"; then
        if verify_ingress "dr-sync-test-case07" "test-ingress-basic"; then
            print_result "Basic Ingress synced and verified" "pass"
        else
            print_result "Basic Ingress verification failed" "fail"
        fi
    else
        print_result "Basic Ingress not found" "fail"
    fi
    
    # Verify Complex Ingress
    if verify_resource "dr-sync-test-case07" "ingress" "test-ingress-complex"; then
        if verify_ingress "dr-sync-test-case07" "test-ingress-complex"; then
            print_result "Complex Ingress synced and verified" "pass"
        else
            print_result "Complex Ingress verification failed" "fail"
        fi
    else
        print_result "Complex Ingress not found" "fail"
    fi
    
    # Verify Annotated Ingress
    if verify_resource "dr-sync-test-case07" "ingress" "test-ingress-annotations"; then
        if verify_ingress "dr-sync-test-case07" "test-ingress-annotations"; then
            print_result "Annotated Ingress synced and verified" "pass"
        else
            print_result "Annotated Ingress verification failed" "fail"
        fi
    else
        print_result "Annotated Ingress not found" "fail"
    fi
    
    # Verify Default Backend Ingress
    if verify_resource "dr-sync-test-case07" "ingress" "test-ingress-default"; then
        if verify_ingress "dr-sync-test-case07" "test-ingress-default"; then
            print_result "Default Backend Ingress synced and verified" "pass"
        else
            print_result "Default Backend Ingress verification failed" "fail"
        fi
    else
        print_result "Default Backend Ingress not found" "fail"
    fi
    
    # Verify Replication status fields
    echo "Verifying replication status..."
    
    # Get status fields
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.lastSyncTime}')
    local next_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.nextSyncTime}')
    local sync_duration=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.syncStats.lastSyncDuration}')
    
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
    local resource_status_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling -o jsonpath='{.status.resourceStatus[*].status}' | tr ' ' '\n' | grep -c "Synced" || echo "0")
    if [ "$resource_status_count" -ge 7 ]; then
        print_result "Detailed resource status (7 resources synced)" "pass"
    else
        print_result "Detailed resource status" "fail"
    fi
    
    # Verify printer columns
    local columns=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ingress-handling)
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
