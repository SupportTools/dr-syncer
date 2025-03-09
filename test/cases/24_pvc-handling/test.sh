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

# Function to verify PVC
verify_pvc() {
    local namespace=$1
    local name=$2
    local expected_storage_class=$3
    
    # Verify metadata
    if ! verify_metadata "$namespace" "persistentvolumeclaim" "$name"; then
        echo "PVC metadata verification failed"
        return 1
    fi
    
    # Get PVC specs from both clusters
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get pvc ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${name} -o json)
    
    # Verify storage class mapping
    local dr_storage_class=$(echo "$dr_spec" | jq -r '.spec.storageClassName')
    if [ "$dr_storage_class" != "$expected_storage_class" ]; then
        echo "Storage class mismatch: expected $expected_storage_class, got $dr_storage_class"
        return 1
    fi
    
    # Verify access modes
    local source_access_modes=$(echo "$source_spec" | jq -r '.spec.accessModes[]' | sort)
    local dr_access_modes=$(echo "$dr_spec" | jq -r '.spec.accessModes[]' | sort)
    if [ "$source_access_modes" != "$dr_access_modes" ]; then
        echo "Access modes mismatch:"
        diff <(echo "$source_access_modes") <(echo "$dr_access_modes")
        return 1
    fi
    
    # Verify storage requests
    local source_storage=$(echo "$source_spec" | jq -r '.spec.resources.requests.storage')
    local dr_storage=$(echo "$dr_spec" | jq -r '.spec.resources.requests.storage')
    if [ "$source_storage" != "$dr_storage" ]; then
        echo "Storage request mismatch: expected $source_storage, got $dr_storage"
        return 1
    fi
    
    # Verify volume mode
    local source_volume_mode=$(echo "$source_spec" | jq -r '.spec.volumeMode')
    local dr_volume_mode=$(echo "$dr_spec" | jq -r '.spec.volumeMode')
    if [ "$source_volume_mode" != "$dr_volume_mode" ]; then
        echo "Volume mode mismatch: expected $source_volume_mode, got $dr_volume_mode"
        return 1
    fi
    
    # Verify selector if present
    local source_selector=$(echo "$source_spec" | jq -r '.spec.selector // ""')
    local dr_selector=$(echo "$dr_spec" | jq -r '.spec.selector // ""')
    if [ "$source_selector" != "$dr_selector" ]; then
        echo "Selector mismatch:"
        diff <(echo "$source_selector") <(echo "$dr_selector")
        return 1
    fi
    
    return 0
}

# Function to verify Deployment
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
        echo "DR deployment should have 0 replicas, got: $dr_replicas"
        return 1
    fi
    
    # Compare volume mounts and PVC references
    local source_volumes=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get deployment ${name} -o json | jq -S '.spec.template.spec.volumes')
    local dr_volumes=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get deployment ${name} -o json | jq -S '.spec.template.spec.volumes')
    
    if [ "$source_volumes" != "$dr_volumes" ]; then
        echo "Volume configuration mismatch:"
        diff <(echo "$source_volumes") <(echo "$dr_volumes")
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

# Function to wait for replication to be ready
wait_for_replication() {
    local max_attempts=300
    local attempt=1
    local sleep_time=1
    
    echo "Waiting for replication to be ready..."
    while [ $attempt -le $max_attempts ]; do
        # Check phase and conditions
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
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
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/09_pvc-handling/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/09_pvc-handling/controller.yaml
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 09 (PVC Handling)..."
    
    # Deploy resources
    deploy_resources
    
    echo "Waiting for replication to complete..."
    if ! wait_for_replication; then
        print_result "Replication ready" "fail"
        exit 1
    fi
    print_result "Replication ready" "pass"
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case09"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify Basic PVC
    if verify_resource "dr-sync-test-case09" "persistentvolumeclaim" "test-pvc-basic"; then
        if verify_pvc "dr-sync-test-case09" "test-pvc-basic" "standard-dr"; then
            print_result "Basic PVC synced and verified" "pass"
        else
            print_result "Basic PVC verification failed" "fail"
        fi
    else
        print_result "Basic PVC not found" "fail"
    fi
    
    # Verify Multi-access PVC
    if verify_resource "dr-sync-test-case09" "persistentvolumeclaim" "test-pvc-multi"; then
        if verify_pvc "dr-sync-test-case09" "test-pvc-multi" "premium-dr"; then
            print_result "Multi-access PVC synced and verified" "pass"
        else
            print_result "Multi-access PVC verification failed" "fail"
        fi
    else
        print_result "Multi-access PVC not found" "fail"
    fi
    
    # Verify Dynamic PVC
    if verify_resource "dr-sync-test-case09" "persistentvolumeclaim" "test-pvc-dynamic"; then
        if verify_pvc "dr-sync-test-case09" "test-pvc-dynamic" "premium-dr"; then
            print_result "Dynamic PVC synced and verified" "pass"
        else
            print_result "Dynamic PVC verification failed" "fail"
        fi
    else
        print_result "Dynamic PVC not found" "fail"
    fi
    
    # Verify Deployment
    if verify_resource "dr-sync-test-case09" "deployment" "test-deployment"; then
        if verify_deployment "dr-sync-test-case09" "test-deployment"; then
            print_result "Deployment synced and verified" "pass"
        else
            print_result "Deployment verification failed" "fail"
        fi
    else
        print_result "Deployment not found" "fail"
    fi
    
    # Verify Service
    if verify_resource "dr-sync-test-case09" "service" "test-service"; then
        if verify_service "dr-sync-test-case09" "test-service"; then
            print_result "Service synced and verified" "pass"
        else
            print_result "Service verification failed" "fail"
        fi
    else
        print_result "Service not found" "fail"
    fi
    
    # Verify NamespaceMapping status fields
    echo "Verifying replication status..."
    
    # Get status fields
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.lastSyncTime}')
    local next_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.nextSyncTime}')
    local sync_duration=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.syncStats.lastSyncDuration}')
    
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
    local resource_status_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling -o jsonpath='{.status.resourceStatus[*].status}' | tr ' ' '\n' | grep -c "Synced" || echo "0")
    if [ "$resource_status_count" -ge 5 ]; then
        print_result "Detailed resource status (5 resources synced)" "pass"
    else
        print_result "Detailed resource status" "fail"
    fi
    
    # Verify printer columns
    local columns=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-handling)
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
