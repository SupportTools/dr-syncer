#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Function to create test cluster
create_test_cluster() {
    local name=$1
    local context="kind-${name}"
    
    echo -e "${YELLOW}Creating test cluster: ${name}${NC}"
    
    echo "Creating cluster with kind..."
    if ! kind create cluster --name "${name}" --wait 60s; then
        echo -e "${RED}Failed to create cluster: ${name}${NC}"
        return 1
    fi
    
    echo "Verifying cluster access..."
    if ! kubectl --context "${context}" cluster-info; then
        echo -e "${RED}Failed to verify cluster access: ${name}${NC}"
        return 1
    fi
    
    echo "Waiting for cluster nodes to be ready..."
    if ! kubectl --context "${context}" wait --for=condition=Ready nodes --all --timeout=120s; then
        echo -e "${RED}Failed waiting for nodes to be ready: ${name}${NC}"
        return 1
    fi
    
    # Get kubeconfig
    kind get kubeconfig --name "${name}" > "/tmp/${name}.kubeconfig"
    
    echo -e "${GREEN}✓ Cluster ${name} ready${NC}"
}

# Function to delete test cluster
delete_test_cluster() {
    local name=$1
    
    echo -e "${YELLOW}Deleting test cluster: ${name}${NC}"
    
    if ! kind delete cluster --name "${name}" &>/dev/null; then
        echo -e "${RED}Failed to delete cluster: ${name}${NC}"
        return 1
    fi
    
    rm -f "/tmp/${name}.kubeconfig"
    
    echo -e "${GREEN}✓ Cluster ${name} deleted${NC}"
}

# Set kubeconfig paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROD_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/prod"
DR_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/dr"
CONTROLLER_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/controller"

# Function to setup test environment
setup_environment() {
    # Create test clusters
    create_test_cluster "dr-syncer-prod" || return 1
    create_test_cluster "dr-syncer-dr" || return 1
    create_test_cluster "dr-syncer-controller" || return 1
    
    # Create kubeconfig directory if it doesn't exist
    mkdir -p "${PROJECT_ROOT}/kubeconfig"
    
    # Copy kubeconfig files to project directory
    cp "/tmp/dr-syncer-prod.kubeconfig" "${PROD_KUBECONFIG}"
    cp "/tmp/dr-syncer-dr.kubeconfig" "${DR_KUBECONFIG}"
    cp "/tmp/dr-syncer-controller.kubeconfig" "${CONTROLLER_KUBECONFIG}"
    
    echo -e "\n${GREEN}Test environment ready${NC}"
    echo "Kubeconfig files copied to project directory:"
    echo "  PROD_KUBECONFIG=${PROD_KUBECONFIG}"
    echo "  DR_KUBECONFIG=${DR_KUBECONFIG}"
    echo "  CONTROLLER_KUBECONFIG=${CONTROLLER_KUBECONFIG}"
}

# Function to cleanup test environment
cleanup_environment() {
    echo -e "\n${YELLOW}Cleaning up test environment...${NC}"
    
    delete_test_cluster "dr-syncer-prod"
    delete_test_cluster "dr-syncer-dr"
    delete_test_cluster "dr-syncer-controller"
    
    # Remove kubeconfig files from project directory
    rm -f "${PROD_KUBECONFIG}" "${DR_KUBECONFIG}" "${CONTROLLER_KUBECONFIG}"
    
    echo -e "${GREEN}✓ Test environment cleaned up${NC}"
}

# Function to ensure kind network exists
ensure_kind_network() {
    echo "Ensuring kind network exists..."
    if ! docker network inspect kind &>/dev/null; then
        echo "Creating kind network..."
        if ! docker network create kind; then
            echo -e "${RED}Error: Failed to create kind network${NC}"
            return 1
        fi
    fi
    echo -e "${GREEN}✓ Kind network ready${NC}"
    return 0
}

# Function to check prerequisites
check_prerequisites() {
    # Check if Docker is running
    if ! docker info &>/dev/null; then
        echo -e "${RED}Error: Docker is not running${NC}"
        echo "Please start Docker and try again"
        return 1
    fi
    
    # Ensure kind network exists
    if ! ensure_kind_network; then
        return 1
    fi
    
    # Check if kind is installed
    if ! command -v kind &>/dev/null; then
        echo -e "${RED}Error: kind is not installed${NC}"
        echo "Please install kind: https://kind.sigs.k8s.io/docs/user/quick-start/"
        return 1
    fi
    
    # Check if kubectl is installed
    if ! command -v kubectl &>/dev/null; then
        echo -e "${RED}Error: kubectl is not installed${NC}"
        echo "Please install kubectl: https://kubernetes.io/docs/tasks/tools/install-kubectl/"
        return 1
    fi
    
    return 0
}

# Main function
main() {
    # Check prerequisites
    if ! check_prerequisites; then
        exit 1
    fi
    
    # Parse command line arguments
    local action="setup"
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --cleanup)
                action="cleanup"
                shift
                ;;
            *)
                echo "Usage: $0 [--cleanup]"
                exit 1
                ;;
        esac
    done
    
    # Execute requested action
    case $action in
        setup)
            setup_environment
            ;;
        cleanup)
            cleanup_environment
            ;;
    esac
}

# Execute main function
main "$@"
