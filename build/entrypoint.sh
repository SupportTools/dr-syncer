#!/bin/bash
set -e

# Host keys are now provided by the controller in the secret
# No need to generate them in the container

# Copy host keys to a writable location and set correct permissions
mkdir -p /etc/ssh/host_keys
if [ -f /etc/ssh/keys/ssh_host_rsa_key ]; then
    cp /etc/ssh/keys/ssh_host_rsa_key /etc/ssh/host_keys/
    cp /etc/ssh/keys/ssh_host_rsa_key.pub /etc/ssh/host_keys/
    chmod 600 /etc/ssh/host_keys/ssh_host_rsa_key
    chmod 644 /etc/ssh/host_keys/ssh_host_rsa_key.pub
fi

if [ -f /etc/ssh/keys/ssh_host_ecdsa_key ]; then
    cp /etc/ssh/keys/ssh_host_ecdsa_key /etc/ssh/host_keys/
    cp /etc/ssh/keys/ssh_host_ecdsa_key.pub /etc/ssh/host_keys/
    chmod 600 /etc/ssh/host_keys/ssh_host_ecdsa_key
    chmod 644 /etc/ssh/host_keys/ssh_host_ecdsa_key.pub
fi

if [ -f /etc/ssh/keys/ssh_host_ed25519_key ]; then
    cp /etc/ssh/keys/ssh_host_ed25519_key /etc/ssh/host_keys/
    cp /etc/ssh/keys/ssh_host_ed25519_key.pub /etc/ssh/host_keys/
    chmod 600 /etc/ssh/host_keys/ssh_host_ed25519_key
    chmod 644 /etc/ssh/host_keys/ssh_host_ed25519_key.pub
fi

# Update syncer's authorized_keys if provided
if [ -f /etc/ssh/keys/authorized_keys ]; then
    cp /etc/ssh/keys/authorized_keys /home/syncer/.ssh/authorized_keys
    chmod 600 /home/syncer/.ssh/authorized_keys
    chown syncer:syncer /home/syncer/.ssh/authorized_keys
fi

# Install the SSH command handler
mkdir -p /usr/local/bin
cp /build/ssh-command-handler.sh /usr/local/bin/
chmod +x /usr/local/bin/ssh-command-handler.sh

# Create log directory for SSH command handler
mkdir -p /var/log
touch /var/log/ssh-command-handler.log
chmod 644 /var/log/ssh-command-handler.log

# Install netcat for proxy functionality
apk add --no-cache netcat-openbsd

# Start sshd
exec /usr/sbin/sshd -D -e
