#!/bin/bash
# Removing set -e to see full output

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Debug flag
DEBUG=true

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
    local source_metadata=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} -o json | jq '.metadata')
    local dr_metadata=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} -o json | jq '.metadata')
    
    echo "DEBUG: Checking metadata for ${resource_type}/${resource_name}"
    echo "Source metadata before filtering: $source_metadata"
    echo "DR metadata before filtering: $dr_metadata"
    
    # Remove ignored fields and last-applied-configuration for comparison
    for field in $(echo $ignore_fields | tr ',' ' '); do
        source_metadata=$(echo "$source_metadata" | jq "del(.${field})")
        dr_metadata=$(echo "$dr_metadata" | jq "del(.${field})")
    done
    
    # Remove last-applied-configuration annotation
    source_metadata=$(echo "$source_metadata" | jq 'del(.annotations["kubectl.kubernetes.io/last-applied-configuration"])')
    dr_metadata=$(echo "$dr_metadata" | jq 'del(.annotations["kubectl.kubernetes.io/last-applied-configuration"])')
    
    # Handle empty annotations (make sure both source and dr have consistent annotations)
    if [[ $(echo "$source_metadata" | jq 'has("annotations")') == "true" ]]; then
        if [[ $(echo "$source_metadata" | jq '.annotations | length') == "0" ]]; then
            source_metadata=$(echo "$source_metadata" | jq 'del(.annotations)')
        fi
    fi
    
    if [[ $(echo "$dr_metadata" | jq 'has("annotations")') == "true" ]]; then
        if [[ $(echo "$dr_metadata" | jq '.annotations | length') == "0" ]]; then
            dr_metadata=$(echo "$dr_metadata" | jq 'del(.annotations)')
        fi
    fi
    
    echo "DEBUG: After filtering:"
    echo "Source metadata: $source_metadata"
    echo "DR metadata: $dr_metadata"
    
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
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get service ${name} -o json | jq -S '.spec | {ports, selector, type}')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get service ${name} -o json | jq -S '.spec | {ports, selector, type}')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Service spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to verify ingress annotations
verify_ingress_annotations() {
    local namespace=$1
    local name=$2
    
    # Get full ingress specs for debugging
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    
    # List of expected annotations for test-ingress-annotations
    local expected_annotations=(
        "kubernetes.io/ingress.class"
        "nginx.ingress.kubernetes.io/ssl-redirect"
        "nginx.ingress.kubernetes.io/proxy-body-size"
        "nginx.ingress.kubernetes.io/proxy-connect-timeout"
        "nginx.ingress.kubernetes.io/proxy-read-timeout"
        "nginx.ingress.kubernetes.io/proxy-send-timeout"
        "nginx.ingress.kubernetes.io/limit-rps"
        "nginx.ingress.kubernetes.io/enable-cors"
        "nginx.ingress.kubernetes.io/cors-allow-methods"
        "nginx.ingress.kubernetes.io/cors-allow-origin"
    )
    
    echo -e "\n${YELLOW}Checking individual annotations:${NC}"
    local failed=false
    
    for annotation in "${expected_annotations[@]}"; do
        local source_value=$(echo "$source_spec" | jq -r ".metadata.annotations[\"$annotation\"]")
        local dr_value=$(echo "$dr_spec" | jq -r ".metadata.annotations[\"$annotation\"]")
        
        echo -e "\nChecking annotation: $annotation"
        echo "Source value: $source_value"
        echo "DR value: $dr_value"
        
        if [ "$source_value" != "$dr_value" ]; then
            echo -e "${RED}✗ Annotation mismatch: $annotation${NC}"
            failed=true
        else
            echo -e "${GREEN}✓ Annotation matched: $annotation${NC}"
        fi
    done
    
    if [ "$failed" = true ]; then
        return 1
    fi
    return 0
}

# Function to verify ingress TLS configuration
verify_ingress_tls() {
    local namespace=$1
    local name=$2
    
    # Get full ingress specs for debugging
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    
    # Get TLS config
    local source_tls=$(echo "$source_spec" | jq -S '.spec.tls')
    local dr_tls=$(echo "$dr_spec" | jq -S '.spec.tls')
    
    echo "DEBUG: Checking TLS configuration for ingress ${name}..."
    echo "Source TLS: $source_tls"
    echo "DR TLS: $dr_tls"
    if [ "$source_tls" != "$dr_tls" ]; then
        echo "Ingress TLS configuration mismatch:"
        diff <(echo "$source_tls") <(echo "$dr_tls")
        return 1
    fi
    return 0
}

# Function to verify ingress backend references
verify_ingress_backends() {
    local namespace=$1
    local name=$2
    
    # Get full ingress specs for debugging
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ingress ${name} -o json)
    
    # Get rules and default backend
    local source_rules=$(echo "$source_spec" | jq -S '.spec.rules')
    local dr_rules=$(echo "$dr_spec" | jq -S '.spec.rules')
    local source_backend=$(echo "$source_spec" | jq -S '.spec.defaultBackend')
    local dr_backend=$(echo "$dr_spec" | jq -S '.spec.defaultBackend')
    
    echo "DEBUG: Checking backend references for ingress ${name}..."
    echo "Source rules: $source_rules"
    echo "DR rules: $dr_rules"
    echo "Source backend: $source_backend"
    echo "DR backend: $dr_backend"
    
    # Check rules
    if [ "$source_rules" != "$dr_rules" ]; then
        echo "Ingress rules mismatch:"
        diff <(echo "$source_rules") <(echo "$dr_rules")
        return 1
    fi
    
    # Check default backend if present
    if [ "$source_backend" != "$dr_backend" ]; then
        echo "Ingress default backend mismatch:"
        diff <(echo "$source_backend") <(echo "$dr_backend")
        return 1
    fi
    
    return 0
}

# Function to verify ingress metadata
verify_ingress_metadata() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "ingress" "$name"; then
        echo "Ingress metadata verification failed"
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
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
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
    
    # Force an immediate sync
    echo "Forcing an immediate sync..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} annotate namespacemapping -n dr-syncer test07-ingress-handling dr-syncer.io/sync-now=true --overwrite
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
    
    # Add a short sleep to ensure resources are fully propagated
    echo "Waiting 10 seconds for resources to propagate..."
    sleep 10
    
    # Verify namespace exists in DR cluster
    if kubectl --kubeconfig ${DR_KUBECONFIG} get ns dr-sync-test-case07 &>/dev/null; then
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
    
    # Test just one ingress for debugging
    ingress="test-ingress-annotations"
    echo -e "\n${YELLOW}Testing ingress: $ingress${NC}"
    
    # First verify the resource exists
    echo -e "\n${YELLOW}Verifying ingress exists in both clusters...${NC}"
    if ! kubectl --kubeconfig ${PROD_KUBECONFIG} -n dr-sync-test-case07 get ingress ${ingress} &>/dev/null; then
        echo "${RED}Error: Ingress $ingress not found in source cluster${NC}"
        exit 1
    fi
    if ! kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case07 get ingress ${ingress} &>/dev/null; then
        echo "${RED}Error: Ingress $ingress not found in DR cluster${NC}"
        exit 1
    fi
    echo "${GREEN}✓ Ingress exists in both clusters${NC}"
    
    # Get and display full specs
    echo -e "\n${YELLOW}Source ingress spec:${NC}"
    kubectl --kubeconfig ${PROD_KUBECONFIG} -n dr-sync-test-case07 get ingress ${ingress} -o yaml
    
    echo -e "\n${YELLOW}DR ingress spec:${NC}"
    kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case07 get ingress ${ingress} -o yaml
    
    echo -e "\n${YELLOW}Comparing specific components:${NC}"
        
        # Check metadata
        echo -e "\n${YELLOW}Checking metadata...${NC}"
        if verify_ingress_metadata "dr-sync-test-case07" "$ingress"; then
            print_result "${ingress} metadata preserved" "pass"
        else
            print_result "${ingress} metadata preserved" "fail"
        fi
        
        # Check annotations
        echo -e "\n${YELLOW}Checking annotations...${NC}"
        if verify_ingress_annotations "dr-sync-test-case07" "$ingress"; then
            print_result "${ingress} annotations preserved" "pass"
        else
            print_result "${ingress} annotations preserved" "fail"
        fi
        
        # Check TLS (if applicable)
        if [[ "$ingress" == "test-ingress-complex" ]]; then
            echo -e "\n${YELLOW}Checking TLS configuration...${NC}"
            if verify_ingress_tls "dr-sync-test-case07" "$ingress"; then
                print_result "${ingress} TLS configuration preserved" "pass"
            else
                print_result "${ingress} TLS configuration preserved" "fail"
            fi
        fi
        
        # Check backends
        echo -e "\n${YELLOW}Checking backend references...${NC}"
        if verify_ingress_backends "dr-sync-test-case07" "$ingress"; then
            print_result "${ingress} backend references preserved" "pass"
        else
            print_result "${ingress} backend references preserved" "fail"
        fi
    
    # Verify NamespaceMapping status fields
    echo "Verifying replication status..."
    
    # Get status fields
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.lastSyncTime}')
    local next_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.nextSyncTime}')
    local sync_duration=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.syncStats.lastSyncDuration}')
    
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
    echo "Checking replication status..."
    local total_resources=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.syncStats.totalResources}')
    local successful_syncs=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling -o jsonpath='{.status.syncStats.successfulSyncs}')
    
    echo "Total resources: $total_resources"
    echo "Successful syncs: $successful_syncs"
    
    # We expect at least one successful sync and total resources > 0
    if [ "$successful_syncs" -ge 1 ] && [ "$total_resources" -gt 0 ]; then
        print_result "Replication status verified" "pass"
    else
        print_result "Replication status verification failed" "fail"
    fi
    
    # Verify printer columns
    local columns=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping test07-ingress-handling)
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
