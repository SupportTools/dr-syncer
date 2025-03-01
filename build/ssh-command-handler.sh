#!/bin/bash
set -e

# Log function for debugging
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> /var/log/ssh-command-handler.log
}

log "Command received: ${SSH_ORIGINAL_COMMAND}"

# Check if this is a test connection command
if [[ "${SSH_ORIGINAL_COMMAND}" == "test-connection" ]]; then
    log "Executing test connection command"
    echo "SSH proxy connection successful"
    exit 0

# Check if this is a proxy command
elif [[ "${SSH_ORIGINAL_COMMAND}" == proxy:* ]]; then
    # Extract the target from the command
    # Format: proxy:host:port
    IFS=':' read -r cmd target_host target_port <<< "${SSH_ORIGINAL_COMMAND}"
    
    if [[ -z "${target_host}" || -z "${target_port}" ]]; then
        log "Error: Invalid proxy command format. Expected: proxy:host:port"
        echo "Error: Invalid proxy command format. Expected: proxy:host:port" >&2
        exit 1
    fi
    
    log "Setting up proxy to ${target_host}:${target_port}"
    
    # Set up the proxy connection
    # This uses netcat to forward the connection
    exec nc -v "${target_host}" "${target_port}"
    
# Check if this is an rsync command
elif [[ "${SSH_ORIGINAL_COMMAND}" == rsync* ]]; then
    log "Executing rsync command"
    exec /usr/bin/rsync ${SSH_ORIGINAL_COMMAND}
    
# Handle unknown commands
else
    log "Error: Unsupported command: ${SSH_ORIGINAL_COMMAND}"
    echo "Error: Only test-connection, rsync, and proxy commands are supported" >&2
    exit 1
fi
