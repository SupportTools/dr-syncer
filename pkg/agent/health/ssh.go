package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// TestCommand is the command to run to test SSH connectivity
	TestCommand = "echo dr-syncer-ssh-test"

	// ProxyTestCommand is the command to run to test SSH proxy connectivity
	ProxyTestCommand = "ssh-command-handler.sh test-connection"
)

// SSHHealthChecker handles SSH connectivity verification
type SSHHealthChecker struct {
	client client.Client
}

// CheckNodeSSHProxyHealth verifies SSH proxy connectivity to a node
func (h *SSHHealthChecker) CheckNodeSSHProxyHealth(ctx context.Context, rc *drv1alpha1.RemoteCluster,
	nodeName, podIP string) (*drv1alpha1.SSHConnectionStatus, error) {

	status := &drv1alpha1.SSHConnectionStatus{
		LastCheckTime: &metav1.Time{Time: time.Now()},
		Connected:     false,
	}

	// Get SSH key secret
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil || rc.Spec.PVCSync.SSH.KeySecretRef == nil {
		status.Error = "PVCSync SSH configuration not found"
		return status, fmt.Errorf(status.Error)
	}

	// Get SSH port
	port := int32(22)
	if rc.Spec.PVCSync.SSH.Port > 0 {
		port = rc.Spec.PVCSync.SSH.Port
	}

	// Get SSH key secret
	var secret corev1.Secret
	err := h.client.Get(ctx, client.ObjectKey{
		Namespace: rc.Spec.PVCSync.SSH.KeySecretRef.Namespace,
		Name:      rc.Spec.PVCSync.SSH.KeySecretRef.Name,
	}, &secret)

	if err != nil {
		status.Error = fmt.Sprintf("Failed to get SSH key secret: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Get private key
	privateKeyBytes, ok := secret.Data["ssh-private-key"]
	if !ok {
		status.Error = "SSH private key not found in secret"
		return status, fmt.Errorf(status.Error)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		status.Error = fmt.Sprintf("Failed to parse SSH private key: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Get timeout from context or use default
	timeout := DefaultSSHTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	// Create SSH client config
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	// Connect to SSH server
	hostport := net.JoinHostPort(podIP, strconv.Itoa(int(port)))
	sshClient, err := ssh.Dial("tcp", hostport, config)
	if err != nil {
		status.Error = fmt.Sprintf("Failed to connect to SSH server: %v", err)
		return status, fmt.Errorf(status.Error)
	}
	defer sshClient.Close()

	// Create session
	session, err := sshClient.NewSession()
	if err != nil {
		status.Error = fmt.Sprintf("Failed to create SSH session: %v", err)
		return status, fmt.Errorf(status.Error)
	}
	defer session.Close()

	// Run proxy test command
	var output []byte
	outputPipe, err := session.StdoutPipe()
	if err != nil {
		status.Error = fmt.Sprintf("Failed to get SSH stdout pipe: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Start command
	if err := session.Start(ProxyTestCommand); err != nil {
		status.Error = fmt.Sprintf("Failed to start SSH proxy command: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Read output with timeout
	outputChan := make(chan []byte)
	errChan := make(chan error)

	go func() {
		output, err := io.ReadAll(outputPipe)
		if err != nil {
			errChan <- err
			return
		}
		outputChan <- output
	}()

	// Wait for command to complete with timeout (half of the SSH timeout)
	commandTimeout := timeout / 2
	if commandTimeout < time.Second {
		commandTimeout = time.Second
	}

	select {
	case output = <-outputChan:
		// Command completed successfully
	case err = <-errChan:
		status.Error = fmt.Sprintf("Failed to read SSH proxy command output: %v", err)
		return status, fmt.Errorf(status.Error)
	case <-time.After(commandTimeout):
		status.Error = "SSH proxy command timed out"
		return status, fmt.Errorf(status.Error)
	}

	// Wait for command to exit
	if err := session.Wait(); err != nil {
		status.Error = fmt.Sprintf("SSH proxy command failed: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Verify output contains success message
	if string(output) != "SSH proxy connection successful\n" {
		status.Error = fmt.Sprintf("Unexpected SSH proxy command output: %s", string(output))
		return status, fmt.Errorf(status.Error)
	}

	// SSH proxy connection successful
	status.Connected = true
	status.Error = ""

	return status, nil
}

// NewSSHHealthChecker creates a new SSH health checker
func NewSSHHealthChecker(client client.Client) *SSHHealthChecker {
	return &SSHHealthChecker{
		client: client,
	}
}

// CheckNodeSSHHealth verifies SSH connectivity to a node
func (h *SSHHealthChecker) CheckNodeSSHHealth(ctx context.Context, rc *drv1alpha1.RemoteCluster,
	nodeName, podIP string) (*drv1alpha1.SSHConnectionStatus, error) {

	status := &drv1alpha1.SSHConnectionStatus{
		LastCheckTime: &metav1.Time{Time: time.Now()},
		Connected:     false,
	}

	// Get SSH key secret
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil || rc.Spec.PVCSync.SSH.KeySecretRef == nil {
		status.Error = "PVCSync SSH configuration not found"
		return status, fmt.Errorf(status.Error)
	}

	// Get SSH port
	port := int32(22)
	if rc.Spec.PVCSync.SSH.Port > 0 {
		port = rc.Spec.PVCSync.SSH.Port
	}

	// Get SSH key secret
	var secret corev1.Secret
	err := h.client.Get(ctx, client.ObjectKey{
		Namespace: rc.Spec.PVCSync.SSH.KeySecretRef.Namespace,
		Name:      rc.Spec.PVCSync.SSH.KeySecretRef.Name,
	}, &secret)

	if err != nil {
		status.Error = fmt.Sprintf("Failed to get SSH key secret: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Get private key
	privateKeyBytes, ok := secret.Data["ssh-private-key"]
	if !ok {
		status.Error = "SSH private key not found in secret"
		return status, fmt.Errorf(status.Error)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		status.Error = fmt.Sprintf("Failed to parse SSH private key: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Get timeout from context or use default
	timeout := DefaultSSHTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	// Create SSH client config
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	// Connect to SSH server
	hostport := net.JoinHostPort(podIP, strconv.Itoa(int(port)))
	sshClient, err := ssh.Dial("tcp", hostport, config)
	if err != nil {
		status.Error = fmt.Sprintf("Failed to connect to SSH server: %v", err)
		return status, fmt.Errorf(status.Error)
	}
	defer sshClient.Close()

	// Create session
	session, err := sshClient.NewSession()
	if err != nil {
		status.Error = fmt.Sprintf("Failed to create SSH session: %v", err)
		return status, fmt.Errorf(status.Error)
	}
	defer session.Close()

	// Run test command
	var output []byte
	outputPipe, err := session.StdoutPipe()
	if err != nil {
		status.Error = fmt.Sprintf("Failed to get SSH stdout pipe: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Start command
	if err := session.Start(TestCommand); err != nil {
		status.Error = fmt.Sprintf("Failed to start SSH command: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Read output with timeout
	outputChan := make(chan []byte)
	errChan := make(chan error)

	go func() {
		output, err := io.ReadAll(outputPipe)
		if err != nil {
			errChan <- err
			return
		}
		outputChan <- output
	}()

	// Wait for command to complete with timeout (half of the SSH timeout)
	commandTimeout := timeout / 2
	if commandTimeout < time.Second {
		commandTimeout = time.Second
	}

	select {
	case output = <-outputChan:
		// Command completed successfully
	case err = <-errChan:
		status.Error = fmt.Sprintf("Failed to read SSH command output: %v", err)
		return status, fmt.Errorf(status.Error)
	case <-time.After(commandTimeout):
		status.Error = "SSH command timed out"
		return status, fmt.Errorf(status.Error)
	}

	// Wait for command to exit
	if err := session.Wait(); err != nil {
		status.Error = fmt.Sprintf("SSH command failed: %v", err)
		return status, fmt.Errorf(status.Error)
	}

	// Verify output
	if string(output) != "dr-syncer-ssh-test\n" {
		status.Error = fmt.Sprintf("Unexpected SSH command output: %s", string(output))
		return status, fmt.Errorf(status.Error)
	}

	// SSH connection successful
	status.Connected = true
	status.Error = ""

	return status, nil
}
