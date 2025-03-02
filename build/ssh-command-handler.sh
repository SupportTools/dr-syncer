#!/bin/bash
set -e

# Set PATH to ensure we can find all binaries in Ubuntu
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# Check if this is a test connection command
if [[ "${SSH_ORIGINAL_COMMAND}" == "test-connection" ]]; then
    echo "SSH proxy connection successful"
    exit 0

# Check if this is a proxy command
elif [[ "${SSH_ORIGINAL_COMMAND}" == proxy:* ]]; then
    # Extract the target from the command
    # Format: proxy:host:port
    IFS=':' read -r cmd target_host target_port <<< "${SSH_ORIGINAL_COMMAND}"
    
    if [[ -z "${target_host}" || -z "${target_port}" ]]; then
        echo "Error: Invalid proxy command format. Expected: proxy:host:port" >&2
        exit 1
    fi
    
    # Set up the proxy connection using netcat
    NETCAT_CMD=$(which nc 2>/dev/null || which netcat 2>/dev/null)
    if [[ -z "${NETCAT_CMD}" ]]; then
        echo "ERROR: netcat (nc) not found in PATH!" >&2
        exit 1
    fi
    
    # Using netcat to forward the connection
    exec ${NETCAT_CMD} -v "${target_host}" "${target_port}"
    
# Check if this is an rsync command
elif [[ "${SSH_ORIGINAL_COMMAND}" == rsync* ]]; then
    # Create data directory if it doesn't exist
    mkdir -p /data
    
    # Change to the /data directory to avoid any working directory issues
    cd /data
    
    # Check for rsync binary
    RSYNC_PATH=$(which rsync)
    if [[ ! -x "${RSYNC_PATH}" ]]; then
        echo "ERROR: rsync binary not found or not executable" >&2
        exit 1
    fi
    
    # Handle wildcards in path if present
    if [[ "${SSH_ORIGINAL_COMMAND}" == *"*"* ]]; then
        # Extract the PVC ID (the only consistent part of the path)
        PVC_ID=$(echo "${SSH_ORIGINAL_COMMAND}" | grep -o 'pvc-[a-z0-9-]*' || echo "")
        
        if [[ -n "${PVC_ID}" ]]; then
            # Find actual path on the system
            MOUNT_PATH=$(find /var/lib/kubelet/pods -path "*/volumes/kubernetes.io~csi/${PVC_ID}/mount" 2>/dev/null | head -1)
            
            if [[ -n "${MOUNT_PATH}" ]]; then
                # Replace wildcard path with actual path
                RESOLVED_CMD=$(echo "${SSH_ORIGINAL_COMMAND}" | sed -E "s|/var/lib/kubelet/pods/\*/volumes/kubernetes.io~csi/${PVC_ID}/mount|${MOUNT_PATH}|g")
                SSH_ORIGINAL_COMMAND="${RESOLVED_CMD}"
            fi
        fi
    fi
    
    # Extract arguments (everything after 'rsync')
    ARGS="${SSH_ORIGINAL_COMMAND#rsync}"
    
    # Execute rsync directly with the arguments
    # No logging, no wrapper scripts, no redirection
    eval "${RSYNC_PATH} ${ARGS}"
    exit $?
    
# Handle other commands - allow any command
else
    # Execute the command directly
    # No logging, no wrapper scripts
    eval ${SSH_ORIGINAL_COMMAND}
    exit $?
fi
