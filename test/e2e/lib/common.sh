#!/bin/bash
# Common utilities for E2E tests
# This file should be sourced by other scripts

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Project root directory (use LIB_DIR to avoid conflicts with sourcing scripts)
LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$LIB_DIR")"
TEST_DIR="$(dirname "$E2E_DIR")"
PROJECT_ROOT="$(dirname "$TEST_DIR")"

# Default kubeconfig paths
KUBECONFIG_DIR="${PROJECT_ROOT}/kubeconfig"
CONTROLLER_KUBECONFIG="${KUBECONFIG_DIR}/controller"
PROD_KUBECONFIG="${KUBECONFIG_DIR}/prod"
DR_KUBECONFIG="${KUBECONFIG_DIR}/dr"

# Export kubeconfig paths for test scripts
export CONTROLLER_KUBECONFIG
export PROD_KUBECONFIG
export DR_KUBECONFIG

# Default settings
E2E_CLEANUP="${E2E_CLEANUP:-true}"
E2E_DEBUG="${E2E_DEBUG:-false}"
E2E_TIMEOUT="${E2E_TIMEOUT:-600}"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

log_debug() {
    if [[ "$E2E_DEBUG" == "true" ]]; then
        echo -e "${YELLOW}[DEBUG]${NC} $*"
    fi
}

# Print section header
section() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $*${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

# Print subsection header
subsection() {
    echo ""
    echo -e "${YELLOW}───────────────────────────────────────────────────────────────${NC}"
    echo -e "${YELLOW}  $*${NC}"
    echo -e "${YELLOW}───────────────────────────────────────────────────────────────${NC}"
}

# Check if a command exists
check_command() {
    local cmd="$1"
    if ! command -v "$cmd" &>/dev/null; then
        log_error "Required command not found: $cmd"
        return 1
    fi
    log_debug "Found command: $cmd ($(command -v "$cmd"))"
    return 0
}

# Check all required commands
check_prerequisites() {
    section "Checking Prerequisites"
    local missing=0

    for cmd in docker kubectl helm k3d; do
        if ! check_command "$cmd"; then
            missing=$((missing + 1))
        fi
    done

    if [[ $missing -gt 0 ]]; then
        log_error "Missing $missing required command(s). Please install them first."
        return 1
    fi

    log_success "All prerequisites found"
    return 0
}

# Wait for a condition with timeout
wait_for() {
    local description="$1"
    local check_cmd="$2"
    local timeout="${3:-$E2E_TIMEOUT}"
    local interval="${4:-5}"

    log_info "Waiting for: $description (timeout: ${timeout}s)"

    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        if eval "$check_cmd" &>/dev/null; then
            log_success "$description - ready"
            return 0
        fi
        sleep "$interval"
        elapsed=$((elapsed + interval))
        if [[ $((elapsed % 30)) -eq 0 ]]; then
            log_debug "Still waiting for $description... (${elapsed}s elapsed)"
        fi
    done

    log_error "Timeout waiting for: $description"
    return 1
}

# Run command with optional debug output
run_cmd() {
    local description="$1"
    shift

    log_debug "Running: $*"

    if [[ "$E2E_DEBUG" == "true" ]]; then
        if "$@"; then
            log_debug "$description - completed"
            return 0
        else
            log_error "$description - failed"
            return 1
        fi
    else
        if "$@" &>/dev/null; then
            return 0
        else
            return 1
        fi
    fi
}

# Kubectl wrapper for specific cluster
kubectl_controller() {
    kubectl --kubeconfig "$CONTROLLER_KUBECONFIG" "$@"
}

kubectl_prod() {
    kubectl --kubeconfig "$PROD_KUBECONFIG" "$@"
}

kubectl_dr() {
    kubectl --kubeconfig "$DR_KUBECONFIG" "$@"
}

# Verify cluster connectivity
verify_cluster() {
    local name="$1"
    local kubeconfig="$2"

    log_info "Verifying cluster connectivity: $name"

    if ! kubectl --kubeconfig "$kubeconfig" cluster-info &>/dev/null; then
        log_error "Cannot connect to cluster: $name"
        return 1
    fi

    local server
    server=$(kubectl --kubeconfig "$kubeconfig" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
    log_success "Connected to $name at $server"
    return 0
}

# Verify all three clusters
verify_all_clusters() {
    subsection "Verifying Cluster Connectivity"

    verify_cluster "controller" "$CONTROLLER_KUBECONFIG" || return 1
    verify_cluster "prod" "$PROD_KUBECONFIG" || return 1
    verify_cluster "dr" "$DR_KUBECONFIG" || return 1

    log_success "All clusters accessible"
    return 0
}

# Get test duration
get_duration() {
    local start="$1"
    local end="${2:-$(date +%s)}"
    local duration=$((end - start))

    if [[ $duration -ge 60 ]]; then
        echo "$((duration / 60))m $((duration % 60))s"
    else
        echo "${duration}s"
    fi
}

# Create kubeconfig directory if needed
ensure_kubeconfig_dir() {
    if [[ ! -d "$KUBECONFIG_DIR" ]]; then
        log_info "Creating kubeconfig directory: $KUBECONFIG_DIR"
        mkdir -p "$KUBECONFIG_DIR"
    fi
}

# Parse common arguments
parse_common_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --no-cleanup)
                E2E_CLEANUP="false"
                shift
                ;;
            --debug)
                E2E_DEBUG="true"
                shift
                ;;
            --timeout)
                E2E_TIMEOUT="$2"
                shift 2
                ;;
            *)
                # Let calling script handle unknown args
                break
                ;;
        esac
    done
}

# Print usage footer
usage_footer() {
    echo ""
    echo "Common Options:"
    echo "  --no-cleanup    Don't cleanup resources after test"
    echo "  --debug         Enable debug output"
    echo "  --timeout N     Set timeout in seconds (default: $E2E_TIMEOUT)"
}
