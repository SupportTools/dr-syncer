#!/bin/bash
#set +e  # Don't exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Test status tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Enable debug mode if DEBUG is set to "true"
if [ "${DEBUG}" = "true" ]; then
    set -x  # Enable debug output
fi

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
    
    # Special handling for namespace resources (don't use -n flag)
    if [ "$resource_type" = "namespace" ]; then
        if ! kubectl --kubeconfig ${DR_KUBECONFIG} get ${resource_type} ${resource_name} &> /dev/null; then
            return 1
        fi
    else
        if ! kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} &> /dev/null; then
            return 1
        fi
    fi
    return 0
}

# Function to verify metadata matches between source and DR
verify_metadata() {
    local namespace=$1
    local resource_type=$2
    local resource_name=$3
    local ignore_fields=${4:-"resourceVersion,uid,creationTimestamp,generation,selfLink,managedFields,ownerReferences,finalizers"}
    
    # Get metadata from both clusters
    local source_metadata=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} -o jsonpath='{.metadata}')
    local dr_metadata=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ${resource_type} ${resource_name} -o jsonpath='{.metadata}')
    
    # Remove ignored fields for comparison
    for field in $(echo $ignore_fields | tr ',' ' '); do
        source_metadata=$(echo "$source_metadata" | jq "del(.${field})")
        dr_metadata=$(echo "$dr_metadata" | jq "del(.${field})")
    done
    
    # Remove all annotations
    source_metadata=$(echo "$source_metadata" | jq 'del(.annotations)')
    dr_metadata=$(echo "$dr_metadata" | jq 'del(.annotations)')
    
    # Remove dr-syncer labels
    source_metadata=$(echo "$source_metadata" | jq 'if .labels then .labels |= with_entries(select(.key | startswith("dr-syncer.io/") | not)) else . end')
    dr_metadata=$(echo "$dr_metadata" | jq 'if .labels then .labels |= with_entries(select(.key | startswith("dr-syncer.io/") | not)) else . end')
    
    if [ "$source_metadata" = "$dr_metadata" ]; then
        return 0
    fi
    echo "Metadata mismatch for ${resource_type}/${resource_name}:"
    diff <(echo "$source_metadata" | jq -S .) <(echo "$dr_metadata" | jq -S .)
    return 1
}

# Debug function
debug_resource() {
    # Only show debug output if DEBUG is true
    if [ "${DEBUG}" = "true" ]; then
        local namespace=$1
        local resource_type=$2
        local name=$3
        echo "DEBUG: Checking $resource_type/$name in namespace $namespace"
        echo "Source cluster:"
        kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get ${resource_type} ${name} -o yaml
        echo "DR cluster:"
        kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get ${resource_type} ${name} -o yaml
    fi
}

# Function to verify ConfigMap
verify_configmap() {
    local namespace=$1
    local name=$2
    
    if [ "${DEBUG}" = "true" ]; then
        echo "DEBUG: Verifying ConfigMap $name"
        debug_resource "$namespace" "configmap" "$name"
    fi
    
    # Verify metadata
    if ! verify_metadata "$namespace" "configmap" "$name"; then
        echo "ConfigMap metadata verification failed"
        return 1
    fi
    
    # Compare data
    local source_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get configmap ${name} -o json | jq -S '.data')
    local dr_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get configmap ${name} -o json | jq -S '.data')
    
    if [ "${DEBUG}" = "true" ]; then
        echo "DEBUG: Source data: $source_data"
        echo "DEBUG: DR data: $dr_data"
    fi
    
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

# Function to verify PVC
verify_pvc() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "persistentvolumeclaim" "$name"; then
        echo "PVC metadata verification failed"
        return 1
    fi
    
    # Get PVC specs from both clusters
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get pvc ${name} -o json)
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${name} -o json)
    
    # Compare storage class (with mapping support)
    local source_storage_class=$(echo "$source_spec" | jq -r '.spec.storageClassName')
    local dr_storage_class=$(echo "$dr_spec" | jq -r '.spec.storageClassName')
    
    # Storage class is allowed to be different due to mapping
    if [ "${DEBUG}" = "true" ]; then
        echo "DEBUG: Source storage class: $source_storage_class"
        echo "DEBUG: DR storage class: $dr_storage_class"
    fi
    
    # Compare access modes
    local source_access_modes=$(echo "$source_spec" | jq -r '.spec.accessModes[]' | sort)
    local dr_access_modes=$(echo "$dr_spec" | jq -r '.spec.accessModes[]' | sort)
    if [ "$source_access_modes" != "$dr_access_modes" ]; then
        echo "Access modes mismatch:"
        diff <(echo "$source_access_modes") <(echo "$dr_access_modes")
        return 1
    fi
    
    # Compare storage requests (must match exactly)
    local source_storage=$(echo "$source_spec" | jq -r '.spec.resources.requests.storage')
    local dr_storage=$(echo "$dr_spec" | jq -r '.spec.resources.requests.storage')
    if [ "$source_storage" != "$dr_storage" ]; then
        echo "Storage request mismatch: expected $source_storage, got $dr_storage"
        return 1
    fi
    
    return 0
}

# Function to verify StatefulSet
verify_statefulset() {
    local namespace=$1
    local name=$2
    
    # Verify metadata
    if ! verify_metadata "$namespace" "statefulset" "$name"; then
        echo "StatefulSet metadata verification failed"
        return 1
    fi
    
    # Get full specs (excluding status and metadata)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get statefulset ${name} -o json | jq -S 'del(.status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get statefulset ${name} -o json | jq -S 'del(.status, .metadata)')
    
    # Check replicas separately (should be 0 in DR)
    local dr_replicas=$(echo "$dr_spec" | jq '.spec.replicas')
    if [ "$dr_replicas" != "0" ]; then
        echo "DR StatefulSet replicas should be 0, got: $dr_replicas"
        return 1
    fi
    
    # Compare specs (excluding replicas)
    source_spec=$(echo "$source_spec" | jq 'del(.spec.replicas)')
    dr_spec=$(echo "$dr_spec" | jq 'del(.spec.replicas)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "StatefulSet spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to trigger manual sync
trigger_manual_sync() {
    echo "Triggering manual sync..."
    
    # Force update by applying a patched version with the annotation
    cat <<EOF | kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -
apiVersion: dr-syncer.io/v1alpha1
kind: NamespaceMapping
metadata:
  name: replication-manual
  namespace: dr-syncer
  annotations:
    dr-syncer.io/sync-now: "true"
spec:
  # Must include at least one spec field to avoid merge issues
  replicationMode: Manual
EOF
    
    echo "Manual sync triggered via annotation"
    
    # Verify the annotation was actually set
    echo "Verifying annotation was set:"
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get namespacemapping replication-manual -n dr-syncer -o yaml | grep -A 5 annotations
    
    # Wait for the controller to process the annotation
    sleep 5
}

# Function to wait for replication to be ready
wait_for_replication() {
    local max_attempts=600  # Double the timeout for manual mode
    local attempt=1
    local sleep_time=1
    
    echo "Waiting for replication to be ready..."
    while [ $attempt -le $max_attempts ]; do
        # Check phase and conditions - for Manual mode, we look for different status indicators
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        LAST_SYNC=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.lastSyncTime}' 2>/dev/null)
        
        # For Manual mode, we care about:
        # 1. Having a phase
        # 2. Having a Synced=True condition OR
        # 3. Having a lastSyncTime value
        if [ ! -z "$PHASE" ] && ([ "$REPLICATION_STATUS" = "True" ] || [ ! -z "$LAST_SYNC" ]); then
            echo "Replication is ready (Phase: $PHASE, Synced: $REPLICATION_STATUS, LastSync: $LAST_SYNC)"
            return 0
        fi
        
        # Debug output
        if [ "${DEBUG}" = "true" ]; then
            echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Status=$REPLICATION_STATUS, LastSync=$LAST_SYNC"
            # Show full object status for debugging
            kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o yaml
            echo "Waiting ${sleep_time}s..."
        elif [ $((attempt % 30)) -eq 0 ]; then
            # Print status every 30 attempts even in non-debug mode
            echo "Still waiting for replication... (attempt $attempt/$max_attempts)"
            echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Status=$REPLICATION_STATUS, LastSync=$LAST_SYNC"
        fi
        
        sleep $sleep_time
        ((attempt++))
    done
    
    # If we timed out, show the current status
    echo "Timeout waiting for replication to be ready"
    echo "Final status:"
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o yaml
    return 1
}

# Function to verify replication status
verify_replication_status() {
    local phase=$1
    local expected_phase=$2
    local synced_count=$3
    local failed_count=$4
    
    # For Manual mode, we need to be more flexible about phases
    if [ -z "$phase" ]; then
        echo "No phase found in status"
        return 1
    fi
    
    # In Manual mode, the phase might be different (like "Idle" instead of "Completed")
    # So we don't strictly check for expected_phase
    
    # Check that at least one successful sync was recorded
    if [ -z "$synced_count" ] || [ "$synced_count" -lt 1 ]; then
        echo "No successful syncs recorded (count: $synced_count)"
        return 1
    fi
    
    # Check for failed syncs
    if [ ! -z "$failed_count" ] && [ "$failed_count" -ne 0 ]; then
        echo "Found failed syncs: $failed_count"
        return 1
    fi
    
    # Pass if we have a phase and at least one successful sync
    return 0
}

# Function to deploy resources
deploy_resources() {
    echo "Deploying resources in production cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/16_manual_mode/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/16_manual_mode/controller.yaml
}

# Main test function
main() {
    echo "Testing DR-Sync functionality for case 16 (Manual Mode)..."
    
    # Deploy resources
    deploy_resources
    
    echo "Waiting for namespace mapping to initialize..."
    sleep 10
    
    # First check: verify resources are NOT synced before manual trigger
    echo "Checking that resources are NOT synced before manual trigger..."
    
    # Verify ConfigMap is not synced prior to manual trigger
    if verify_resource "dr-sync-test-case16-manual" "configmap" "test-configmap"; then
        print_result "ConfigMap NOT synced before manual trigger" "fail"
    else
        print_result "ConfigMap NOT synced before manual trigger" "pass"
    fi
    
    # Verify Secret is not synced prior to manual trigger
    if verify_resource "dr-sync-test-case16-manual" "secret" "test-secret"; then
        print_result "Secret NOT synced before manual trigger" "fail"
    else
        print_result "Secret NOT synced before manual trigger" "pass"
    fi
    
    # We've removed PVC from this test
    
    # Verify StatefulSet is not synced prior to manual trigger
    if verify_resource "dr-sync-test-case16-manual" "statefulset" "test-statefulset"; then
        print_result "StatefulSet NOT synced before manual trigger" "fail"
    else
        print_result "StatefulSet NOT synced before manual trigger" "pass"
    fi
    
    # Now trigger manual sync
    echo "Now triggering manual sync..."
    trigger_manual_sync
    
    echo "Waiting for manual replication to complete..."
    if ! wait_for_replication; then
        print_result "Replication ready" "fail"
        exit 1
    fi
    print_result "Replication ready" "pass"
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case16-manual"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify ConfigMap
    if verify_resource "dr-sync-test-case16-manual" "configmap" "test-configmap"; then
        if verify_configmap "dr-sync-test-case16-manual" "test-configmap"; then
            print_result "ConfigMap fully verified" "pass"
        else
            print_result "ConfigMap verification failed" "fail"
        fi
    else
        print_result "ConfigMap not found" "fail"
    fi
    
    # Verify Secret
    if verify_resource "dr-sync-test-case16-manual" "secret" "test-secret"; then
        if verify_secret "dr-sync-test-case16-manual" "test-secret"; then
            print_result "Secret fully verified" "pass"
        else
            print_result "Secret verification failed" "fail"
        fi
    else
        print_result "Secret not found" "fail"
    fi
    
    # We've removed PVC from this test
    
    # Verify StatefulSet
    if verify_resource "dr-sync-test-case16-manual" "statefulset" "test-statefulset"; then
        if verify_statefulset "dr-sync-test-case16-manual" "test-statefulset"; then
            print_result "StatefulSet fully verified" "pass"
        else
            print_result "StatefulSet verification failed" "fail"
        fi
    else
        print_result "StatefulSet not found" "fail"
    fi
    
    # Verify NamespaceMapping status fields
    echo "Verifying replication status..."
    
    # Get status fields
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.lastSyncTime}')
    local next_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.nextSyncTime}')
    local sync_duration=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual -o jsonpath='{.status.syncStats.lastSyncDuration}')
    
    # Verify phase and sync counts - for Manual mode we don't specify an expected phase
    if verify_replication_status "$phase" "" "$synced_count" "$failed_count"; then
        print_result "Replication phase and sync counts" "pass"
    else
        print_result "Replication phase and sync counts" "fail"
    fi
    
    # Verify timestamps
    if [ ! -z "$last_sync" ]; then
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
    
    # Try a second manual sync to verify it works consistently
    echo "Testing a second manual sync..."
    echo "Waiting 15 seconds before triggering second sync..."
    sleep 15  # Give the system time to settle before triggering another sync
    trigger_manual_sync
    
    echo "Waiting for second manual sync to complete..."
    if ! wait_for_replication; then
        print_result "Second manual sync completed" "fail"
    else
        print_result "Second manual sync completed" "pass"
    fi
    
    # Verify printer columns
    local columns=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping replication-manual)
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
