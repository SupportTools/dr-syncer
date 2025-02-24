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

	// Verify authorized_keys exists
	authKeysPath := filepath.Join(keyPath, "authorized_keys")
	if _, err := os.Stat(authKeysPath); err != nil {
		return nil, fmt.Errorf("authorized_keys not found: %s", authKeysPath)
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

	// Check authorized_keys
	authKeysPath := filepath.Join(s.keyPath, "authorized_keys")
	content, err := ioutil.ReadFile(authKeysPath)
	if err != nil {
		return fmt.Errorf("failed to read authorized_keys: %v", err)
	}
	if len(content) == 0 {
		return fmt.Errorf("authorized_keys is empty")
	}

	// Check sshd_config
	if _, err := os.Stat("/etc/ssh/sshd_config"); err != nil {
		return fmt.Errorf("sshd_config not found: %v", err)
	}

	return nil
}
