#!/bin/bash
set -e

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

# Function to verify namespace exists and has correct labels/annotations
verify_namespace() {
    local namespace=$1
    local source_namespace=$2
    
    # Verify namespace exists
    if ! verify_resource "" "namespace" "$namespace"; then
        echo "Namespace $namespace not found"
        return 1
    fi
    
    # Compare labels and annotations
    local source_metadata=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get namespace ${source_namespace} -o json | jq -S '.metadata | {labels, annotations}')
    local dr_metadata=$(kubectl --kubeconfig ${DR_KUBECONFIG} get namespace ${namespace} -o json | jq -S '.metadata | {labels, annotations}')
    
    if [ "$source_metadata" != "$dr_metadata" ]; then
        echo "Namespace metadata mismatch:"
        diff <(echo "$source_metadata") <(echo "$dr_metadata")
        return 1
    fi
    
    return 0
}

# Function to verify metadata matches between source and DR
verify_metadata() {
    local source_namespace=$1
    local dr_namespace=$2
    local resource_type=$3
    local resource_name=$4
    local ignore_fields=${5:-"resourceVersion,uid,creationTimestamp,generation"}
    
    # Get metadata from both clusters
    local source_metadata=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get ${resource_type} ${resource_name} -o jsonpath='{.metadata}')
    local dr_metadata=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get ${resource_type} ${resource_name} -o jsonpath='{.metadata}')
    
    # Remove ignored fields for comparison
    for field in $(echo $ignore_fields | tr ',' ' '); do
        source_metadata=$(echo "$source_metadata" | jq "del(.${field})")
        dr_metadata=$(echo "$dr_metadata" | jq "del(.${field})")
    done
    
    # Update namespace in source metadata to match DR namespace
    source_metadata=$(echo "$source_metadata" | jq ".namespace = \"$dr_namespace\"")
    
    if [ "$source_metadata" = "$dr_metadata" ]; then
        return 0
    fi
    echo "Metadata mismatch for ${resource_type}/${resource_name}:"
    diff <(echo "$source_metadata" | jq -S .) <(echo "$dr_metadata" | jq -S .)
    return 1
}

# Function to verify ConfigMap
verify_configmap() {
    local source_namespace=$1
    local dr_namespace=$2
    local name=$3
    
    # Verify metadata
    if ! verify_metadata "$source_namespace" "$dr_namespace" "configmap" "$name"; then
        echo "ConfigMap metadata verification failed"
        return 1
    fi
    
    # Compare data
    local source_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get configmap ${name} -o json | jq -S '.data')
    local dr_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get configmap ${name} -o json | jq -S '.data')
    
    if [ "$source_data" != "$dr_data" ]; then
        echo "ConfigMap data mismatch:"
        diff <(echo "$source_data") <(echo "$dr_data")
        return 1
    fi
    return 0
}

# Function to verify Secret
verify_secret() {
    local source_namespace=$1
    local dr_namespace=$2
    local name=$3
    
    # Verify metadata
    if ! verify_metadata "$source_namespace" "$dr_namespace" "secret" "$name"; then
        echo "Secret metadata verification failed"
        return 1
    fi
    
    # Compare data (after decoding)
    local source_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get secret ${name} -o json | jq -S '.data | map_values(@base64d)')
    local dr_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get secret ${name} -o json | jq -S '.data | map_values(@base64d)')
    
    if [ "$source_data" != "$dr_data" ]; then
        echo "Secret data mismatch:"
        diff <(echo "$source_data") <(echo "$dr_data")
        return 1
    fi
    return 0
}

# Function to verify Deployment
verify_deployment() {
    local source_namespace=$1
    local dr_namespace=$2
    local name=$3
    
    # Verify metadata
    if ! verify_metadata "$source_namespace" "$dr_namespace" "deployment" "$name"; then
        echo "Deployment metadata verification failed"
        return 1
    fi
    
    # Get source replicas to verify original count
    local source_replicas=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get deployment ${name} -o jsonpath='{.spec.replicas}')
    if [ "$source_replicas" != "3" ]; then
        echo "Source deployment should have 3 replicas, got: $source_replicas"
        return 1
    fi
    
    # Get DR replicas to verify scale down
    local dr_replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get deployment ${name} -o jsonpath='{.spec.replicas}')
    if [ "$dr_replicas" != "0" ]; then
        echo "DR deployment should have 0 replicas, got: $dr_replicas"
        return 1
    fi
    
    # Compare specs (excluding replicas and namespace references)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get deployment ${name} -o json | jq -S 'del(.spec.replicas, .status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get deployment ${name} -o json | jq -S 'del(.spec.replicas, .status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Deployment spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to verify Service
verify_service() {
    local source_namespace=$1
    local dr_namespace=$2
    local name=$3
    
    # Verify metadata
    if ! verify_metadata "$source_namespace" "$dr_namespace" "service" "$name"; then
        echo "Service metadata verification failed"
        return 1
    fi
    
    # Compare specs (excluding clusterIP and status)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get service ${name} -o json | jq -S 'del(.spec.clusterIP, .status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get service ${name} -o json | jq -S 'del(.spec.clusterIP, .status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Service spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to verify Ingress
verify_ingress() {
    local source_namespace=$1
    local dr_namespace=$2
    local name=$3
    
    # Verify metadata
    if ! verify_metadata "$source_namespace" "$dr_namespace" "ingress" "$name"; then
        echo "Ingress metadata verification failed"
        return 1
    fi
    
    # Get ingress specs
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${source_namespace} get ingress ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${dr_namespace} get ingress ${name} -o json)
    
    # Compare specs (excluding status)
    source_spec=$(echo "$source_spec" | jq -S 'del(.status, .metadata)')
    dr_spec=$(echo "$dr_spec" | jq -S 'del(.status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Ingress spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to wait for replication to be ready
wait_for_replication() {
    local name=$1
    local max_attempts=300
    local attempt=1
    local sleep_time=1
    
    echo "Waiting for replication $name to be ready..."
    while [ $attempt -le $max_attempts ]; do
        # Check phase and conditions
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ${name} -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication ${name} -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
        if [ "$PHASE" = "Completed" ] && [ "$REPLICATION_STATUS" = "True" ]; then
            echo "Replication $name is ready"
            return 0
        fi
        
        # Print current status for debugging
        echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Status=$REPLICATION_STATUS"
        echo "Waiting ${sleep_time}s..."
        sleep $sleep_time
        ((attempt++))
    done
    
    echo "Timeout waiting for replication $name to be ready"
    return 1
}

# Function to verify replication status
verify_replication_status() {
    local name=$1
    local phase=$2
    local expected_phase=$3
    local synced_count=$4
    local failed_count=$5
    
    if [ "$phase" != "$expected_phase" ]; then
        echo "Phase mismatch for $name: expected $expected_phase, got $phase"
        return 1
    fi
    
    if [ "$synced_count" -lt 1 ]; then
        echo "No successful syncs recorded for $name"
        return 1
    fi
    
    if [ "$failed_count" -ne 0 ]; then
        echo "Found failed syncs for $name: $failed_count"
        return 1
    fi
    
    return 0
}

# Function to deploy resources
deploy_resources() {
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/08_namespace-mapping/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/08_namespace-mapping/controller.yaml
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 08 (Namespace Mapping)..."
    
    # Deploy resources
    deploy_resources
    
    # Wait for replication
    echo "Waiting for namespace mapping replication to complete..."
    if ! wait_for_replication "namespace-mapping"; then
        print_result "Namespace mapping replication ready" "fail"
        exit 1
    fi
    print_result "Namespace mapping replication ready" "pass"
    
    # Verify namespace mapping
    echo "Verifying namespace mapping..."
    verify_namespace_resources "Namespace-Prod" "Namespace-DR"
    
    # Verify Replication status fields
    echo "Verifying replication status..."
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication namespace-mapping -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication namespace-mapping -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get replication namespace-mapping -o jsonpath='{.status.syncStats.failedSyncs}')
    
    if verify_replication_status "namespace mapping" "$phase" "Completed" "$synced_count" "$failed_count"; then
        print_result "Replication status" "pass"
    else
        print_result "Replication status" "fail"
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

# Function to verify resources in mapped namespace
verify_namespace_resources() {
    local source_namespace=$1
    local dr_namespace=$2
    
    # Verify namespace
    if verify_namespace "$dr_namespace" "$source_namespace"; then
        print_result "Namespace $dr_namespace created and verified" "pass"
    else
        print_result "Namespace $dr_namespace verification failed" "fail"
        return 1
    fi
    
    # Verify ConfigMap
    if verify_resource "$dr_namespace" "configmap" "test-configmap"; then
        if verify_configmap "$source_namespace" "$dr_namespace" "test-configmap"; then
            print_result "ConfigMap in $dr_namespace verified" "pass"
        else
            print_result "ConfigMap in $dr_namespace verification failed" "fail"
        fi
    else
        print_result "ConfigMap in $dr_namespace not found" "fail"
    fi
    
    # Verify Secret
    if verify_resource "$dr_namespace" "secret" "test-secret"; then
        if verify_secret "$source_namespace" "$dr_namespace" "test-secret"; then
            print_result "Secret in $dr_namespace verified" "pass"
        else
            print_result "Secret in $dr_namespace verification failed" "fail"
        fi
    else
        print_result "Secret in $dr_namespace not found" "fail"
    fi
    
    # Verify Deployment
    if verify_resource "$dr_namespace" "deployment" "test-deployment"; then
        if verify_deployment "$source_namespace" "$dr_namespace" "test-deployment"; then
            print_result "Deployment in $dr_namespace verified" "pass"
        else
            print_result "Deployment in $dr_namespace verification failed" "fail"
        fi
    else
        print_result "Deployment in $dr_namespace not found" "fail"
    fi
    
    # Verify Service
    if verify_resource "$dr_namespace" "service" "test-service"; then
        if verify_service "$source_namespace" "$dr_namespace" "test-service"; then
            print_result "Service in $dr_namespace verified" "pass"
        else
            print_result "Service in $dr_namespace verification failed" "fail"
        fi
    else
        print_result "Service in $dr_namespace not found" "fail"
    fi
    
    # Verify Ingress
    if verify_resource "$dr_namespace" "ingress" "test-ingress"; then
        if verify_ingress "$source_namespace" "$dr_namespace" "test-ingress"; then
            print_result "Ingress in $dr_namespace verified" "pass"
        else
            print_result "Ingress in $dr_namespace verification failed" "fail"
        fi
    else
        print_result "Ingress in $dr_namespace not found" "fail"
    fi
}

# Execute main function
main
