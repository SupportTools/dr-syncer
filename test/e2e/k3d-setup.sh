#!/bin/bash
# k3d-setup.sh - Create k3d clusters for E2E testing
#
# This script creates three k3d clusters for DR-Syncer E2E testing:
# - controller: Runs the DR-Syncer operator
# - prod: Source cluster (simulates production)
# - dr: Destination cluster (simulates DR site)
#
# Usage:
#   ./k3d-setup.sh [options]
#
# Options:
#   --skip-wait    Don't wait for clusters to be fully ready
#   --debug        Enable debug output
#   --help         Show this help message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source helper libraries
# shellcheck source=./lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"
# shellcheck source=./lib/k3d.sh
source "${SCRIPT_DIR}/lib/k3d.sh"

# Script-specific options
SKIP_WAIT="${SKIP_WAIT:-false}"

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Create k3d clusters for DR-Syncer E2E testing."
    echo ""
    echo "Options:"
    echo "  --skip-wait    Don't wait for clusters to be fully ready"
    echo "  --help         Show this help message"
    usage_footer
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --skip-wait)
                SKIP_WAIT="true"
                shift
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            --debug|--no-cleanup|--timeout)
                # Let common parser handle these
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

main() {
    local start_time
    start_time=$(date +%s)

    parse_args "$@"

    section "DR-Syncer E2E Cluster Setup"

    log_info "Starting k3d cluster setup..."
    log_info "Debug mode: $E2E_DEBUG"
    log_info "Skip wait: $SKIP_WAIT"

    # Check prerequisites
    check_prerequisites || exit 1

    # Create all clusters
    create_all_k3d_clusters || exit 1

    # Export kubeconfigs
    export_kubeconfigs || exit 1

    # Wait for clusters if not skipped
    if [[ "$SKIP_WAIT" != "true" ]]; then
        wait_for_all_clusters_ready || exit 1
    fi

    # Verify connectivity
    verify_all_clusters || exit 1

    # Print status
    print_cluster_status

    local duration
    duration=$(get_duration "$start_time")

    section "Setup Complete"
    log_success "All k3d clusters are ready (took $duration)"
    echo ""
    log_info "Kubeconfigs exported to: $KUBECONFIG_DIR"
    log_info "  - controller: $CONTROLLER_KUBECONFIG"
    log_info "  - prod:       $PROD_KUBECONFIG"
    log_info "  - dr:         $DR_KUBECONFIG"
    echo ""
    log_info "Next steps:"
    log_info "  1. Deploy controller: ./deploy-controller.sh"
    log_info "  2. Run E2E tests:     ./run-e2e.sh"
    log_info "  3. Cleanup:           ./k3d-teardown.sh"
}

main "$@"
