#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Set kubeconfig paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROD_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/prod"
DR_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/dr"
CONTROLLER_KUBECONFIG="${PROJECT_ROOT}/kubeconfig/controller"

# Function to check required kubeconfig files
check_environment() {
    local missing_files=()
    
    # Verify kubeconfig files exist
    for config in "${PROD_KUBECONFIG}" "${DR_KUBECONFIG}" "${CONTROLLER_KUBECONFIG}"; do
        if [ ! -f "${config}" ]; then
            missing_files+=("${config}")
        fi
    done
    
    if [ ${#missing_files[@]} -ne 0 ]; then
        echo -e "${RED}Error: Missing required kubeconfig files:${NC}"
        printf '%s\n' "${missing_files[@]}"
        echo
        echo "Please ensure the following kubeconfig files exist:"
        echo "  ${PROD_KUBECONFIG}: Production cluster kubeconfig"
        echo "  ${DR_KUBECONFIG}: DR cluster kubeconfig"
        echo "  ${CONTROLLER_KUBECONFIG}: Controller cluster kubeconfig"
        exit 1
    fi
    
    # Verify cluster access
    echo "Verifying cluster access..."
    
    if ! kubectl --kubeconfig "${PROD_KUBECONFIG}" cluster-info &>/dev/null; then
        echo -e "${RED}Error: Cannot access production cluster${NC}"
        exit 1
    fi
    
    if ! kubectl --kubeconfig "${DR_KUBECONFIG}" cluster-info &>/dev/null; then
        echo -e "${RED}Error: Cannot access DR cluster${NC}"
        exit 1
    fi
    
    if ! kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" cluster-info &>/dev/null; then
        echo -e "${RED}Error: Cannot access controller cluster${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ Environment verified${NC}"
}

# Function to create the dr-syncer namespace
create_namespace() {
    echo "Creating dr-syncer namespace if it doesn't exist..."
    
    if ! kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" get namespace dr-syncer &>/dev/null; then
        kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" create namespace dr-syncer
        echo -e "${GREEN}✓ Created dr-syncer namespace${NC}"
    else
        echo -e "${GREEN}✓ dr-syncer namespace already exists${NC}"
    fi
}

# Function to create kubeconfig secrets
create_kubeconfig_secrets() {
    echo "Creating kubeconfig secrets..."
    
    # Create nyc3 kubeconfig secret
    if ! kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer get secret dr-syncer-nyc3-kubeconfig &>/dev/null; then
        kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer create secret generic dr-syncer-nyc3-kubeconfig \
            --from-file=kubeconfig="${PROD_KUBECONFIG}"
        echo -e "${GREEN}✓ Created dr-syncer-nyc3-kubeconfig secret${NC}"
    else
        # Update the secret if it already exists
        kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer create secret generic dr-syncer-nyc3-kubeconfig \
            --from-file=kubeconfig="${PROD_KUBECONFIG}" --dry-run=client -o yaml | \
            kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" apply -f -
        echo -e "${GREEN}✓ Updated dr-syncer-nyc3-kubeconfig secret${NC}"
    fi
    
    # Create sfo3 kubeconfig secret
    if ! kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer get secret dr-syncer-sfo3-kubeconfig &>/dev/null; then
        kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer create secret generic dr-syncer-sfo3-kubeconfig \
            --from-file=kubeconfig="${DR_KUBECONFIG}"
        echo -e "${GREEN}✓ Created dr-syncer-sfo3-kubeconfig secret${NC}"
    else
        # Update the secret if it already exists
        kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer create secret generic dr-syncer-sfo3-kubeconfig \
            --from-file=kubeconfig="${DR_KUBECONFIG}" --dry-run=client -o yaml | \
            kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" apply -f -
        echo -e "${GREEN}✓ Updated dr-syncer-sfo3-kubeconfig secret${NC}"
    fi
}

# Note: SSH key generation is handled by the controller itself
# The controller will create and manage the pvc-syncer-agent-keys secret

# Function to apply remote-clusters.yaml
apply_remote_clusters() {
    echo "Applying remote-clusters.yaml..."
    
    # Apply the remote-clusters.yaml file with the RemoteCluster resources in the dr-syncer namespace
    kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" apply -f "${SCRIPT_DIR}/remote-clusters.yaml" -n dr-syncer
    
    echo -e "${GREEN}✓ Applied remote-clusters.yaml${NC}"
}

# Function to verify setup
verify_setup() {
    echo "Verifying setup..."
    
    # Check RemoteClusters in the dr-syncer namespace
    if kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer get remoteclusters dr-syncer-nyc3 &>/dev/null && \
       kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer get remoteclusters dr-syncer-sfo3 &>/dev/null; then
        echo -e "${GREEN}✓ RemoteClusters created successfully${NC}"
    else
        echo -e "${RED}✗ RemoteClusters not found${NC}"
        return 1
    fi
    
    # Check ClusterMapping
    if kubectl --kubeconfig "${CONTROLLER_KUBECONFIG}" -n dr-syncer get clustermapping nyc3-to-sfo3 &>/dev/null; then
        echo -e "${GREEN}✓ ClusterMapping created successfully${NC}"
    else
        echo -e "${RED}✗ ClusterMapping not found${NC}"
        return 1
    fi
    
    echo -e "${GREEN}✓ Setup verified successfully${NC}"
    return 0
}

# Main function
main() {
    echo -e "${YELLOW}Setting up test clusters for DR Syncer...${NC}"
    
    # Check environment
    check_environment
    
    # Create namespace
    create_namespace
    
    # Create kubeconfig secrets
    create_kubeconfig_secrets
    
    # Apply remote-clusters.yaml
    apply_remote_clusters
    
    # Verify setup
    verify_setup
    
    echo -e "\n${GREEN}✓ Test clusters setup completed successfully${NC}"
}

# Execute main function
main "$@"
