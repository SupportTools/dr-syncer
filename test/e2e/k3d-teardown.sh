#!/bin/bash
# k3d-teardown.sh - Delete k3d clusters used for E2E testing
#
# This script removes all k3d clusters created for DR-Syncer E2E testing.
#
# Usage:
#   ./k3d-teardown.sh [options]
#
# Options:
#   --keep-kubeconfigs    Don't delete kubeconfig files
#   --force               Force cleanup even if some operations fail
#   --debug               Enable debug output
#   --help                Show this help message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source helper libraries
# shellcheck source=./lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"
# shellcheck source=./lib/k3d.sh
source "${SCRIPT_DIR}/lib/k3d.sh"

# Script-specific options
KEEP_KUBECONFIGS="${KEEP_KUBECONFIGS:-false}"
FORCE="${FORCE:-false}"

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Delete k3d clusters used for DR-Syncer E2E testing."
    echo ""
    echo "Options:"
    echo "  --keep-kubeconfigs    Don't delete kubeconfig files"
    echo "  --force               Force cleanup even if some operations fail"
    echo "  --help                Show this help message"
    usage_footer
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --keep-kubeconfigs)
                KEEP_KUBECONFIGS="true"
                shift
                ;;
            --force)
                FORCE="true"
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

cleanup_kubeconfigs() {
    if [[ "$KEEP_KUBECONFIGS" == "true" ]]; then
        log_info "Keeping kubeconfig files (--keep-kubeconfigs)"
        return 0
    fi

    subsection "Cleaning up Kubeconfigs"

    for file in "$CONTROLLER_KUBECONFIG" "$PROD_KUBECONFIG" "$DR_KUBECONFIG"; do
        if [[ -f "$file" ]]; then
            rm -f "$file"
            log_info "Removed: $file"
        fi
    done

    log_success "Kubeconfigs cleaned up"
}

main() {
    local start_time
    start_time=$(date +%s)

    parse_args "$@"

    section "DR-Syncer E2E Cluster Teardown"

    log_info "Starting k3d cluster teardown..."
    log_info "Keep kubeconfigs: $KEEP_KUBECONFIGS"
    log_info "Force: $FORCE"

    # Print current status
    print_cluster_status

    # Set error handling based on force flag
    if [[ "$FORCE" == "true" ]]; then
        set +e
    fi

    # Delete all clusters
    delete_all_k3d_clusters

    # Cleanup kubeconfigs
    cleanup_kubeconfigs

    # Restore error handling
    if [[ "$FORCE" == "true" ]]; then
        set -e
    fi

    local duration
    duration=$(get_duration "$start_time")

    section "Teardown Complete"
    log_success "All k3d clusters removed (took $duration)"
}

main "$@"
