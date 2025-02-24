#!/bin/bash
set -e

# Create host keys if they don't exist
if [ ! -f /etc/ssh/keys/ssh_host_rsa_key ]; then
    ssh-keygen -f /etc/ssh/keys/ssh_host_rsa_key -N '' -t rsa
fi
if [ ! -f /etc/ssh/keys/ssh_host_ecdsa_key ]; then
    ssh-keygen -f /etc/ssh/keys/ssh_host_ecdsa_key -N '' -t ecdsa
fi
if [ ! -f /etc/ssh/keys/ssh_host_ed25519_key ]; then
    ssh-keygen -f /etc/ssh/keys/ssh_host_ed25519_key -N '' -t ed25519
fi

# Set correct permissions
chmod 600 /etc/ssh/keys/ssh_host_*_key
chmod 644 /etc/ssh/keys/ssh_host_*_key.pub

# Update syncer's authorized_keys if provided
if [ -f /etc/ssh/keys/authorized_keys ]; then
    cp /etc/ssh/keys/authorized_keys /home/syncer/.ssh/authorized_keys
    chmod 600 /home/syncer/.ssh/authorized_keys
    chown syncer:syncer /home/syncer/.ssh/authorized_keys
fi

# Start sshd
exec /usr/sbin/sshd -D -e
