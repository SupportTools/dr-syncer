#!/bin/bash

# Test Case 25: Backup-Based PVC Sync
# Verifies that the Kopia backup path correctly backs up PVC data from the source
# cluster, creates standby PVCs on the DR cluster, and restores data to them.

# Source common functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../common.sh"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Test status tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Test configuration
MAPPING_NAME="test25-backup-pvc-sync"
SOURCE_NS="dr-sync-test-case25"
DEST_NS="dr-sync-test-case25"
PVC_NAME="test-pvc-backup"
STANDBY_PVC_NAME="${PVC_NAME}-standby"
TEST_DIR="test/cases/25_backup-based-pvc-sync"

# Enable debug mode if DEBUG is set
if [ "${DEBUG}" = "true" ]; then
    set -x
fi

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

verify_resource() {
    local namespace=$1
    local resource_type=$2
    local resource_name=$3

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

wait_for_backup_repo_ready() {
    local max_attempts=90
    local attempt=1

    echo "Waiting for BackupRepository to be ready..."
    while [ $attempt -le $max_attempts ]; do
        STATE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get backuprepository e2e-backup-repo -o jsonpath='{.status.state}' 2>/dev/null)

        if [ "$STATE" = "Ready" ]; then
            echo "BackupRepository is ready"
            return 0
        fi

        if [ "${DEBUG}" = "true" ]; then
            echo "Attempt $attempt/$max_attempts: State=$STATE"
        elif [ $((attempt % 15)) -eq 0 ]; then
            echo "Still waiting for BackupRepository... (attempt $attempt/$max_attempts, state=$STATE)"
        fi

        sleep 2
        ((attempt++))
    done

    echo "Timeout waiting for BackupRepository to be ready (last state: $STATE)"
    return 1
}

wait_for_replication() {
    local max_attempts=600
    local attempt=1

    echo "Waiting for replication to complete (backup operations may take several minutes)..."
    while [ $attempt -le $max_attempts ]; do
        PHASE=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping ${MAPPING_NAME} -o jsonpath='{.status.phase}' 2>/dev/null)
        SYNCED=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping ${MAPPING_NAME} -o jsonpath='{.status.conditions[?(@.type=="Synced")].status}' 2>/dev/null)

        if [ "$PHASE" = "Completed" ] && [ "$SYNCED" = "True" ]; then
            echo "Replication completed"
            return 0
        fi

        if [ "${DEBUG}" = "true" ]; then
            echo "Attempt $attempt/$max_attempts: Phase=$PHASE, Synced=$SYNCED"
        elif [ $((attempt % 30)) -eq 0 ]; then
            echo "Still waiting for replication... (attempt $attempt/$max_attempts, Phase=$PHASE, Synced=$SYNCED)"
        fi

        sleep 1
        ((attempt++))
    done

    echo "Timeout waiting for replication (Phase=$PHASE, Synced=$SYNCED)"
    # Print NamespaceMapping status for debugging
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping ${MAPPING_NAME} -o yaml 2>/dev/null | tail -30
    return 1
}

wait_for_pod_ready() {
    local kubeconfig=$1
    local namespace=$2
    local pod_name=$3
    local timeout=${4:-120}

    echo "Waiting for pod ${pod_name} to be ready..."
    kubectl --kubeconfig ${kubeconfig} -n ${namespace} wait --for=condition=Ready pod/${pod_name} --timeout=${timeout}s 2>/dev/null
}

deploy_resources() {
    echo "=== Deploying backup infrastructure ==="

    # Deploy S3 secrets and BackupRepository to controller cluster
    echo "Creating S3 credentials and BackupRepository..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f ${TEST_DIR}/backup-setup.yaml

    # Wait for BackupRepository to become ready
    if ! wait_for_backup_repo_ready; then
        echo "BackupRepository failed to reach Ready state"
        return 1
    fi

    echo "=== Deploying source resources ==="

    # Deploy source namespace and resources to prod cluster
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f ${TEST_DIR}/remote.yaml

    # Wait for data-writer pod to be ready and write data
    if ! wait_for_pod_ready ${PROD_KUBECONFIG} ${SOURCE_NS} data-writer 120; then
        echo "Data writer pod failed to start"
        return 1
    fi

    # Give the pod time to write test data
    echo "Waiting for test data to be written..."
    sleep 5

    # Verify data was written
    local test_file=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${SOURCE_NS} exec data-writer -- cat /data/test-file.txt 2>/dev/null)
    if [ -z "$test_file" ]; then
        echo "Warning: Could not verify test data was written"
    else
        echo "Test data verified: ${test_file}"
    fi

    echo "=== Deploying NamespaceMapping ==="

    # Deploy NamespaceMapping to controller cluster
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f ${TEST_DIR}/controller.yaml

    # Force immediate sync
    echo "Forcing immediate sync..."
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} annotate namespacemapping -n dr-syncer ${MAPPING_NAME} dr-syncer.io/sync-now=true --overwrite

    return 0
}

verify_standby_pvc() {
    local namespace=$1
    local standby_name=$2

    # Check standby PVC exists
    if ! verify_resource "${namespace}" "pvc" "${standby_name}"; then
        echo "Standby PVC ${standby_name} not found in DR cluster"
        return 1
    fi

    # Check standby label
    local is_standby=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${standby_name} -o jsonpath='{.metadata.labels.dr-syncer\.io/standby}' 2>/dev/null)
    if [ "$is_standby" != "true" ]; then
        echo "Standby PVC missing dr-syncer.io/standby=true label (got: $is_standby)"
        return 1
    fi

    # Check managed-by label
    local managed_by=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${standby_name} -o jsonpath='{.metadata.labels.dr-syncer\.io/managed-by}' 2>/dev/null)
    if [ "$managed_by" != "dr-syncer" ]; then
        echo "Standby PVC missing dr-syncer.io/managed-by=dr-syncer label (got: $managed_by)"
        return 1
    fi

    # Check source-pvc label
    local source_pvc=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${standby_name} -o jsonpath='{.metadata.labels.dr-syncer\.io/source-pvc}' 2>/dev/null)
    if [ "$source_pvc" != "${PVC_NAME}" ]; then
        echo "Standby PVC source-pvc label mismatch (expected: ${PVC_NAME}, got: $source_pvc)"
        return 1
    fi

    # Check PVC is bound
    local pvc_phase=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} get pvc ${standby_name} -o jsonpath='{.status.phase}' 2>/dev/null)
    if [ "$pvc_phase" != "Bound" ]; then
        echo "Standby PVC phase is ${pvc_phase}, expected Bound"
        return 1
    fi

    return 0
}

verify_backup_operations() {
    # Check VolumeBackupOperation CRs on source cluster (backup ops)
    local backup_count=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${SOURCE_NS} get volumebackupoperation -o json 2>/dev/null | jq '.items | length' 2>/dev/null)
    if [ -z "$backup_count" ] || [ "$backup_count" = "null" ]; then
        backup_count=0
    fi

    # Check VolumeBackupOperation CRs on DR cluster (restore ops)
    local restore_count=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${DEST_NS} get volumebackupoperation -o json 2>/dev/null | jq '.items | length' 2>/dev/null)
    if [ -z "$restore_count" ] || [ "$restore_count" = "null" ]; then
        restore_count=0
    fi

    if [ "${DEBUG}" = "true" ]; then
        echo "DEBUG: Backup operations on source: $backup_count"
        echo "DEBUG: Restore operations on DR: $restore_count"
        kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${SOURCE_NS} get volumebackupoperation -o wide 2>/dev/null
        kubectl --kubeconfig ${DR_KUBECONFIG} -n ${DEST_NS} get volumebackupoperation -o wide 2>/dev/null
    fi

    if [ "$backup_count" -lt 1 ]; then
        echo "No VolumeBackupOperation CRs found on source cluster (expected >= 1)"
        return 1
    fi

    # Check for failed operations
    local failed_ops=$(kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${SOURCE_NS} get volumebackupoperation -o json 2>/dev/null | jq '[.items[] | select(.status.phase == "Failed")] | length' 2>/dev/null)
    if [ -n "$failed_ops" ] && [ "$failed_ops" -gt 0 ]; then
        echo "Found $failed_ops failed backup operations"
        kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${SOURCE_NS} get volumebackupoperation -o json 2>/dev/null | jq '.items[] | select(.status.phase == "Failed") | {name: .metadata.name, message: .status.message}'
        return 1
    fi

    return 0
}

# Creates a single verification pod that mounts the standby PVC and checks all files.
# This avoids creating multiple pods (one per file check) which saves ~2 min of startup time.
VERIFY_POD_NAME=""

create_verify_pod() {
    local namespace=$1
    local pvc_name=$2

    VERIFY_POD_NAME="verify-data-$(date +%s%N)"
    kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} run ${VERIFY_POD_NAME} \
        --image=busybox:1.36 \
        --restart=Never \
        --overrides="{
            \"spec\": {
                \"containers\": [{
                    \"name\": \"verify\",
                    \"image\": \"busybox:1.36\",
                    \"command\": [\"sleep\", \"300\"],
                    \"volumeMounts\": [{
                        \"name\": \"data\",
                        \"mountPath\": \"/data\"
                    }]
                }],
                \"volumes\": [{
                    \"name\": \"data\",
                    \"persistentVolumeClaim\": {
                        \"claimName\": \"${pvc_name}\"
                    }
                }]
            }
        }" 2>/dev/null

    if ! wait_for_pod_ready ${DR_KUBECONFIG} ${namespace} ${VERIFY_POD_NAME} 60; then
        echo "Verification pod failed to start"
        kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} delete pod ${VERIFY_POD_NAME} --force --grace-period=0 2>/dev/null
        VERIFY_POD_NAME=""
        return 1
    fi
    return 0
}

delete_verify_pod() {
    local namespace=$1
    if [ -n "${VERIFY_POD_NAME}" ]; then
        kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} delete pod ${VERIFY_POD_NAME} --force --grace-period=0 2>/dev/null
        VERIFY_POD_NAME=""
    fi
}

verify_standby_pvc_data() {
    local namespace=$1
    local expected_file=$2
    local expected_content=$3

    if [ -z "${VERIFY_POD_NAME}" ]; then
        echo "No verification pod running"
        return 1
    fi

    local actual_content=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${namespace} exec ${VERIFY_POD_NAME} -- cat /data/${expected_file} 2>/dev/null)

    if [[ "$actual_content" == *"$expected_content"* ]]; then
        return 0
    fi

    echo "Content mismatch for /data/${expected_file}"
    echo "  Expected to contain: '$expected_content'"
    echo "  Actual: '$actual_content'"
    return 1
}

cleanup() {
    if [ "${SKIP_CLEANUP}" = "true" ]; then
        echo -e "${YELLOW}Skipping cleanup as requested${NC}"
        return 0
    fi

    echo "Cleaning up test resources..."

    # Delete NamespaceMapping
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer delete namespacemapping ${MAPPING_NAME} 2>/dev/null || true

    # Delete BackupRepository and secrets
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer delete backuprepository e2e-backup-repo 2>/dev/null || true
    kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer delete secret e2e-s3-credentials e2e-kopia-password 2>/dev/null || true

    # Delete VolumeBackupOperation CRs
    kubectl --kubeconfig ${PROD_KUBECONFIG} -n ${SOURCE_NS} delete volumebackupoperation --all 2>/dev/null || true
    kubectl --kubeconfig ${DR_KUBECONFIG} -n ${DEST_NS} delete volumebackupoperation --all 2>/dev/null || true

    # Delete source namespace
    kubectl --kubeconfig ${PROD_KUBECONFIG} delete namespace ${SOURCE_NS} --wait=false 2>/dev/null || true

    # Delete DR namespace (includes standby PVCs)
    kubectl --kubeconfig ${DR_KUBECONFIG} delete namespace ${DEST_NS} --wait=false 2>/dev/null || true

    # Clean up any leftover verify pods (named verify-data-*)
    for pod in $(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${DEST_NS} get pods -o name 2>/dev/null | grep "pod/verify-data-"); do
        kubectl --kubeconfig ${DR_KUBECONFIG} -n ${DEST_NS} delete ${pod} --force --grace-period=0 2>/dev/null || true
    done

    echo "Cleanup complete"
}

main() {
    echo "=== Test Case 25: Backup-Based PVC Sync ==="
    echo ""

    # Deploy resources
    if ! deploy_resources; then
        print_result "Resource deployment" "fail"
        cleanup
        exit 1
    fi
    print_result "Resource deployment" "pass"

    # Wait for replication to complete
    if ! wait_for_replication; then
        print_result "Replication completed" "fail"
        cleanup
        exit 1
    fi
    print_result "Replication completed" "pass"

    # Verify DR namespace was created
    if verify_resource "" "namespace" "${DEST_NS}"; then
        print_result "DR namespace created" "pass"
    else
        print_result "DR namespace created" "fail"
    fi

    # Verify base PVC was synced (resource sync, not data)
    if verify_resource "${DEST_NS}" "pvc" "${PVC_NAME}"; then
        print_result "Base PVC synced to DR" "pass"
    else
        print_result "Base PVC synced to DR" "fail"
    fi

    # Verify standby PVC was created with correct labels
    if verify_standby_pvc "${DEST_NS}" "${STANDBY_PVC_NAME}"; then
        print_result "Standby PVC created with correct labels" "pass"
    else
        print_result "Standby PVC created with correct labels" "fail"
    fi

    # Verify VolumeBackupOperation CRs were created
    if verify_backup_operations; then
        print_result "VolumeBackupOperation CRs created" "pass"
    else
        print_result "VolumeBackupOperation CRs created" "fail"
    fi

    # Verify data was restored to standby PVC (single pod for all checks)
    if create_verify_pod "${DEST_NS}" "${STANDBY_PVC_NAME}"; then
        if verify_standby_pvc_data "${DEST_NS}" "test-file.txt" "Backup E2E test data"; then
            print_result "Standby PVC data: test-file.txt" "pass"
        else
            print_result "Standby PVC data: test-file.txt" "fail"
        fi

        if verify_standby_pvc_data "${DEST_NS}" "backup-verify.txt" "Kopia backup sync verification"; then
            print_result "Standby PVC data: backup-verify.txt" "pass"
        else
            print_result "Standby PVC data: backup-verify.txt" "fail"
        fi

        if verify_standby_pvc_data "${DEST_NS}" "subdir/nested.txt" "Nested directory data"; then
            print_result "Standby PVC data: subdir/nested.txt" "pass"
        else
            print_result "Standby PVC data: subdir/nested.txt" "fail"
        fi

        delete_verify_pod "${DEST_NS}"
    else
        print_result "Standby PVC data: test-file.txt" "fail"
        print_result "Standby PVC data: backup-verify.txt" "fail"
        print_result "Standby PVC data: subdir/nested.txt" "fail"
    fi

    # Verify deployment was synced
    if verify_resource "${DEST_NS}" "deployment" "backup-test-app"; then
        print_result "Deployment synced to DR" "pass"

        # Verify deployment is scaled to zero
        local replicas=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n ${DEST_NS} get deployment backup-test-app -o jsonpath='{.spec.replicas}')
        if [ "$replicas" = "0" ]; then
            print_result "Deployment scaled to zero" "pass"
        else
            print_result "Deployment scaled to zero (replicas=$replicas)" "fail"
        fi
    else
        print_result "Deployment synced to DR" "fail"
    fi

    # Verify ConfigMap was synced
    if verify_resource "${DEST_NS}" "configmap" "backup-test-config"; then
        print_result "ConfigMap synced to DR" "pass"
    else
        print_result "ConfigMap synced to DR" "fail"
    fi

    # Verify NamespaceMapping status
    local phase=$(kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer get namespacemapping ${MAPPING_NAME} -o jsonpath='{.status.phase}')
    if [ "$phase" = "Completed" ]; then
        print_result "NamespaceMapping phase is Completed" "pass"
    else
        print_result "NamespaceMapping phase is Completed (got: $phase)" "fail"
    fi

    # Print summary
    echo ""
    echo -e "Test Summary:"
    echo "Total tests: ${TOTAL_TESTS}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"

    # Cleanup
    cleanup

    # Return exit code
    if [ ${FAILED_TESTS} -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

main
