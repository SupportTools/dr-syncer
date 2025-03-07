#!/bin/bash
#set +e  # Don't exit on error

# Source common functions
source "$(dirname "$0")/../common.sh"

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

# Initialize test environment
init_test_env

# Verify ClusterMappings exist
verify_clustermappings_exist() {
    echo "Verifying global ClusterMappings exist..."
    
    local all_exist=true
    
    # Check nyc3-to-sfo3
    if kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping nyc3-to-sfo3 -n dr-syncer &>/dev/null; then
        echo "Global ClusterMapping 'nyc3-to-sfo3' found"
    else
        echo "ERROR: Global ClusterMapping 'nyc3-to-sfo3' not found"
        all_exist=false
    fi
    
    # Check sfo3-to-tor1
    if kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping sfo3-to-tor1 -n dr-syncer &>/dev/null; then
        echo "Global ClusterMapping 'sfo3-to-tor1' found"
    else
        echo "ERROR: Global ClusterMapping 'sfo3-to-tor1' not found"
        all_exist=false
    fi
    
    # Check tor1-to-nyc3
    if kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping tor1-to-nyc3 -n dr-syncer &>/dev/null; then
        echo "Global ClusterMapping 'tor1-to-nyc3' found"
    else
        echo "ERROR: Global ClusterMapping 'tor1-to-nyc3' not found"
        all_exist=false
    fi
    
    # Check sfo3-to-nyc3
    if kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping sfo3-to-nyc3 -n dr-syncer &>/dev/null; then
        echo "Global ClusterMapping 'sfo3-to-nyc3' found"
    else
        echo "ERROR: Global ClusterMapping 'sfo3-to-nyc3' not found"
        all_exist=false
    fi
    
    if [ "$all_exist" = true ]; then
        print_result "All ClusterMappings exist" "pass"
        return 0
    else
        print_result "All ClusterMappings exist" "fail"
        return 1
    fi
}

# Wait for all ClusterMappings to reach the Connected phase
wait_for_clustermappings() {
    echo "Waiting for ClusterMappings to reach Connected phase..."
    
    local all_connected=true
    local max_attempts=30
    
    for mapping in nyc3-to-sfo3 sfo3-to-tor1 tor1-to-nyc3 sfo3-to-nyc3; do
        echo "Checking $mapping..."
        for i in $(seq 1 $max_attempts); do
            PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping $mapping -n dr-syncer -o jsonpath='{.status.phase}' 2>/dev/null)
            if [ "$PHASE" == "Connected" ]; then
                echo "ClusterMapping $mapping is now in Connected phase"
                break
            fi
            
            if [ $i -eq $max_attempts ]; then
                echo "ERROR: ClusterMapping $mapping did not reach Connected phase within timeout"
                kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get clustermapping $mapping -n dr-syncer -o yaml
                all_connected=false
            fi
            
            echo "Current phase: $PHASE waiting (attempt $i/$max_attempts)..."
            sleep 10
        done
    done
    
    if [ "$all_connected" = true ]; then
        print_result "All ClusterMappings connected" "pass"
        return 0
    else
        print_result "All ClusterMappings connected" "fail"
        return 1
    fi
}

# Create all test namespaces on each cluster
create_test_namespaces() {
    # Create on NYC3 (controller/prod)
    echo "Creating namespaces on NYC3 cluster..."
    kubectl --kubeconfig ${PROD_KUBECONFIG} create namespace test-case-22-a --dry-run=client -o yaml | kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${PROD_KUBECONFIG} create namespace test-case-22-c --dry-run=client -o yaml | kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${PROD_KUBECONFIG} create namespace test-case-22-d --dry-run=client -o yaml | kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${PROD_KUBECONFIG} create namespace test-case-22-e --dry-run=client -o yaml | kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f -
    
    # Create on SFO3 (dr)
    echo "Creating namespaces on SFO3 cluster..."
    kubectl --kubeconfig ${DR_KUBECONFIG} create namespace test-case-22-a --dry-run=client -o yaml | kubectl --kubeconfig ${DR_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${DR_KUBECONFIG} create namespace test-case-22-b --dry-run=client -o yaml | kubectl --kubeconfig ${DR_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${DR_KUBECONFIG} create namespace test-case-22-d --dry-run=client -o yaml | kubectl --kubeconfig ${DR_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${DR_KUBECONFIG} create namespace test-case-22-e --dry-run=client -o yaml | kubectl --kubeconfig ${DR_KUBECONFIG} apply -f -
    
    # Create on TOR1 (edge cluster)
    echo "Creating namespaces on TOR1 cluster..."
    # Use the EDGE_KUBECONFIG environment variable or the edge kubeconfig file
    EDGE_KUBECONFIG=${EDGE_KUBECONFIG:-"${PROJECT_ROOT}/kubeconfig/edge"}
    kubectl --kubeconfig ${EDGE_KUBECONFIG} create namespace test-case-22-b --dry-run=client -o yaml | kubectl --kubeconfig ${EDGE_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${EDGE_KUBECONFIG} create namespace test-case-22-c --dry-run=client -o yaml | kubectl --kubeconfig ${EDGE_KUBECONFIG} apply -f -
    
    print_result "Create test namespaces" "pass"
}

# Apply all resources to their respective clusters
apply_resources() {
    echo "Applying resources to clusters..."
    
    # Apply to NYC3 (controller/prod)
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f "$(dirname "$0")/nyc3-resources.yaml"
    
    # Apply to SFO3 (dr)
    kubectl --kubeconfig ${DR_KUBECONFIG} apply -f "$(dirname "$0")/sfo3-resources.yaml"
    
    # Apply to TOR1 (edge cluster)
    EDGE_KUBECONFIG=${EDGE_KUBECONFIG:-"${PROJECT_ROOT}/kubeconfig/edge"}
    kubectl --kubeconfig ${EDGE_KUBECONFIG} apply -f "$(dirname "$0")/tor1-resources.yaml"
    
    print_result "Apply resources to clusters" "pass"
}

# Apply NamespaceMapping resources
apply_namespacemappings() {
    echo "Applying NamespaceMapping resources..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f "$(dirname "$0")/controller.yaml"
    print_result "Apply NamespaceMapping resources" "pass"
}

# Trigger all NamespaceMappings for manual replication
trigger_replication() {
    echo "Triggering manual replication for all NamespaceMappings..."
    
    for ns in test-case-22-a test-case-22-b test-case-22-c test-case-22-d test-case-22-e; do
        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} annotate namespacemapping $ns -n dr-syncer dr-syncer.io/sync-now=true --overwrite
    done
    
    print_result "Trigger replication" "pass"
}

# Wait for all NamespaceMappings to complete
wait_for_namespacemappings() {
    echo "Waiting for all NamespaceMappings to complete..."
    
    local all_completed=true
    local max_attempts=30
    
    for ns in test-case-22-a test-case-22-b test-case-22-c test-case-22-d test-case-22-e; do
        echo "Checking NamespaceMapping $ns..."
        for i in $(seq 1 $max_attempts); do
            STATUS=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get namespacemapping $ns -n dr-syncer -o jsonpath='{.status.phase}' 2>/dev/null)
            if [ "$STATUS" == "Completed" ]; then
                echo "NamespaceMapping $ns completed successfully"
                break
            fi
            
            if [ $i -eq $max_attempts ]; then
                echo "ERROR: NamespaceMapping $ns did not complete within timeout"
                kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} get namespacemapping $ns -n dr-syncer -o yaml
                all_completed=false
            fi
            
            echo "Current status: $STATUS waiting (attempt $i/$max_attempts)..."
            sleep 10
        done
    done
    
    if [ "$all_completed" = true ]; then
        print_result "All NamespaceMappings completed" "pass"
        return 0
    else
        print_result "All NamespaceMappings completed" "fail"
        return 1
    fi
}

# Verify replication from nyc3 to sfo3 (test-case-22-a)
verify_nyc3_to_sfo3_a() {
    echo "Verifying replication from nyc3 to sfo3 for namespace test-case-22-a..."
    
    # Verify ConfigMap
    local cm_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} get configmap configmap-nyc3-a -n test-case-22-a -o jsonpath='{.data.source}' 2>/dev/null)
    if [ "$cm_data" == "nyc3-a" ]; then
        echo "ConfigMap configmap-nyc3-a replicated correctly"
    else
        echo "ERROR: ConfigMap configmap-nyc3-a not replicated correctly, got: $cm_data"
        print_result "Verify nyc3→sfo3 (test-case-22-a)" "fail"
        return 1
    fi
    
    # Verify Deployment (should have 0 replicas in DR)
    local deployment_replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} get deployment deployment-nyc3-a -n test-case-22-a -o jsonpath='{.spec.replicas}' 2>/dev/null)
    if [ "$deployment_replicas" == "0" ]; then
        echo "Deployment deployment-nyc3-a replicated correctly with 0 replicas"
    else
        echo "ERROR: Deployment deployment-nyc3-a not replicated with 0 replicas, got: $deployment_replicas"
        print_result "Verify nyc3→sfo3 (test-case-22-a)" "fail"
        return 1
    fi
    
    # Verify Service
    local service_exists=$(kubectl --kubeconfig ${DR_KUBECONFIG} get service service-nyc3-a -n test-case-22-a -o name 2>/dev/null)
    if [ ! -z "$service_exists" ]; then
        echo "Service service-nyc3-a replicated correctly"
    else
        echo "ERROR: Service service-nyc3-a not replicated"
        print_result "Verify nyc3→sfo3 (test-case-22-a)" "fail"
        return 1
    fi
    
    print_result "Verify nyc3→sfo3 (test-case-22-a)" "pass"
    return 0
}

# Verify replication from sfo3 to tor1 (test-case-22-b)
verify_sfo3_to_tor1_b() {
    echo "Verifying replication from sfo3 to tor1 for namespace test-case-22-b..."
    
    # Verify ConfigMap
    EDGE_KUBECONFIG=${EDGE_KUBECONFIG:-"${PROJECT_ROOT}/kubeconfig/edge"}
    local cm_data=$(kubectl --kubeconfig ${EDGE_KUBECONFIG} get configmap configmap-sfo3-b -n test-case-22-b -o jsonpath='{.data.source}' 2>/dev/null)
    if [ "$cm_data" == "sfo3-b" ]; then
        echo "ConfigMap configmap-sfo3-b replicated correctly"
    else
        echo "ERROR: ConfigMap configmap-sfo3-b not replicated correctly, got: $cm_data"
        print_result "Verify sfo3→tor1 (test-case-22-b)" "fail"
        return 1
    fi
    
    # Verify Deployment (should have 0 replicas in DR)
    local deployment_replicas=$(kubectl --kubeconfig ${EDGE_KUBECONFIG} get deployment deployment-sfo3-b -n test-case-22-b -o jsonpath='{.spec.replicas}' 2>/dev/null)
    if [ "$deployment_replicas" == "0" ]; then
        echo "Deployment deployment-sfo3-b replicated correctly with 0 replicas"
    else
        echo "ERROR: Deployment deployment-sfo3-b not replicated with 0 replicas, got: $deployment_replicas"
        print_result "Verify sfo3→tor1 (test-case-22-b)" "fail"
        return 1
    fi
    
    # Verify Service
    local service_exists=$(kubectl --kubeconfig ${EDGE_KUBECONFIG} get service service-sfo3-b -n test-case-22-b -o name 2>/dev/null)
    if [ ! -z "$service_exists" ]; then
        echo "Service service-sfo3-b replicated correctly"
    else
        echo "ERROR: Service service-sfo3-b not replicated"
        print_result "Verify sfo3→tor1 (test-case-22-b)" "fail"
        return 1
    fi
    
    print_result "Verify sfo3→tor1 (test-case-22-b)" "pass"
    return 0
}

# Verify replication from tor1 to nyc3 (test-case-22-c)
verify_tor1_to_nyc3_c() {
    echo "Verifying replication from tor1 to nyc3 for namespace test-case-22-c..."
    
    # Verify ConfigMap
    local cm_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get configmap configmap-tor1-c -n test-case-22-c -o jsonpath='{.data.source}' 2>/dev/null)
    if [ "$cm_data" == "tor1-c" ]; then
        echo "ConfigMap configmap-tor1-c replicated correctly"
    else
        echo "ERROR: ConfigMap configmap-tor1-c not replicated correctly, got: $cm_data"
        print_result "Verify tor1→nyc3 (test-case-22-c)" "fail"
        return 1
    fi
    
    # Verify Deployment (should have 0 replicas in DR)
    local deployment_replicas=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get deployment deployment-tor1-c -n test-case-22-c -o jsonpath='{.spec.replicas}' 2>/dev/null)
    if [ "$deployment_replicas" == "0" ]; then
        echo "Deployment deployment-tor1-c replicated correctly with 0 replicas"
    else
        echo "ERROR: Deployment deployment-tor1-c not replicated with 0 replicas, got: $deployment_replicas"
        print_result "Verify tor1→nyc3 (test-case-22-c)" "fail"
        return 1
    fi
    
    # Verify Service
    local service_exists=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get service service-tor1-c -n test-case-22-c -o name 2>/dev/null)
    if [ ! -z "$service_exists" ]; then
        echo "Service service-tor1-c replicated correctly"
    else
        echo "ERROR: Service service-tor1-c not replicated"
        print_result "Verify tor1→nyc3 (test-case-22-c)" "fail"
        return 1
    fi
    
    print_result "Verify tor1→nyc3 (test-case-22-c)" "pass"
    return 0
}

# Verify replication from nyc3 to sfo3 (test-case-22-d)
verify_nyc3_to_sfo3_d() {
    echo "Verifying replication from nyc3 to sfo3 for namespace test-case-22-d..."
    
    # Verify ConfigMap
    local cm_data=$(kubectl --kubeconfig ${DR_KUBECONFIG} get configmap configmap-nyc3-d -n test-case-22-d -o jsonpath='{.data.source}' 2>/dev/null)
    if [ "$cm_data" == "nyc3-d" ]; then
        echo "ConfigMap configmap-nyc3-d replicated correctly"
    else
        echo "ERROR: ConfigMap configmap-nyc3-d not replicated correctly, got: $cm_data"
        print_result "Verify nyc3→sfo3 (test-case-22-d)" "fail"
        return 1
    fi
    
    # Verify Deployment (should have 0 replicas in DR)
    local deployment_replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} get deployment deployment-nyc3-d -n test-case-22-d -o jsonpath='{.spec.replicas}' 2>/dev/null)
    if [ "$deployment_replicas" == "0" ]; then
        echo "Deployment deployment-nyc3-d replicated correctly with 0 replicas"
    else
        echo "ERROR: Deployment deployment-nyc3-d not replicated with 0 replicas, got: $deployment_replicas"
        print_result "Verify nyc3→sfo3 (test-case-22-d)" "fail"
        return 1
    fi
    
    # Verify Service
    local service_exists=$(kubectl --kubeconfig ${DR_KUBECONFIG} get service service-nyc3-d -n test-case-22-d -o name 2>/dev/null)
    if [ ! -z "$service_exists" ]; then
        echo "Service service-nyc3-d replicated correctly"
    else
        echo "ERROR: Service service-nyc3-d not replicated"
        print_result "Verify nyc3→sfo3 (test-case-22-d)" "fail"
        return 1
    fi
    
    print_result "Verify nyc3→sfo3 (test-case-22-d)" "pass"
    return 0
}

# Verify replication from sfo3 to nyc3 (test-case-22-e)
verify_sfo3_to_nyc3_e() {
    echo "Verifying replication from sfo3 to nyc3 for namespace test-case-22-e..."
    
    # Verify ConfigMap
    local cm_data=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get configmap configmap-sfo3-e -n test-case-22-e -o jsonpath='{.data.source}' 2>/dev/null)
    if [ "$cm_data" == "sfo3-e" ]; then
        echo "ConfigMap configmap-sfo3-e replicated correctly"
    else
        echo "ERROR: ConfigMap configmap-sfo3-e not replicated correctly, got: $cm_data"
        print_result "Verify sfo3→nyc3 (test-case-22-e)" "fail"
        return 1
    fi
    
    # Verify Deployment (should have 0 replicas in DR)
    local deployment_replicas=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get deployment deployment-sfo3-e -n test-case-22-e -o jsonpath='{.spec.replicas}' 2>/dev/null)
    if [ "$deployment_replicas" == "0" ]; then
        echo "Deployment deployment-sfo3-e replicated correctly with 0 replicas"
    else
        echo "ERROR: Deployment deployment-sfo3-e not replicated with 0 replicas, got: $deployment_replicas"
        print_result "Verify sfo3→nyc3 (test-case-22-e)" "fail"
        return 1
    fi
    
    # Verify Service
    local service_exists=$(kubectl --kubeconfig ${PROD_KUBECONFIG} get service service-sfo3-e -n test-case-22-e -o name 2>/dev/null)
    if [ ! -z "$service_exists" ]; then
        echo "Service service-sfo3-e replicated correctly"
    else
        echo "ERROR: Service service-sfo3-e not replicated"
        print_result "Verify sfo3→nyc3 (test-case-22-e)" "fail"
        return 1
    fi
    
    print_result "Verify sfo3→nyc3 (test-case-22-e)" "pass"
    return 0
}

# Clean up resources
cleanup_resources() {
    echo "Cleaning up test resources..."
    
    # Delete NamespaceMappings
    for ns in test-case-22-a test-case-22-b test-case-22-c test-case-22-d test-case-22-e; do
        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespacemapping $ns -n dr-syncer --wait=false
    done
    
    # Delete namespaces in all clusters
    for ns in test-case-22-a test-case-22-b test-case-22-c test-case-22-d test-case-22-e; do
        # Controller cluster
        kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} delete namespace $ns --wait=false
    done
    
    # Delete namespaces in NYC3 (controller/prod)
    for ns in test-case-22-a test-case-22-c test-case-22-d test-case-22-e; do
        kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace $ns --wait=false
    done
    
    # Delete namespaces in SFO3 (dr)
    for ns in test-case-22-a test-case-22-b test-case-22-d test-case-22-e; do
        kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace $ns --wait=false
    done
    
    # Delete namespaces in TOR1 (edge cluster)
    EDGE_KUBECONFIG=${EDGE_KUBECONFIG:-"${PROJECT_ROOT}/kubeconfig/edge"}
    for ns in test-case-22-b test-case-22-c; do
        kubectl --kubeconfig ${EDGE_KUBECONFIG} delete namespace $ns --wait=false
    done
    
    print_result "Resource cleanup" "pass"
}

# Main test function
main() {
    echo "Testing multi-cluster circular replication across three clusters..."
    
    # Verify ClusterMappings exist
    verify_clustermappings_exist || exit 1
    
    # Wait for ClusterMappings to be connected
    wait_for_clustermappings || exit 1
    
    # Create all test namespaces
    create_test_namespaces
    
    # Apply resources to clusters
    apply_resources
    
    # Apply NamespaceMapping resources
    apply_namespacemappings
    
    # Trigger replication
    trigger_replication
    
    # Wait for NamespaceMappings to complete
    wait_for_namespacemappings || exit 1
    
    # Verify all replication paths
    verify_nyc3_to_sfo3_a
    verify_sfo3_to_tor1_b
    verify_tor1_to_nyc3_c
    verify_nyc3_to_sfo3_d
    verify_sfo3_to_nyc3_e
    
    # Clean up resources
    cleanup_resources
    
    # Print summary
    echo -e "\nTest Summary:"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    
    # Return exit code based on test results
    if [ ${FAILED_TESTS} -eq 0 ]; then
        echo "Test completed successfully"
        exit 0
    else
        echo "Test failed"
        exit 1
    fi
}

# Execute main function
main
