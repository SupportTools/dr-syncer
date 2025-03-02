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

# Function to verify resource existence
verify_resource() {
    local context=$1
    local namespace=$2
    local resource_type=$3
    local resource_name=$4
    
    if ! kubectl --kubeconfig="${context}" -n ${namespace} get ${resource_type} ${resource_name} &> /dev/null; then
        return 1
    fi
    return 0
}

# Function to wait for pod to be ready
wait_for_pod_ready() {
    local context=$1
    local namespace=$2
    local pod_name=$3
    local timeout=${4:-120}
    
    echo -e "${YELLOW}Waiting for pod ${pod_name} to be ready (timeout: ${timeout}s)...${NC}"
    
    if kubectl --kubeconfig="${context}" wait --for=condition=Ready pod/${pod_name} -n ${namespace} --timeout=${timeout}s; then
        return 0
    else
        echo -e "${RED}Timeout waiting for pod ${pod_name} to be ready${NC}"
        return 1
    fi
}

# Function to wait for replication to be ready
wait_for_replication() {
    local max_attempts=60
    local attempt=1
    local sleep_time=5
    
    echo -e "${YELLOW}Waiting for replication to be ready...${NC}"
    while [ $attempt -le $max_attempts ]; do
        # Check phase and conditions
        PHASE=$(kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" get replication pvc-replication-test -o jsonpath='{.status.phase}' 2>/dev/null)
        REPLICATION_STATUS=$(kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" get replication pvc-replication-test -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)
        
        if [ "$PHASE" = "Completed" ] && [ "$REPLICATION_STATUS" = "True" ]; then
            echo -e "${GREEN}Replication is ready${NC}"
            return 0
        fi
        
        # Print current status for debugging
        if [ "${DEBUG}" = "true" ]; then
            echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Status=$REPLICATION_STATUS"
        elif [ $((attempt % 6)) -eq 0 ]; then
            # Print status every 30 seconds in non-debug mode
            echo "Still waiting for replication... (attempt $attempt/$max_attempts)"
        fi
        
        sleep $sleep_time
        ((attempt++))
    done
    
    echo -e "${RED}Timeout waiting for replication to be ready${NC}"
    return 1
}

# Function to verify file content
verify_file_content() {
    local context=$1
    local namespace=$2
    local pod_name=$3
    local file_path=$4
    local expected_content=$5
    
    local actual_content=$(kubectl --kubeconfig="${context}" -n ${namespace} exec ${pod_name} -- cat ${file_path} 2>/dev/null)
    
    if [ "$actual_content" = "$expected_content" ]; then
        return 0
    else
        echo "Content mismatch for file ${file_path}:"
        echo "Expected: ${expected_content}"
        echo "Actual: ${actual_content}"
        return 1
    fi
}

# Function to verify file checksum
verify_file_checksum() {
    local context=$1
    local namespace=$2
    local pod_name=$3
    local file_path=$4
    
    local source_checksum=$(kubectl --kubeconfig="${PROD_KUBECONFIG}" -n ${namespace} exec source-data-writer -- md5sum ${file_path} | awk '{print $1}')
    local dest_checksum=$(kubectl --kubeconfig="${DR_KUBECONFIG}" -n ${namespace} exec ${pod_name} -- md5sum ${file_path} | awk '{print $1}')
    
    if [ "$source_checksum" = "$dest_checksum" ]; then
        return 0
    else
        echo "Checksum mismatch for file ${file_path}:"
        echo "Source: ${source_checksum}"
        echo "Destination: ${dest_checksum}"
        return 1
    fi
}

# Function to verify replication status
verify_replication_status() {
    local phase=$(kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" get replication pvc-replication-test -o jsonpath='{.status.phase}')
    local synced_count=$(kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" get replication pvc-replication-test -o jsonpath='{.status.syncStats.successfulSyncs}')
    local failed_count=$(kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" get replication pvc-replication-test -o jsonpath='{.status.syncStats.failedSyncs}')
    local last_sync=$(kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" get replication pvc-replication-test -o jsonpath='{.status.lastSyncTime}')
    
    if [ "$phase" != "Completed" ]; then
        echo "Phase mismatch: expected Completed, got $phase"
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
    
    if [ -z "$last_sync" ]; then
        echo "No last sync time recorded"
        return 1
    fi
    
    return 0
}

# Function to create test data
create_test_data() {
    echo -e "${YELLOW}Creating test data in source PVC...${NC}"
    
    # Create a variety of test files with different sizes and content
    kubectl --kubeconfig="${PROD_KUBECONFIG}" -n test-pvc-replication exec source-data-writer -- /bin/sh -c "
        # Create a simple text file
        echo 'This is test data for PVC replication' > /data/test-file.txt
        
        # Create a larger file (1MB)
        dd if=/dev/urandom of=/data/large-file.bin bs=1M count=1
        
        # Create a directory structure with multiple files
        mkdir -p /data/nested/dir/structure
        echo 'Nested file content' > /data/nested/dir/structure/nested-file.txt
        
        # Create a file with special characters
        echo 'File with special chars: !@#$%^&*()' > /data/special-chars.txt
        
        # Create a symlink
        ln -s /data/test-file.txt /data/symlink-file
        
        # Set different permissions
        chmod 600 /data/test-file.txt
        chmod 644 /data/large-file.bin
        chmod 755 /data/nested
        
        # Generate checksums for verification
        md5sum /data/test-file.txt /data/large-file.bin /data/nested/dir/structure/nested-file.txt /data/special-chars.txt > /data/checksums.md5
        
        # List all created files
        find /data -type f | sort
    "
    
    # Verify test data was created
    local file_count=$(kubectl --kubeconfig="${PROD_KUBECONFIG}" -n test-pvc-replication exec source-data-writer -- find /data -type f | wc -l)
    if [ "$file_count" -ge 5 ]; then
        echo -e "${GREEN}Test data created successfully${NC}"
        return 0
    else
        echo -e "${RED}Failed to create test data${NC}"
        return 1
    fi
}

# Main test function
main() {
    echo -e "${GREEN}=== Running Test: PVC Data Replication ===${NC}"
    
    # Apply the test resources
    echo -e "${YELLOW}Applying test resources...${NC}"
    kubectl --kubeconfig="${PROD_KUBECONFIG}" apply -f test/cases/19_pvc-replication/controller.yaml --validate=false
    kubectl --kubeconfig="${DR_KUBECONFIG}" apply -f test/cases/19_pvc-replication/remote.yaml --validate=false
    
    # Wait for resources to be created
    echo -e "${YELLOW}Waiting for resources to be created...${NC}"
    sleep 5
    
    # Check if PVCs are created in both clusters
    echo -e "${YELLOW}Checking if PVCs are created in both clusters...${NC}"
    if verify_resource "${PROD_KUBECONFIG}" "test-pvc-replication" "pvc" "source-pvc"; then
        print_result "Source PVC created" "pass"
    else
        print_result "Source PVC created" "fail"
        exit 1
    fi
    
    if verify_resource "${DR_KUBECONFIG}" "test-pvc-replication" "pvc" "dest-pvc"; then
        print_result "Destination PVC created" "pass"
    else
        print_result "Destination PVC created" "fail"
        exit 1
    fi
    
    # Create a pod to write data to the source PVC
    echo -e "${YELLOW}Creating pod to write data to source PVC...${NC}"
    cat <<EOF | kubectl --kubeconfig="${PROD_KUBECONFIG}" apply -f - --validate=false
apiVersion: v1
kind: Pod
metadata:
  name: source-data-writer
  namespace: test-pvc-replication
spec:
  containers:
  - name: writer
    image: busybox
    command: ["/bin/sh", "-c", "touch /data/.initialized && sleep 3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: source-pvc
EOF
    
    # Wait for the pod to be ready
    if wait_for_pod_ready "${PROD_KUBECONFIG}" "test-pvc-replication" "source-data-writer"; then
        print_result "Source data writer pod ready" "pass"
    else
        print_result "Source data writer pod ready" "fail"
        exit 1
    fi
    
    # Create test data
    if create_test_data; then
        print_result "Test data creation" "pass"
    else
        print_result "Test data creation" "fail"
        exit 1
    fi
    
    # Create the replication resource
    echo -e "${YELLOW}Creating replication resource...${NC}"
    cat <<EOF | kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" apply -f - --validate=false
apiVersion: dr-syncer.io/v1alpha1
kind: Replication
metadata:
  name: pvc-replication-test
spec:
  clusterMappingRef:
    name: nyc3-to-sfo3
    namespace: dr-syncer
  sourceNamespace: test-pvc-replication
  destinationNamespace: test-pvc-replication
  replicationMode: Manual
  pvcConfig:
    syncData: true
    dataSyncConfig:
      concurrentSyncs: 1
      timeout: "5m"
      rsyncOptions:
        - "--archive"
        - "--verbose"
        - "--human-readable"
EOF
    
    # Trigger the replication
    echo -e "${YELLOW}Triggering PVC data replication...${NC}"
    kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" annotate replication pvc-replication-test dr-syncer.io/trigger-sync="$(date +%s)" --overwrite
    
    # Wait for the replication to complete
    if wait_for_replication; then
        print_result "Replication completed" "pass"
    else
        print_result "Replication completed" "fail"
        # Don't exit here, continue to check what happened
    fi
    
    # Create a pod to read data from the destination PVC
    echo -e "${YELLOW}Creating pod to read data from destination PVC...${NC}"
    cat <<EOF | kubectl --kubeconfig="${DR_KUBECONFIG}" apply -f - --validate=false
apiVersion: v1
kind: Pod
metadata:
  name: dest-data-reader
  namespace: test-pvc-replication
spec:
  containers:
  - name: reader
    image: busybox
    command: ["/bin/sh", "-c", "sleep 3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: dest-pvc
EOF
    
    # Wait for the pod to be ready
    if wait_for_pod_ready "${DR_KUBECONFIG}" "test-pvc-replication" "dest-data-reader"; then
        print_result "Destination data reader pod ready" "pass"
    else
        print_result "Destination data reader pod ready" "fail"
        exit 1
    fi
    
    # Verify the data replication
    echo -e "${YELLOW}Verifying data replication...${NC}"
    
    # Check if the simple text file was replicated
    if verify_file_content "${DR_KUBECONFIG}" "test-pvc-replication" "dest-data-reader" "/data/test-file.txt" "This is test data for PVC replication"; then
        print_result "Simple text file replication" "pass"
    else
        print_result "Simple text file replication" "fail"
    fi
    
    # Check if the large file was replicated (using checksum)
    if verify_file_checksum "${DR_KUBECONFIG}" "test-pvc-replication" "dest-data-reader" "/data/large-file.bin"; then
        print_result "Large file replication" "pass"
    else
        print_result "Large file replication" "fail"
    fi
    
    # Check if the nested directory structure was replicated
    if verify_file_content "${DR_KUBECONFIG}" "test-pvc-replication" "dest-data-reader" "/data/nested/dir/structure/nested-file.txt" "Nested file content"; then
        print_result "Nested directory replication" "pass"
    else
        print_result "Nested directory replication" "fail"
    fi
    
    # Check if the file with special characters was replicated
    if verify_file_content "${DR_KUBECONFIG}" "test-pvc-replication" "dest-data-reader" "/data/special-chars.txt" "File with special chars: !@#$%^&*()"; then
        print_result "Special characters file replication" "pass"
    else
        print_result "Special characters file replication" "fail"
    fi
    
    # Check if symlinks were preserved
    if kubectl --kubeconfig="${DR_KUBECONFIG}" -n test-pvc-replication exec dest-data-reader -- ls -la /data/symlink-file | grep -q "test-file.txt"; then
        print_result "Symlink preservation" "pass"
    else
        print_result "Symlink preservation" "fail"
    fi
    
    # Verify file permissions
    if kubectl --kubeconfig="${DR_KUBECONFIG}" -n test-pvc-replication exec dest-data-reader -- stat -c "%a" /data/test-file.txt | grep -q "600"; then
        print_result "File permissions preservation" "pass"
    else
        print_result "File permissions preservation" "fail"
    fi
    
    # Verify replication status
    if verify_replication_status; then
        print_result "Replication status" "pass"
    else
        print_result "Replication status" "fail"
    fi
    
    # Print summary
    echo -e "\n${GREEN}Test Summary:${NC}"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    
    # Clean up
    echo -e "${YELLOW}Cleaning up resources...${NC}"
    kubectl --kubeconfig="${PROD_KUBECONFIG}" delete -f test/cases/19_pvc-replication/controller.yaml --ignore-not-found
    kubectl --kubeconfig="${DR_KUBECONFIG}" delete -f test/cases/19_pvc-replication/remote.yaml --ignore-not-found
    kubectl --kubeconfig="${PROD_KUBECONFIG}" delete pod/source-data-writer -n test-pvc-replication --ignore-not-found
    kubectl --kubeconfig="${DR_KUBECONFIG}" delete pod/dest-data-reader -n test-pvc-replication --ignore-not-found
    kubectl --kubeconfig="${CONTROLLER_KUBECONFIG}" delete replication pvc-replication-test --ignore-not-found
    
    # Return exit code based on test results
    if [ ${FAILED_TESTS} -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# Execute main function
main
