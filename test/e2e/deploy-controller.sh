#!/bin/bash
# deploy-controller.sh - Build and deploy DR-Syncer controller for E2E testing
#
# This script:
# 1. Builds Docker images locally (controller, agent, rsync)
# 2. Loads images into k3d clusters
# 3. Deploys the controller via Helm
# 4. Creates kubeconfig secrets for remote clusters
# 5. Applies RemoteCluster and ClusterMapping CRDs
#
# Usage:
#   ./deploy-controller.sh [options]
#
# Options:
#   --skip-build      Don't rebuild Docker images
#   --skip-load       Don't load images into clusters (use registry)
#   --use-registry    Use local registry instead of k3d image import
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
SKIP_BUILD="${SKIP_BUILD:-false}"
SKIP_LOAD="${SKIP_LOAD:-false}"
USE_REGISTRY="${USE_REGISTRY:-false}"

# Image configuration
IMAGE_TAG="${IMAGE_TAG:-e2e-test}"
CONTROLLER_IMAGE="dr-syncer:${IMAGE_TAG}"
AGENT_IMAGE="dr-syncer-agent:${IMAGE_TAG}"
RSYNC_IMAGE="dr-syncer-rsync:${IMAGE_TAG}"

# Namespace
DR_SYNCER_NAMESPACE="dr-syncer"

usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Build and deploy DR-Syncer controller for E2E testing."
    echo ""
    echo "Options:"
    echo "  --skip-build      Don't rebuild Docker images"
    echo "  --skip-load       Don't load images into clusters"
    echo "  --use-registry    Use local registry instead of k3d image import"
    echo "  --image-tag TAG   Image tag to use (default: e2e-test)"
    echo "  --help            Show this help message"
    usage_footer
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --skip-build)
                SKIP_BUILD="true"
                shift
                ;;
            --skip-load)
                SKIP_LOAD="true"
                shift
                ;;
            --use-registry)
                USE_REGISTRY="true"
                shift
                ;;
            --image-tag)
                IMAGE_TAG="$2"
                CONTROLLER_IMAGE="dr-syncer:${IMAGE_TAG}"
                AGENT_IMAGE="dr-syncer-agent:${IMAGE_TAG}"
                RSYNC_IMAGE="dr-syncer-rsync:${IMAGE_TAG}"
                shift 2
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            --debug|--no-cleanup|--timeout)
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

# Build Docker images
build_images() {
    if [[ "$SKIP_BUILD" == "true" ]]; then
        log_info "Skipping image build (--skip-build)"
        return 0
    fi

    subsection "Building Docker Images"

    cd "$PROJECT_ROOT"

    log_info "Building controller image: $CONTROLLER_IMAGE"
    docker build -t "$CONTROLLER_IMAGE" -f build/Dockerfile . || {
        log_error "Failed to build controller image"
        return 1
    }
    log_success "Built: $CONTROLLER_IMAGE"

    log_info "Building agent image: $AGENT_IMAGE"
    docker build -t "$AGENT_IMAGE" -f build/Dockerfile.agent . || {
        log_error "Failed to build agent image"
        return 1
    }
    log_success "Built: $AGENT_IMAGE"

    log_info "Building rsync image: $RSYNC_IMAGE"
    docker build -t "$RSYNC_IMAGE" -f build/Dockerfile.rsync . || {
        log_error "Failed to build rsync image"
        return 1
    }
    log_success "Built: $RSYNC_IMAGE"

    log_success "All images built successfully"
}

# Load images into k3d clusters
load_images() {
    if [[ "$SKIP_LOAD" == "true" ]]; then
        log_info "Skipping image load (--skip-load)"
        return 0
    fi

    load_images_to_all_clusters "$CONTROLLER_IMAGE" "$AGENT_IMAGE" "$RSYNC_IMAGE"
}

# Create dr-syncer namespace
create_namespace() {
    subsection "Creating Namespace"

    if kubectl_controller get namespace "$DR_SYNCER_NAMESPACE" &>/dev/null; then
        log_info "Namespace $DR_SYNCER_NAMESPACE already exists"
        return 0
    fi

    kubectl_controller create namespace "$DR_SYNCER_NAMESPACE"
    log_success "Created namespace: $DR_SYNCER_NAMESPACE"
}

# Create kubeconfig secrets for remote clusters
create_kubeconfig_secrets() {
    subsection "Creating Kubeconfig Secrets"

    # Create internal kubeconfigs (using Docker network hostnames)
    local internal_kubeconfig_dir="${E2E_DIR}/internal-kubeconfigs"
    mkdir -p "$internal_kubeconfig_dir"

    # Generate internal kubeconfigs for prod and dr clusters
    for cluster in prod dr; do
        create_internal_kubeconfig "$cluster" "${internal_kubeconfig_dir}/${cluster}"
    done

    # Create secrets in controller cluster
    for cluster in prod dr; do
        local secret_name="${cluster}"
        local kubeconfig_file="${internal_kubeconfig_dir}/${cluster}"

        if kubectl_controller -n "$DR_SYNCER_NAMESPACE" get secret "$secret_name" &>/dev/null; then
            log_info "Deleting existing secret: $secret_name"
            kubectl_controller -n "$DR_SYNCER_NAMESPACE" delete secret "$secret_name"
        fi

        kubectl_controller -n "$DR_SYNCER_NAMESPACE" create secret generic "$secret_name" \
            --from-file=kubeconfig="$kubeconfig_file"
        log_success "Created secret: $secret_name"
    done

    # Cleanup internal kubeconfigs
    rm -rf "$internal_kubeconfig_dir"

    log_success "All kubeconfig secrets created"
}

# Deploy controller via Helm
deploy_helm_chart() {
    subsection "Deploying Controller via Helm"

    cd "$PROJECT_ROOT"

    # Prepare Helm values
    local helm_values=""
    helm_values+="image.tag=${IMAGE_TAG},"
    helm_values+="image.pullPolicy=Never,"
    helm_values+="image.repository=dr-syncer,"
    helm_values+="agent.image.tag=${IMAGE_TAG},"
    helm_values+="agent.image.pullPolicy=Never,"
    helm_values+="agent.image.repository=dr-syncer-agent,"
    helm_values+="rsyncPod.image.tag=${IMAGE_TAG},"
    helm_values+="rsyncPod.image.pullPolicy=Never,"
    helm_values+="rsyncPod.image.repository=dr-syncer-rsync,"
    helm_values+="controller.logLevel=debug,"
    helm_values+="controller.enableLeaderElection=false,"
    helm_values+="crds.install=true"

    log_info "Deploying Helm chart..."
    KUBECONFIG="$CONTROLLER_KUBECONFIG" helm upgrade --install dr-syncer charts/dr-syncer \
        --namespace "$DR_SYNCER_NAMESPACE" \
        --create-namespace \
        --set "$helm_values" \
        --wait \
        --timeout 5m

    log_success "Helm chart deployed"
}

# Wait for controller to be ready
wait_for_controller() {
    subsection "Waiting for Controller"

    wait_for "controller deployment ready" \
        "kubectl_controller -n $DR_SYNCER_NAMESPACE get deployment dr-syncer -o jsonpath='{.status.readyReplicas}' | grep -q '1'" \
        120

    log_success "Controller is ready"
}

# Apply RemoteCluster and ClusterMapping CRDs
apply_remote_clusters() {
    subsection "Applying RemoteCluster and ClusterMapping CRDs"

    # Create a modified version of remote-clusters.yaml for k3d
    local e2e_remote_clusters="${E2E_DIR}/e2e-remote-clusters.yaml"

    # Generate RemoteCluster and ClusterMapping for E2E
    cat > "$e2e_remote_clusters" <<EOF
---
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: prod
  namespace: ${DR_SYNCER_NAMESPACE}
spec:
  kubeconfigSecretRef:
    name: prod
    namespace: ${DR_SYNCER_NAMESPACE}
  defaultSchedule: "*/1 * * * *"
  defaultResourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
    - ingresses
  pvcSync:
    enabled: true
    image:
      repository: dr-syncer-agent
      tag: ${IMAGE_TAG}
      pullPolicy: Never
    ssh:
      port: 2222
    concurrency: 3
    deployment:
      hostNetwork: true
      nodeSelector:
        kubernetes.io/os: linux
      tolerations:
        - key: "node-role.kubernetes.io/control-plane"
          operator: "Exists"
          effect: "NoSchedule"
      resources:
        limits:
          cpu: "200m"
          memory: "256Mi"
        requests:
          cpu: "100m"
          memory: "128Mi"
      privileged: true
---
apiVersion: dr-syncer.io/v1alpha1
kind: RemoteCluster
metadata:
  name: dr
  namespace: ${DR_SYNCER_NAMESPACE}
spec:
  kubeconfigSecretRef:
    name: dr
    namespace: ${DR_SYNCER_NAMESPACE}
  defaultSchedule: "*/1 * * * *"
  defaultResourceTypes:
    - configmaps
    - secrets
    - deployments
    - services
    - ingresses
  pvcSync:
    enabled: true
    image:
      repository: dr-syncer-agent
      tag: ${IMAGE_TAG}
      pullPolicy: Never
    ssh:
      port: 2222
    concurrency: 3
    deployment:
      hostNetwork: true
      nodeSelector:
        kubernetes.io/os: linux
      tolerations:
        - key: "node-role.kubernetes.io/control-plane"
          operator: "Exists"
          effect: "NoSchedule"
      resources:
        limits:
          cpu: "200m"
          memory: "256Mi"
        requests:
          cpu: "100m"
          memory: "128Mi"
      privileged: true
---
apiVersion: dr-syncer.io/v1alpha1
kind: ClusterMapping
metadata:
  name: prod-to-dr
  namespace: ${DR_SYNCER_NAMESPACE}
spec:
  sourceCluster: prod
  targetCluster: dr
  verifyConnectivity: true
  connectivityTimeoutSeconds: 60
---
apiVersion: dr-syncer.io/v1alpha1
kind: ClusterMapping
metadata:
  name: dr-to-prod
  namespace: ${DR_SYNCER_NAMESPACE}
spec:
  sourceCluster: dr
  targetCluster: prod
  verifyConnectivity: true
  connectivityTimeoutSeconds: 60
EOF

    kubectl_controller apply -f "$e2e_remote_clusters"
    log_success "Applied RemoteCluster and ClusterMapping CRDs"

    # Wait for RemoteClusters to be ready
    log_info "Waiting for RemoteClusters to be healthy..."
    sleep 10  # Give controller time to process

    for cluster in prod dr; do
        local attempts=0
        local max_attempts=30
        while [[ $attempts -lt $max_attempts ]]; do
            # Check if ClusterAvailable condition is True (indicates connectivity)
            local available
            available=$(kubectl_controller -n "$DR_SYNCER_NAMESPACE" get remotecluster "$cluster" \
                -o jsonpath='{.status.conditions[?(@.type=="ClusterAvailable")].status}' 2>/dev/null || echo "Unknown")
            if [[ "$available" == "True" ]]; then
                log_success "RemoteCluster $cluster is connected and available"
                break
            fi
            attempts=$((attempts + 1))
            log_debug "Waiting for RemoteCluster $cluster... ($attempts/$max_attempts)"
            sleep 5
        done
        if [[ $attempts -eq $max_attempts ]]; then
            log_warn "RemoteCluster $cluster may not be fully available yet"
        fi
    done
}

# Print deployment summary
print_summary() {
    section "Deployment Summary"

    log_info "Controller Status:"
    kubectl_controller -n "$DR_SYNCER_NAMESPACE" get pods -l app.kubernetes.io/name=dr-syncer

    echo ""
    log_info "RemoteClusters:"
    kubectl_controller -n "$DR_SYNCER_NAMESPACE" get remoteclusters

    echo ""
    log_info "ClusterMappings:"
    kubectl_controller -n "$DR_SYNCER_NAMESPACE" get clustermappings
}

main() {
    local start_time
    start_time=$(date +%s)

    parse_args "$@"

    section "DR-Syncer Controller Deployment"

    log_info "Starting controller deployment..."
    log_info "Image tag: $IMAGE_TAG"
    log_info "Skip build: $SKIP_BUILD"
    log_info "Skip load: $SKIP_LOAD"

    # Verify clusters are accessible
    verify_all_clusters || exit 1

    # Build images
    build_images || exit 1

    # Load images into clusters
    load_images || exit 1

    # Create namespace
    create_namespace || exit 1

    # Create kubeconfig secrets
    create_kubeconfig_secrets || exit 1

    # Deploy Helm chart
    deploy_helm_chart || exit 1

    # Wait for controller
    wait_for_controller || exit 1

    # Apply RemoteCluster and ClusterMapping
    apply_remote_clusters || exit 1

    # Print summary
    print_summary

    local duration
    duration=$(get_duration "$start_time")

    section "Deployment Complete"
    log_success "Controller deployed successfully (took $duration)"
    echo ""
    log_info "Next steps:"
    log_info "  1. Run E2E tests: ./run-e2e.sh"
    log_info "  2. View logs:     kubectl --kubeconfig $CONTROLLER_KUBECONFIG -n $DR_SYNCER_NAMESPACE logs -l app.kubernetes.io/name=dr-syncer -f"
}

main "$@"
