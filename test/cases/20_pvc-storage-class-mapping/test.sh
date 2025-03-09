#!/bin/bash
#set +e  # Don't exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
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

# Function to verify PVC
verify_pvc() {
    local namespace=$1
    local name=$2
    
    if [ "${DEBUG}" = "true" ]; then
        echo "DEBUG: Verifying PVC $name"
        debug_resource "$namespace" "pvc" "$name"
    fi
    
    # Verify metadata
    if ! verify_metadata "$namespace" "pvc" "$name"; then
        echo "PVC metadata verification failed"
        return 1
    fi
    
    # Compare access modes
    local source_access_modes=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get pvc ${name} -o json | jq -S '.spec.accessModes')
    local dr_access_modes=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${name} -o json | jq -S '.spec.accessModes')
    
    if [ "$source_access_modes" != "$dr_access_modes" ]; then
        echo "PVC access modes mismatch:"
        diff <(echo "$source_access_modes") <(echo "$dr_access_modes")
        return 1
    fi
    
    # Compare volume mode
    local source_volume_mode=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get pvc ${name} -o json | jq -S '.spec.volumeMode')
    local dr_volume_mode=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${name} -o json | jq -S '.spec.volumeMode')
    
    if [ "$source_volume_mode" != "$dr_volume_mode" ]; then
        echo "PVC volume mode mismatch:"
        diff <(echo "$source_volume_mode") <(echo "$dr_volume_mode")
        return 1
    fi
    
    return 0
}

# Function to verify storage class
verify_storage_class() {
    local namespace=$1
    local name=$2
    local expected_source_class=$3
    local expected_dr_class=$4
    
    # Get storage classes
    local source_storage_class=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get pvc ${name} -o jsonpath='{.spec.storageClassName}')
    local dr_storage_class=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${name} -o jsonpath='{.spec.storageClassName}')
    
    if [ "$source_storage_class" != "$expected_source_class" ]; then
        echo "Source PVC storage class mismatch: expected $expected_source_class, got $source_storage_class"
        return 1
    fi
    
    if [ "$dr_storage_class" != "$expected_dr_class" ]; then
        echo "DR PVC storage class mismatch: expected $expected_dr_class, got $dr_storage_class"
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
    
    # Get full specs (excluding status and metadata)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get deployment ${name} -o json | jq -S 'del(.status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get deployment ${name} -o json | jq -S 'del(.status, .metadata)')
    
    # Check replicas separately (should be 0 in DR)
    local dr_replicas=$(echo "$dr_spec" | jq '.spec.replicas')
    if [ "$dr_replicas" != "0" ]; then
        echo "DR deployment replicas should be 0, got: $dr_replicas"
        return 1
    fi
    
    # Compare specs (excluding replicas)
    source_spec=$(echo "$source_spec" | jq 'del(.spec.replicas)')
    dr_spec=$(echo "$dr_spec" | jq 'del(.spec.replicas)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Deployment spec mismatch:"
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
    
    # Get specs (excluding clusterIP, clusterIPs and status)
    local source_spec=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${namespace} get service ${name} -o json | jq -S 'del(.spec.clusterIP, .spec.clusterIPs, .status, .metadata)')
    local dr_spec=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get service ${name} -o json | jq -S 'del(.spec.clusterIP, .spec.clusterIPs, .status, .metadata)')
    
    if [ "$source_spec" != "$dr_spec" ]; then
        echo "Service spec mismatch:"
        diff <(echo "$source_spec") <(echo "$dr_spec")
        return 1
    fi
    return 0
}

# Function to create a test pod that mounts PVCs
create_test_pod() {
    local kubeconfig=$1
    local namespace=$2
    local pod_name=$3
    local pvc_names=$4  # Comma-separated list of PVC names
    
    echo "Creating test pod ${pod_name} in ${namespace}..."
    
    # Create volume mounts and volumes sections
    local volume_mounts=""
    local volumes=""
    
    # Convert comma-separated PVC names to array
    IFS=',' read -ra PVC_ARRAY <<< "$pvc_names"
    
    for pvc in "${PVC_ARRAY[@]}"; do
        # Create safe name for volume (remove special chars)
        local vol_name=$(echo "${pvc}" | tr -cd '[:alnum:]-')
        
        # Add volume mount
        volume_mounts="${volume_mounts}
        - name: ${vol_name}
          mountPath: /data/${pvc}"
          
        # Add volume
        volumes="${volumes}
        - name: ${vol_name}
          persistentVolumeClaim:
            claimName: ${pvc}"
    done
    
    # Create pod YAML
    cat <<EOF | kubectl --kubeconfig ${kubeconfig} apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: ${pod_name}
  namespace: ${namespace}
  labels:
    app: test-pod
    test-type: pvc-data-verification
spec:
  containers:
  - name: test-container
    image: busybox:latest
    command: ["sleep", "3600"]
    volumeMounts:${volume_mounts}
  volumes:${volumes}
  restartPolicy: Never
EOF
    
    # Wait for pod to be ready
    echo "Waiting for pod ${pod_name} to be ready..."
    kubectl --kubeconfig ${kubeconfig} -n ${namespace} wait --for=condition=Ready pod/${pod_name} --timeout=120s
    
    if [ $? -ne 0 ]; then
        echo "Failed to create test pod ${pod_name}"
        return 1
    fi
    
    echo "Test pod ${pod_name} is ready"
    return 0
}

# Function to write timestamp data to PVCs
write_timestamp_to_pvcs() {
    local kubeconfig=$1
    local namespace=$2
    local pod_name=$3
    local pvc_names=$4  # Comma-separated list of PVC names
    
    echo "Writing timestamp data to PVCs..."
    
    # Convert comma-separated PVC names to array
    IFS=',' read -ra PVC_ARRAY <<< "$pvc_names"
    
    # Get current timestamp
    local timestamp=$(date +"%Y-%m-%d %H:%M:%S")
    
    for pvc in "${PVC_ARRAY[@]}"; do
        echo "Writing to PVC ${pvc}..."
        
        # Write timestamp to file
        kubectl --kubeconfig ${kubeconfig} -n ${namespace} exec ${pod_name} -- sh -c "echo 'Timestamp: ${timestamp}' > /data/${pvc}/timestamp.txt"
        
        # Add PVC name to file for verification
        kubectl --kubeconfig ${kubeconfig} -n ${namespace} exec ${pod_name} -- sh -c "echo 'PVC: ${pvc}' >> /data/${pvc}/timestamp.txt"
        
        # Add some random data to ensure it's not just empty files being replicated
        kubectl --kubeconfig ${kubeconfig} -n ${namespace} exec ${pod_name} -- sh -c "head -c 1024 /dev/urandom | base64 | head -c 100 >> /data/${pvc}/timestamp.txt"
        
        # Verify file was written
        local file_content=$(kubectl --kubeconfig ${kubeconfig} -n ${namespace} exec ${pod_name} -- cat /data/${pvc}/timestamp.txt)
        
        if [[ "$file_content" == *"Timestamp: ${timestamp}"* ]]; then
            echo "Successfully wrote timestamp to PVC ${pvc}"
        else
            echo "Failed to write timestamp to PVC ${pvc}"
            return 1
        fi
    done
    
    return 0
}

# Function to verify timestamp data in PVCs
verify_timestamp_in_pvcs() {
    local source_kubeconfig=$1
    local dr_kubeconfig=$2
    local namespace=$3
    local source_pod=$4
    local dr_pod=$5
    local pvc_names=$6  # Comma-separated list of PVC names
    
    echo "Verifying timestamp data in PVCs..."
    
    # Convert comma-separated PVC names to array
    IFS=',' read -ra PVC_ARRAY <<< "$pvc_names"
    
    for pvc in "${PVC_ARRAY[@]}"; do
        echo "Verifying data in PVC ${pvc}..."
        
        # Get source timestamp file content
        local source_content=$(kubectl --kubeconfig ${source_kubeconfig} -n ${namespace} exec ${source_pod} -- cat /data/${pvc}/timestamp.txt)
        
        # Get DR timestamp file content
        local dr_content=$(kubectl --kubeconfig ${dr_kubeconfig} -n ${namespace} exec ${dr_pod} -- cat /data/${pvc}/timestamp.txt 2>/dev/null)
        
        # Check if file exists in DR
        if [ -z "$dr_content" ]; then
            echo "Timestamp file not found in DR PVC ${pvc}"
            return 1
        fi
        
        # Compare content
        if [ "$source_content" = "$dr_content" ]; then
            echo "Timestamp data successfully replicated for PVC ${pvc}"
        else
            echo "Timestamp data mismatch for PVC ${pvc}"
            echo "Source content: ${source_content}"
            echo "DR content: ${dr_content}"
            return 1
        fi
    done
    
    return 0
}

# Function to cleanup test pods
cleanup_test_pod() {
    local kubeconfig=$1
    local namespace=$2
    local pod_name=$3
    
    echo "Cleaning up test pod ${pod_name}..."
    kubectl --kubeconfig ${kubeconfig} -n ${namespace} delete pod ${pod_name} --grace-period=0 --force
    
    # Wait for pod to be deleted
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if ! kubectl --kubeconfig ${kubeconfig} -n ${namespace} get pod ${pod_name} &>/dev/null; then
            echo "Test pod ${pod_name} deleted"
            return 0
        fi
        
        sleep 1
        ((attempt++))
    done
    
    echo "Warning: Failed to confirm deletion of test pod ${pod_name}"
    return 1
}

# Function to wait for replication to be ready
wait_for_replication() {
    local max_attempts=300
    local attempt=1
    local sleep_time=1
    
    echo "Waiting for replication to be ready..."
    while [ $attempt -le $max_attempts ]; do
        # Check phase and conditions
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
        if [ "$PHASE" = "Completed" ] && [ "$REPLICATION_STATUS" = "True" ]; then
            echo "Replication is ready"
            return 0
        fi
        
        # Print current status for debugging only if DEBUG is true
        if [ "${DEBUG}" = "true" ]; then
            echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Status=$REPLICATION_STATUS"
            echo "Waiting ${sleep_time}s..."
        elif [ $((attempt % 30)) -eq 0 ]; then
            # Print status every 30 attempts even in non-debug mode
            echo "Still waiting for replication... (attempt $attempt/$max_attempts)"
        fi
        
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
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f test/cases/20_pvc-storage-class-mapping/remote.yaml
    
    echo "Deploying controller resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f test/cases/20_pvc-storage-class-mapping/controller.yaml
}

# Main test function
main() {
    echo "Testing PVC storage class mapping functionality for case 20..."
    
    # Deploy resources
    deploy_resources
    
    echo "Waiting for replication to complete..."
    if ! wait_for_replication; then
        print_result "Replication ready" "fail"
        exit 1
    fi
    print_result "Replication ready" "pass"
    
    # Verify namespace exists in DR cluster
    if verify_resource "" "namespace" "dr-sync-test-case20"; then
        print_result "Namespace created" "pass"
    else
        print_result "Namespace created" "fail"
    fi
    
    # Verify storage class mapping PVC
    if verify_resource "dr-sync-test-case20" "pvc" "test-pvc-storage-class"; then
        if verify_pvc "dr-sync-test-case20" "test-pvc-storage-class"; then
            if verify_storage_class "dr-sync-test-case20" "test-pvc-storage-class" "do-block-storage" "do-block-storage-retain"; then
                print_result "Storage class mapping PVC fully verified" "pass"
                
                # Test data replication by writing timestamp to PVCs
                echo "Testing PVC data replication..."
                
                # Create test pods in source and DR clusters
                local source_pod_name="source-test-pod"
                local dr_pod_name="dr-test-pod"
                local pvc_list="test-pvc-storage-class"
                
                # Create source test pod
                if create_test_pod "${PROD_KUBECONFIG}" "dr-sync-test-case20" "${source_pod_name}" "${pvc_list}"; then
                    print_result "Source test pod creation" "pass"
                    
                    # Write timestamp data to PVCs
                    if write_timestamp_to_pvcs "${PROD_KUBECONFIG}" "dr-sync-test-case20" "${source_pod_name}" "${pvc_list}"; then
                        print_result "Write timestamp data to PVC" "pass"
                        
                        # Force a sync to replicate the data
                        echo "Triggering a sync to replicate timestamp data..."
                        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer patch replication pvc-storage-class-mapping --type=json -p='[{"op": "replace", "path": "/metadata/annotations/dr-syncer.io~1force-sync", "value": "'$(date +%s)'"}]'
                        
                        # Wait for replication to complete
                        if wait_for_replication; then
                            print_result "Replication after writing timestamp data" "pass"
                            
                            # Create DR test pod
                            if create_test_pod "${DR_KUBECONFIG}" "dr-sync-test-case20" "${dr_pod_name}" "${pvc_list}"; then
                                print_result "DR test pod creation" "pass"
                                
                                # Verify timestamp data in DR PVCs
                                if verify_timestamp_in_pvcs "${PROD_KUBECONFIG}" "${DR_KUBECONFIG}" "dr-sync-test-case20" "${source_pod_name}" "${dr_pod_name}" "${pvc_list}"; then
                                    print_result "PVC data replication verification" "pass"
                                else
                                    print_result "PVC data replication verification" "fail"
                                fi
                                
                                # Clean up DR test pod
                                cleanup_test_pod "${DR_KUBECONFIG}" "dr-sync-test-case20" "${dr_pod_name}"
                            else
                                print_result "DR test pod creation" "fail"
                            fi
                        else
                            print_result "Replication after writing timestamp data" "fail"
                        fi
                    else
                        print_result "Write timestamp data to PVC" "fail"
                    fi
                    
                    # Clean up source test pod
                    cleanup_test_pod "${PROD_KUBECONFIG}" "dr-sync-test-case20" "${source_pod_name}"
                else
                    print_result "Source test pod creation" "fail"
                fi
            else
                print_result "Storage class mapping verification failed" "fail"
            fi
        else
            print_result "Storage class mapping PVC verification failed" "fail"
        fi
    else
        print_result "Storage class mapping PVC not found" "fail"
    fi
    
    # Verify Deployment
    if verify_resource "dr-sync-test-case20" "deployment" "test-deployment"; then
        if verify_deployment "dr-sync-test-case20" "test-deployment"; then
            print_result "Deployment fully verified" "pass"
        else
            print_result "Deployment verification failed" "fail"
        fi
    else
        print_result "Deployment not found" "fail"
    fi
    
    # Verify Service
    if verify_resource "dr-sync-test-case20" "service" "test-service"; then
        if verify_service "dr-sync-test-case20" "test-service"; then
            print_result "Service fully verified" "pass"
        else
            print_result "Service verification failed" "fail"
        fi
    else
        print_result "Service not found" "fail"
    fi
    
    # Verify NamespaceMapping status fields
    echo "Verifying replication status..."
    
    # Get status fields
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.lastSyncTime}')
    local next_sync=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.nextSyncTime}')
    local sync_duration=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping pvc-storage-class-mapping -o jsonpath='{.status.syncStats.lastSyncDuration}')
    
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
