#!/bin/bash
# run-e2e.sh - Main E2E test orchestrator for DR-Syncer
#
# This script orchestrates the full E2E test lifecycle:
# 1. Creates k3d clusters (if needed)
# 2. Builds and deploys controller
# 3. Runs integration tests from test/cases/
# 4. Cleans up clusters (unless --no-cleanup)
#
# Usage:
#   ./run-e2e.sh [options]
#
# Options:
#   --skip-setup      Skip cluster creation (use existing clusters)
#   --skip-deploy     Skip controller deployment
#   --skip-cleanup    Don't cleanup after tests
#   --test <number>   Run only a specific test case (e.g., --test 00)
#   --debug           Enable debug output
#   --help            Show this help message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source helper libraries
# shellcheck source=./lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"
# shellcheck source=./lib/k3d.sh
source "${SCRIPT_DIR}/lib/k3d.sh"

# Script-specific options
SKIP_SETUP="${SKIP_SETUP:-false}"
SKIP_DEPLOY="${SKIP_DEPLOY:-false}"
SKIP_CLEANUP="${SKIP_CLEANUP:-false}"
SPECIFIC_TEST="${SPECIFIC_TEST:-}"

# Test results
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Main E2E test orchestrator for DR-Syncer."
    echo ""
    echo "Options:"
    echo "  --skip-setup      Skip cluster creation (use existing clusters)"
    echo "  --skip-deploy     Skip controller deployment"
    echo "  --no-cleanup      Don't cleanup clusters after tests"
    echo "  --test <number>   Run only a specific test case (e.g., --test 00)"
    echo "  --help            Show this help message"
    usage_footer
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --skip-setup)
                SKIP_SETUP="true"
                shift
                ;;
            --skip-deploy)
                SKIP_DEPLOY="true"
                shift
                ;;
            --no-cleanup)
                SKIP_CLEANUP="true"
                E2E_CLEANUP="false"
                shift
                ;;
            --test)
                SPECIFIC_TEST="$2"
                shift 2
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            --debug|--timeout)
                parse_common_args "$@"
                break
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Setup k3d clusters
setup_clusters() {
    if [[ "$SKIP_SETUP" == "true" ]]; then
        log_info "Skipping cluster setup (--skip-setup)"
        return 0
    fi

    section "Setting up k3d Clusters"

    if [[ "$E2E_DEBUG" == "true" ]]; then
        "${SCRIPT_DIR}/k3d-setup.sh" --debug
    else
        "${SCRIPT_DIR}/k3d-setup.sh"
    fi
}

# Deploy controller
deploy_controller() {
    if [[ "$SKIP_DEPLOY" == "true" ]]; then
        log_info "Skipping controller deployment (--skip-deploy)"
        return 0
    fi

    section "Deploying Controller"

    if [[ "$E2E_DEBUG" == "true" ]]; then
        "${SCRIPT_DIR}/deploy-controller.sh" --debug
    else
        "${SCRIPT_DIR}/deploy-controller.sh"
    fi
}

# Cleanup test namespaces from previous runs
cleanup_test_namespaces() {
    subsection "Cleaning up test namespaces"

    for kubeconfig in "$PROD_KUBECONFIG" "$DR_KUBECONFIG"; do
        local namespaces
        namespaces=$(kubectl --kubeconfig "$kubeconfig" get namespaces -o name 2>/dev/null | grep "dr-sync-test" || true)
        if [[ -n "$namespaces" ]]; then
            log_info "Deleting test namespaces..."
            echo "$namespaces" | xargs -r kubectl --kubeconfig "$kubeconfig" delete --wait=false 2>/dev/null || true
        fi
    done

    # Also cleanup any NamespaceMappings from controller cluster
    kubectl_controller -n dr-syncer delete namespacemappings --all --wait=false 2>/dev/null || true

    # Wait for namespaces to be fully deleted to avoid race conditions
    log_info "Waiting for namespace deletion to complete..."
    local max_wait=60
    local waited=0
    while [[ $waited -lt $max_wait ]]; do
        local remaining=""
        for kubeconfig in "$PROD_KUBECONFIG" "$DR_KUBECONFIG"; do
            local ns
            ns=$(kubectl --kubeconfig "$kubeconfig" get namespaces -o name 2>/dev/null | grep "dr-sync-test" || true)
            if [[ -n "$ns" ]]; then
                remaining="$remaining $ns"
            fi
        done

        if [[ -z "$remaining" ]]; then
            log_success "Test namespaces cleaned up"
            return 0
        fi

        sleep 2
        waited=$((waited + 2))
        if [[ $((waited % 10)) -eq 0 ]]; then
            log_info "Still waiting for namespace deletion... (${waited}s)"
        fi
    done

    log_warning "Timeout waiting for namespace deletion, continuing anyway"
}

# Run a single test case
run_test_case() {
    local test_dir="$1"
    local test_name
    test_name=$(basename "$test_dir")

    TOTAL_TESTS=$((TOTAL_TESTS + 1))

    subsection "Test: $test_name"

    # Check if test.sh exists
    if [[ ! -f "${test_dir}/test.sh" ]]; then
        log_warn "No test.sh found in $test_dir, skipping"
        SKIPPED_TESTS=$((SKIPPED_TESTS + 1))
        return 0
    fi

    # Run the test
    local test_start
    test_start=$(date +%s)

    if [[ "$E2E_DEBUG" == "true" ]]; then
        if bash "${test_dir}/test.sh"; then
            local duration
            duration=$(get_duration "$test_start")
            log_success "PASSED: $test_name ($duration)"
            PASSED_TESTS=$((PASSED_TESTS + 1))
            return 0
        else
            local duration
            duration=$(get_duration "$test_start")
            log_error "FAILED: $test_name ($duration)"
            FAILED_TESTS=$((FAILED_TESTS + 1))
            return 1
        fi
    else
        if bash "${test_dir}/test.sh" &>/dev/null; then
            local duration
            duration=$(get_duration "$test_start")
            log_success "PASSED: $test_name ($duration)"
            PASSED_TESTS=$((PASSED_TESTS + 1))
            return 0
        else
            local duration
            duration=$(get_duration "$test_start")
            log_error "FAILED: $test_name ($duration)"
            FAILED_TESTS=$((FAILED_TESTS + 1))
            return 1
        fi
    fi
}

# Run all test cases
run_tests() {
    section "Running E2E Tests"

    local test_cases_dir="${TEST_DIR}/cases"

    if [[ ! -d "$test_cases_dir" ]]; then
        log_error "Test cases directory not found: $test_cases_dir"
        return 1
    fi

    # Cleanup previous test namespaces
    cleanup_test_namespaces

    # Get list of test directories
    local test_dirs
    if [[ -n "$SPECIFIC_TEST" ]]; then
        # Run specific test
        test_dirs=$(find "$test_cases_dir" -maxdepth 1 -type d -name "${SPECIFIC_TEST}*" | sort)
        if [[ -z "$test_dirs" ]]; then
            log_error "Test case not found: $SPECIFIC_TEST"
            return 1
        fi
    else
        # Run all tests
        test_dirs=$(find "$test_cases_dir" -maxdepth 1 -type d -name "[0-9]*" | sort)
    fi

    local test_failed=0

    for test_dir in $test_dirs; do
        if ! run_test_case "$test_dir"; then
            test_failed=1
            # Continue running other tests even if one fails
        fi
    done

    return $test_failed
}

# Print test summary
print_test_summary() {
    section "Test Summary"

    echo ""
    log_info "Total:   $TOTAL_TESTS"
    log_success "Passed:  $PASSED_TESTS"
    if [[ $FAILED_TESTS -gt 0 ]]; then
        log_error "Failed:  $FAILED_TESTS"
    else
        log_info "Failed:  $FAILED_TESTS"
    fi
    if [[ $SKIPPED_TESTS -gt 0 ]]; then
        log_warn "Skipped: $SKIPPED_TESTS"
    fi
    echo ""

    if [[ $FAILED_TESTS -gt 0 ]]; then
        log_error "Some tests failed!"
        return 1
    else
        log_success "All tests passed!"
        return 0
    fi
}

# Cleanup clusters
cleanup() {
    if [[ "$SKIP_CLEANUP" == "true" ]] || [[ "$E2E_CLEANUP" == "false" ]]; then
        log_info "Skipping cleanup (--no-cleanup)"
        log_info "To cleanup manually, run: ./k3d-teardown.sh"
        return 0
    fi

    section "Cleaning up"

    "${SCRIPT_DIR}/k3d-teardown.sh" --force
}

# Collect logs on failure
collect_logs() {
    if [[ $FAILED_TESTS -eq 0 ]]; then
        return 0
    fi

    subsection "Collecting Logs"

    local logs_dir="${E2E_DIR}/logs"
    mkdir -p "$logs_dir"

    # Controller logs
    kubectl_controller -n dr-syncer logs -l app.kubernetes.io/name=dr-syncer --tail=1000 \
        > "${logs_dir}/controller.log" 2>&1 || true

    # Controller events
    kubectl_controller -n dr-syncer get events --sort-by='.lastTimestamp' \
        > "${logs_dir}/controller-events.log" 2>&1 || true

    # RemoteCluster status
    kubectl_controller -n dr-syncer get remoteclusters -o yaml \
        > "${logs_dir}/remoteclusters.yaml" 2>&1 || true

    # ClusterMapping status
    kubectl_controller -n dr-syncer get clustermappings -o yaml \
        > "${logs_dir}/clustermappings.yaml" 2>&1 || true

    # NamespaceMapping status
    kubectl_controller -n dr-syncer get namespacemappings -o yaml \
        > "${logs_dir}/namespacemappings.yaml" 2>&1 || true

    log_info "Logs collected in: $logs_dir"
}

main() {
    local start_time
    start_time=$(date +%s)
    local exit_code=0

    parse_args "$@"

    section "DR-Syncer E2E Test Suite"

    log_info "Starting E2E tests..."
    log_info "Skip setup:   $SKIP_SETUP"
    log_info "Skip deploy:  $SKIP_DEPLOY"
    log_info "Skip cleanup: $SKIP_CLEANUP"
    log_info "Specific test: ${SPECIFIC_TEST:-all}"
    log_info "Debug mode:   $E2E_DEBUG"

    # Check prerequisites
    check_prerequisites || exit 1

    # Setup clusters
    setup_clusters || exit 1

    # Deploy controller
    deploy_controller || exit 1

    # Run tests
    if ! run_tests; then
        exit_code=1
    fi

    # Collect logs if there were failures
    collect_logs

    # Print summary
    print_test_summary || exit_code=1

    # Cleanup
    cleanup

    local duration
    duration=$(get_duration "$start_time")

    section "E2E Tests Complete"
    if [[ $exit_code -eq 0 ]]; then
        log_success "All E2E tests completed successfully (took $duration)"
    else
        log_error "E2E tests completed with failures (took $duration)"
    fi

    exit $exit_code
}

main "$@"
