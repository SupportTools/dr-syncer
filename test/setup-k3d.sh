#!/bin/bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Helper functions
info() {
    echo -e "${YELLOW}[INFO] $1${NC}"
}

fail() {
    echo -e "${RED}[FAIL] $1${NC}"
    exit 1
}

success() {
    echo -e "${GREEN}[SUCCESS] $1${NC}"
}

# Create k3d clusters
info "Creating test clusters..."

# Create production cluster
info "Creating production cluster..."
k3d cluster create dr-syncer-prod --servers 1 --agents 1 --wait || fail "Failed to create production cluster"

# Create DR cluster
info "Creating DR cluster..."
k3d cluster create dr-syncer-dr --servers 1 --agents 1 --wait || fail "Failed to create DR cluster"

# Create controller cluster
info "Creating controller cluster..."
k3d cluster create dr-syncer-controller --servers 1 --agents 1 --wait || fail "Failed to create controller cluster"

# Set kubeconfig paths
PROD_KUBECONFIG="/tmp/dr-syncer-prod.kubeconfig"
DR_KUBECONFIG="/tmp/dr-syncer-dr.kubeconfig"
CONTROLLER_KUBECONFIG="/tmp/dr-syncer-controller.kubeconfig"

# Export kubeconfigs
k3d kubeconfig get dr-syncer-prod > "${PROD_KUBECONFIG}"
k3d kubeconfig get dr-syncer-dr > "${DR_KUBECONFIG}"
k3d kubeconfig get dr-syncer-controller > "${CONTROLLER_KUBECONFIG}"

# Print environment variables
echo
echo "Test environment ready"
echo "Use the following environment variables:"
echo "  PROD_KUBECONFIG=${PROD_KUBECONFIG}"
echo "  DR_KUBECONFIG=${DR_KUBECONFIG}"
echo "  CONTROLLER_KUBECONFIG=${CONTROLLER_KUBECONFIG}"

# Export variables
export PROD_KUBECONFIG DR_KUBECONFIG CONTROLLER_KUBECONFIG
