#!/bin/bash
# k3d helper functions for E2E tests
# This file should be sourced by other scripts

# Ensure common.sh is loaded
if [[ -z "${PROJECT_ROOT:-}" ]]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    # shellcheck source=./common.sh
    source "${SCRIPT_DIR}/common.sh"
fi

# k3d configuration
K3D_NETWORK="${K3D_NETWORK:-k3d-dr-syncer}"
K3D_REGISTRY_NAME="${K3D_REGISTRY_NAME:-k3d-registry.localhost}"
K3D_REGISTRY_PORT="${K3D_REGISTRY_PORT:-5000}"

# Cluster configurations
declare -A K3D_CLUSTERS=(
    [controller]="6443"
    [prod]="6444"
    [dr]="6445"
)

# Check if Docker is running
check_docker() {
    if ! docker info &>/dev/null; then
        log_error "Docker is not running. Please start Docker first."
        return 1
    fi
    log_debug "Docker is running"
    return 0
}

# Create shared Docker network for inter-cluster communication
create_k3d_network() {
    log_info "Creating Docker network: $K3D_NETWORK"

    if docker network inspect "$K3D_NETWORK" &>/dev/null; then
        log_info "Network $K3D_NETWORK already exists"
        return 0
    fi

    if docker network create "$K3D_NETWORK"; then
        log_success "Created network: $K3D_NETWORK"
        return 0
    else
        log_error "Failed to create network: $K3D_NETWORK"
        return 1
    fi
}

# Delete Docker network
delete_k3d_network() {
    log_info "Removing Docker network: $K3D_NETWORK"

    if ! docker network inspect "$K3D_NETWORK" &>/dev/null; then
        log_info "Network $K3D_NETWORK does not exist"
        return 0
    fi

    if docker network rm "$K3D_NETWORK" 2>/dev/null; then
        log_success "Removed network: $K3D_NETWORK"
        return 0
    else
        log_warn "Could not remove network: $K3D_NETWORK (may still be in use)"
        return 0
    fi
}

# Check if a k3d cluster exists
cluster_exists() {
    local name="$1"
    k3d cluster list 2>/dev/null | grep -q "^$name "
}

# Create a single k3d cluster
create_k3d_cluster() {
    local name="$1"
    local api_port="$2"
    local extra_args="${3:-}"

    log_info "Creating k3d cluster: $name (API port: $api_port)"

    if cluster_exists "$name"; then
        log_info "Cluster $name already exists"
        return 0
    fi

    local cmd="k3d cluster create $name \
        --network $K3D_NETWORK \
        --api-port $api_port \
        --servers 1 \
        --agents 2 \
        --k3s-arg '--disable=traefik@server:0' \
        --wait"

    # Add extra args if provided
    if [[ -n "$extra_args" ]]; then
        cmd="$cmd $extra_args"
    fi

    log_debug "Running: $cmd"

    if eval "$cmd"; then
        log_success "Created cluster: $name"
        return 0
    else
        log_error "Failed to create cluster: $name"
        return 1
    fi
}

# Create all k3d clusters
create_all_k3d_clusters() {
    section "Creating k3d Clusters"

    check_docker || return 1
    create_k3d_network || return 1

    # Create controller cluster (no extra ports needed)
    create_k3d_cluster "controller" "${K3D_CLUSTERS[controller]}" || return 1

    # Create prod cluster with SSH port for rsync testing
    create_k3d_cluster "prod" "${K3D_CLUSTERS[prod]}" \
        "-p '2222:2222@loadbalancer'" || return 1

    # Create dr cluster with SSH port
    create_k3d_cluster "dr" "${K3D_CLUSTERS[dr]}" \
        "-p '2223:2222@loadbalancer'" || return 1

    log_success "All k3d clusters created successfully"
    return 0
}

# Delete a single k3d cluster
delete_k3d_cluster() {
    local name="$1"

    log_info "Deleting k3d cluster: $name"

    if ! cluster_exists "$name"; then
        log_info "Cluster $name does not exist"
        return 0
    fi

    if k3d cluster delete "$name"; then
        log_success "Deleted cluster: $name"
        return 0
    else
        log_error "Failed to delete cluster: $name"
        return 1
    fi
}

# Delete all k3d clusters
delete_all_k3d_clusters() {
    section "Deleting k3d Clusters"

    for cluster in "${!K3D_CLUSTERS[@]}"; do
        delete_k3d_cluster "$cluster"
    done

    # Try to remove network after clusters are deleted
    delete_k3d_network

    log_success "Cleanup complete"
    return 0
}

# Export kubeconfigs from k3d clusters
export_kubeconfigs() {
    subsection "Exporting Kubeconfigs"

    ensure_kubeconfig_dir

    for cluster in "${!K3D_CLUSTERS[@]}"; do
        local kubeconfig_file="${KUBECONFIG_DIR}/${cluster}"
        log_info "Exporting kubeconfig for $cluster to $kubeconfig_file"

        if k3d kubeconfig get "$cluster" > "$kubeconfig_file"; then
            chmod 600 "$kubeconfig_file"
            log_success "Exported: $kubeconfig_file"
        else
            log_error "Failed to export kubeconfig for: $cluster"
            return 1
        fi
    done

    log_success "All kubeconfigs exported"
    return 0
}

# Load Docker image into k3d cluster
load_image_to_cluster() {
    local image="$1"
    local cluster="$2"

    log_info "Loading image $image into cluster $cluster"

    if k3d image import "$image" -c "$cluster"; then
        log_debug "Loaded $image into $cluster"
        return 0
    else
        log_error "Failed to load $image into $cluster"
        return 1
    fi
}

# Load Docker images into all clusters
load_images_to_all_clusters() {
    local images=("$@")

    subsection "Loading Images into Clusters"

    for cluster in "${!K3D_CLUSTERS[@]}"; do
        for image in "${images[@]}"; do
            load_image_to_cluster "$image" "$cluster" || return 1
        done
    done

    log_success "All images loaded"
    return 0
}

# Wait for all nodes in a cluster to be ready
wait_for_cluster_ready() {
    local cluster="$1"
    local kubeconfig="${KUBECONFIG_DIR}/${cluster}"
    local timeout="${2:-120}"

    log_info "Waiting for cluster $cluster to be ready"

    wait_for "cluster $cluster nodes ready" \
        "kubectl --kubeconfig '$kubeconfig' get nodes -o jsonpath='{.items[*].status.conditions[?(@.type==\"Ready\")].status}' | grep -v False" \
        "$timeout" 5

    return $?
}

# Wait for all clusters to be ready
wait_for_all_clusters_ready() {
    subsection "Waiting for Clusters to be Ready"

    for cluster in "${!K3D_CLUSTERS[@]}"; do
        wait_for_cluster_ready "$cluster" || return 1
    done

    log_success "All clusters are ready"
    return 0
}

# Get internal Docker container name for a cluster
get_cluster_container_name() {
    local cluster="$1"
    echo "k3d-${cluster}-server-0"
}

# Get internal Docker IP address for a cluster
# This is required because Kubernetes pods cannot resolve Docker container hostnames
get_cluster_internal_ip() {
    local cluster="$1"
    local container_name
    container_name=$(get_cluster_container_name "$cluster")

    # Get the IP address from Docker
    local ip
    ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container_name" 2>/dev/null)

    if [[ -z "$ip" ]]; then
        log_error "Could not get IP for container: $container_name"
        return 1
    fi

    echo "$ip"
}

# Get cluster API server URL (internal Docker network using IP)
get_cluster_internal_api() {
    local cluster="$1"
    local ip
    ip=$(get_cluster_internal_ip "$cluster") || return 1
    echo "https://${ip}:6443"
}

# Patch kubeconfig to use internal Docker IP addresses
# This is needed for controller to access prod/dr clusters within Docker network
# Kubernetes pods cannot resolve Docker container hostnames, so we must use IPs
create_internal_kubeconfig() {
    local cluster="$1"
    local output_file="$2"
    local internal_ip

    internal_ip=$(get_cluster_internal_ip "$cluster")
    if [[ -z "$internal_ip" ]]; then
        log_error "Failed to get internal IP for cluster: $cluster"
        return 1
    fi

    log_info "Creating internal kubeconfig for $cluster (IP: $internal_ip)"

    # Get original kubeconfig and replace server URL with IP address
    k3d kubeconfig get "$cluster" | \
        sed "s|https://0.0.0.0:[0-9]*|https://${internal_ip}:6443|g" > "$output_file"

    chmod 600 "$output_file"
    log_debug "Created internal kubeconfig: $output_file"
}

# List all k3d clusters
list_k3d_clusters() {
    subsection "k3d Cluster Status"
    k3d cluster list
}

# Get cluster status summary
get_cluster_status() {
    local cluster="$1"
    local kubeconfig="${KUBECONFIG_DIR}/${cluster}"

    if ! cluster_exists "$cluster"; then
        echo "not created"
        return
    fi

    if kubectl --kubeconfig "$kubeconfig" cluster-info &>/dev/null; then
        local nodes
        nodes=$(kubectl --kubeconfig "$kubeconfig" get nodes --no-headers 2>/dev/null | wc -l)
        local ready
        ready=$(kubectl --kubeconfig "$kubeconfig" get nodes --no-headers 2>/dev/null | grep -c " Ready " || echo "0")
        echo "ready ($ready/$nodes nodes)"
    else
        echo "not accessible"
    fi
}

# Print cluster status for all clusters
print_cluster_status() {
    subsection "Cluster Status"

    for cluster in "${!K3D_CLUSTERS[@]}"; do
        local status
        status=$(get_cluster_status "$cluster")
        log_info "$cluster: $status"
    done
}
