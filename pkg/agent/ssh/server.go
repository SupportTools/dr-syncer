package ssh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// Server represents an SSH server instance
type Server struct {
	port     int
	keyPath  string
	hostKeys []string
}

// Port returns the SSH server port
func (s *Server) Port() int {
	return s.port
}

// NewServer creates a new SSH server instance
func NewServer(port int) (*Server, error) {
	keyPath := "/etc/ssh/keys"
	hostKeys := []string{
		filepath.Join(keyPath, "ssh_host_rsa_key"),
		filepath.Join(keyPath, "ssh_host_ecdsa_key"),
		filepath.Join(keyPath, "ssh_host_ed25519_key"),
	}

	// Verify host keys exist
	for _, key := range hostKeys {
		if _, err := os.Stat(key); err != nil {
			return nil, fmt.Errorf("host key not found: %s", key)
		}
	}

	// Verify at least one authorized_keys file exists
	// Check both the primary authorized_keys and the rsync_authorized_keys
	authKeysPath := filepath.Join(keyPath, "authorized_keys")
	rsyncAuthKeysPath := filepath.Join(keyPath, "rsync_authorized_keys")
	_, authKeysErr := os.Stat(authKeysPath)
	_, rsyncAuthKeysErr := os.Stat(rsyncAuthKeysPath)

	if authKeysErr != nil && rsyncAuthKeysErr != nil {
		return nil, fmt.Errorf("no authorized_keys found: checked %s and %s", authKeysPath, rsyncAuthKeysPath)
	}

	return &Server{
		port:     port,
		keyPath:  keyPath,
		hostKeys: hostKeys,
	}, nil
}

// Start starts the SSH server
func (s *Server) Start() error {
	// The actual SSH server is started by the container's entrypoint script
	// This is just a verification that everything is set up correctly
	return s.verifySetup()
}

// Stop stops the SSH server
func (s *Server) Stop() error {
	// The SSH server is managed by the container runtime
	return nil
}

// verifySetup checks that all required files and permissions are correct
func (s *Server) verifySetup() error {
	// Check host keys
	for _, key := range s.hostKeys {
		content, err := ioutil.ReadFile(key)
		if err != nil {
			return fmt.Errorf("failed to read host key %s: %v", key, err)
		}
		if len(content) == 0 {
			return fmt.Errorf("host key is empty: %s", key)
		}
	}

	// Check at least one authorized_keys file exists and is non-empty
	authKeysPath := filepath.Join(s.keyPath, "authorized_keys")
	rsyncAuthKeysPath := filepath.Join(s.keyPath, "rsync_authorized_keys")

	authKeysContent, authKeysErr := ioutil.ReadFile(authKeysPath)
	rsyncAuthKeysContent, rsyncAuthKeysErr := ioutil.ReadFile(rsyncAuthKeysPath)

	hasValidAuthKeys := authKeysErr == nil && len(authKeysContent) > 0
	hasValidRsyncAuthKeys := rsyncAuthKeysErr == nil && len(rsyncAuthKeysContent) > 0

	if !hasValidAuthKeys && !hasValidRsyncAuthKeys {
		return fmt.Errorf("no valid authorized_keys found: checked %s and %s", authKeysPath, rsyncAuthKeysPath)
	}

	// Check sshd_config
	if _, err := os.Stat("/etc/ssh/sshd_config"); err != nil {
		return fmt.Errorf("sshd_config not found: %v", err)
	}

	return nil
}
