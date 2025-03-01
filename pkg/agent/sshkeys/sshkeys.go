package sshkeys

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DefaultKeyBits is the default number of bits for RSA keys
	DefaultKeyBits = 2048

	// DefaultKeyRotationInterval is the default interval for key rotation
	DefaultKeyRotationInterval = 24 * time.Hour

	// DefaultAgentKeySecretName is the default name for the agent key secret
	DefaultAgentKeySecretName = "dr-syncer-agent-key"

	// DefaultTempKeySecretName is the default name for the temporary pod key secret
	DefaultTempKeySecretName = "dr-syncer-temp-key"

	// DefaultKeyAnnotationPrefix is the default prefix for key annotations
	DefaultKeyAnnotationPrefix = "dr-syncer.io/key-"

	// DefaultPrivateKeyName is the default name for the private key in secrets
	DefaultPrivateKeyName = "id_rsa"

	// DefaultPublicKeyName is the default name for the public key in secrets
	DefaultPublicKeyName = "id_rsa.pub"

	// DefaultAuthorizedKeysName is the default name for the authorized_keys file in secrets
	DefaultAuthorizedKeysName = "authorized_keys"
)

// KeyPair represents an SSH key pair
type KeyPair struct {
	// PrivateKey is the PEM encoded private key
	PrivateKey []byte

	// PublicKey is the SSH public key
	PublicKey []byte

	// AuthorizedKeys is the authorized_keys file content
	AuthorizedKeys []byte

	// Fingerprint is the SSH key fingerprint
	Fingerprint string

	// CreatedAt is the time the key was created
	CreatedAt time.Time
}

// KeyManager manages SSH keys
type KeyManager struct {
	// Client is the Kubernetes client
	Client kubernetes.Interface
}

// NewKeyManager creates a new key manager
func NewKeyManager(client kubernetes.Interface) *KeyManager {
	return &KeyManager{
		Client: client,
	}
}

// GenerateKeyPair generates a new SSH key pair
func GenerateKeyPair(bits int) (*KeyPair, error) {
	if bits <= 0 {
		bits = DefaultKeyBits
	}

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %v", err)
	}

	// Convert private key to PEM
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	// Generate public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate public key: %v", err)
	}

	// Get public key in authorized_keys format
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	// Get fingerprint
	fingerprint := ssh.FingerprintSHA256(publicKey)

	return &KeyPair{
		PrivateKey:     privateKeyBytes,
		PublicKey:      publicKeyBytes,
		AuthorizedKeys: publicKeyBytes,
		Fingerprint:    fingerprint,
		CreatedAt:      time.Now(),
	}, nil
}

// CreateAgentKeySecret creates a secret for the agent SSH key
func (m *KeyManager) CreateAgentKeySecret(ctx context.Context, namespace string, keyPair *KeyPair) error {
	// Create secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultAgentKeySecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "dr-syncer",
				"component": "agent-key",
			},
			Annotations: map[string]string{
				DefaultKeyAnnotationPrefix + "fingerprint": keyPair.Fingerprint,
				DefaultKeyAnnotationPrefix + "created-at":  keyPair.CreatedAt.Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			DefaultPrivateKeyName:     keyPair.PrivateKey,
			DefaultPublicKeyName:      keyPair.PublicKey,
			DefaultAuthorizedKeysName: keyPair.AuthorizedKeys,
		},
	}

	// Create or update secret
	_, err := m.Client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing secret
			_, err = m.Client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update agent key secret: %v", err)
			}
			log.WithFields(map[string]interface{}{
				"namespace":   namespace,
				"fingerprint": keyPair.Fingerprint,
			}).Info("Updated agent key secret")
			return nil
		}
		return fmt.Errorf("failed to create agent key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   namespace,
		"fingerprint": keyPair.Fingerprint,
	}).Info("Created agent key secret")

	return nil
}

// CreateTempKeySecret creates a secret for the temporary pod SSH key
func (m *KeyManager) CreateTempKeySecret(ctx context.Context, namespace, name string, keyPair *KeyPair) error {
	secretName := DefaultTempKeySecretName
	if name != "" {
		secretName = fmt.Sprintf("%s-%s", DefaultTempKeySecretName, name)
	}

	// Create secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "dr-syncer",
				"component": "temp-key",
			},
			Annotations: map[string]string{
				DefaultKeyAnnotationPrefix + "fingerprint": keyPair.Fingerprint,
				DefaultKeyAnnotationPrefix + "created-at":  keyPair.CreatedAt.Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			DefaultPrivateKeyName:     keyPair.PrivateKey,
			DefaultPublicKeyName:      keyPair.PublicKey,
			DefaultAuthorizedKeysName: keyPair.AuthorizedKeys,
		},
	}

	// Create or update secret
	_, err := m.Client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing secret
			_, err = m.Client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update temp key secret: %v", err)
			}
			log.WithFields(map[string]interface{}{
				"namespace":   namespace,
				"name":        secretName,
				"fingerprint": keyPair.Fingerprint,
			}).Info("Updated temp key secret")
			return nil
		}
		return fmt.Errorf("failed to create temp key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   namespace,
		"name":        secretName,
		"fingerprint": keyPair.Fingerprint,
	}).Info("Created temp key secret")

	return nil
}

// GetAgentKeyPair gets the agent key pair from the secret
func (m *KeyManager) GetAgentKeyPair(ctx context.Context, namespace string) (*KeyPair, error) {
	// Get secret
	secret, err := m.Client.CoreV1().Secrets(namespace).Get(ctx, DefaultAgentKeySecretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get agent key secret: %v", err)
	}

	// Extract key pair
	privateKey, ok := secret.Data[DefaultPrivateKeyName]
	if !ok {
		return nil, fmt.Errorf("private key not found in secret")
	}

	publicKey, ok := secret.Data[DefaultPublicKeyName]
	if !ok {
		return nil, fmt.Errorf("public key not found in secret")
	}

	authorizedKeys, ok := secret.Data[DefaultAuthorizedKeysName]
	if !ok {
		authorizedKeys = publicKey
	}

	// Get fingerprint and created time from annotations
	fingerprint := secret.Annotations[DefaultKeyAnnotationPrefix+"fingerprint"]
	createdAtStr := secret.Annotations[DefaultKeyAnnotationPrefix+"created-at"]
	createdAt := time.Now()
	if createdAtStr != "" {
		parsedTime, err := time.Parse(time.RFC3339, createdAtStr)
		if err == nil {
			createdAt = parsedTime
		}
	}

	return &KeyPair{
		PrivateKey:     privateKey,
		PublicKey:      publicKey,
		AuthorizedKeys: authorizedKeys,
		Fingerprint:    fingerprint,
		CreatedAt:      createdAt,
	}, nil
}

// GetTempKeyPair gets the temporary pod key pair from the secret
func (m *KeyManager) GetTempKeyPair(ctx context.Context, namespace, name string) (*KeyPair, error) {
	secretName := DefaultTempKeySecretName
	if name != "" {
		secretName = fmt.Sprintf("%s-%s", DefaultTempKeySecretName, name)
	}

	// Get secret
	secret, err := m.Client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get temp key secret: %v", err)
	}

	// Extract key pair
	privateKey, ok := secret.Data[DefaultPrivateKeyName]
	if !ok {
		return nil, fmt.Errorf("private key not found in secret")
	}

	publicKey, ok := secret.Data[DefaultPublicKeyName]
	if !ok {
		return nil, fmt.Errorf("public key not found in secret")
	}

	authorizedKeys, ok := secret.Data[DefaultAuthorizedKeysName]
	if !ok {
		authorizedKeys = publicKey
	}

	// Get fingerprint and created time from annotations
	fingerprint := secret.Annotations[DefaultKeyAnnotationPrefix+"fingerprint"]
	createdAtStr := secret.Annotations[DefaultKeyAnnotationPrefix+"created-at"]
	createdAt := time.Now()
	if createdAtStr != "" {
		parsedTime, err := time.Parse(time.RFC3339, createdAtStr)
		if err == nil {
			createdAt = parsedTime
		}
	}

	return &KeyPair{
		PrivateKey:     privateKey,
		PublicKey:      publicKey,
		AuthorizedKeys: authorizedKeys,
		Fingerprint:    fingerprint,
		CreatedAt:      createdAt,
	}, nil
}

// EnsureAgentKeyPair ensures that the agent key pair exists
func (m *KeyManager) EnsureAgentKeyPair(ctx context.Context, namespace string) (*KeyPair, error) {
	// Try to get existing key pair
	keyPair, err := m.GetAgentKeyPair(ctx, namespace)
	if err == nil {
		// Check if key needs rotation
		if time.Since(keyPair.CreatedAt) < DefaultKeyRotationInterval {
			return keyPair, nil
		}

		log.WithFields(map[string]interface{}{
			"namespace":   namespace,
			"fingerprint": keyPair.Fingerprint,
			"age":         time.Since(keyPair.CreatedAt),
		}).Info("Rotating agent key pair")
	}

	// Generate new key pair
	keyPair, err = GenerateKeyPair(DefaultKeyBits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create secret
	if err := m.CreateAgentKeySecret(ctx, namespace, keyPair); err != nil {
		return nil, fmt.Errorf("failed to create agent key secret: %v", err)
	}

	return keyPair, nil
}

// EnsureTempKeyPair ensures that the temporary pod key pair exists
func (m *KeyManager) EnsureTempKeyPair(ctx context.Context, namespace, name string) (*KeyPair, error) {
	// Try to get existing key pair
	keyPair, err := m.GetTempKeyPair(ctx, namespace, name)
	if err == nil {
		// Check if key needs rotation
		if time.Since(keyPair.CreatedAt) < DefaultKeyRotationInterval {
			return keyPair, nil
		}

		log.WithFields(map[string]interface{}{
			"namespace":   namespace,
			"name":        name,
			"fingerprint": keyPair.Fingerprint,
			"age":         time.Since(keyPair.CreatedAt),
		}).Info("Rotating temp key pair")
	}

	// Generate new key pair
	keyPair, err = GenerateKeyPair(DefaultKeyBits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create secret
	if err := m.CreateTempKeySecret(ctx, namespace, name, keyPair); err != nil {
		return nil, fmt.Errorf("failed to create temp key secret: %v", err)
	}

	return keyPair, nil
}

// AddAuthorizedKey adds a public key to the authorized_keys file
func (m *KeyManager) AddAuthorizedKey(ctx context.Context, namespace, name string, publicKey []byte) error {
	secretName := DefaultAgentKeySecretName
	if name != "" {
		secretName = fmt.Sprintf("%s-%s", DefaultTempKeySecretName, name)
	}

	// Get secret
	secret, err := m.Client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get key secret: %v", err)
	}

	// Get authorized_keys
	authorizedKeys, ok := secret.Data[DefaultAuthorizedKeysName]
	if !ok {
		authorizedKeys = []byte{}
	}

	// Check if key already exists
	if strings.Contains(string(authorizedKeys), string(publicKey)) {
		return nil
	}

	// Add new key
	authorizedKeys = append(authorizedKeys, publicKey...)
	if !strings.HasSuffix(string(authorizedKeys), "\n") {
		authorizedKeys = append(authorizedKeys, '\n')
	}

	// Update secret
	secret.Data[DefaultAuthorizedKeysName] = authorizedKeys
	_, err = m.Client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace": namespace,
		"name":      secretName,
	}).Info("Added authorized key")

	return nil
}

// RemoveAuthorizedKey removes a public key from the authorized_keys file
func (m *KeyManager) RemoveAuthorizedKey(ctx context.Context, namespace, name string, publicKey []byte) error {
	secretName := DefaultAgentKeySecretName
	if name != "" {
		secretName = fmt.Sprintf("%s-%s", DefaultTempKeySecretName, name)
	}

	// Get secret
	secret, err := m.Client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get key secret: %v", err)
	}

	// Get authorized_keys
	authorizedKeys, ok := secret.Data[DefaultAuthorizedKeysName]
	if !ok {
		return nil
	}

	// Remove key
	lines := strings.Split(string(authorizedKeys), "\n")
	newLines := make([]string, 0, len(lines))
	keyStr := strings.TrimSpace(string(publicKey))
	for _, line := range lines {
		if strings.TrimSpace(line) != keyStr {
			newLines = append(newLines, line)
		}
	}
	newAuthorizedKeys := []byte(strings.Join(newLines, "\n"))

	// Update secret
	secret.Data[DefaultAuthorizedKeysName] = newAuthorizedKeys
	_, err = m.Client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace": namespace,
		"name":      secretName,
	}).Info("Removed authorized key")

	return nil
}

// RotateAgentKeyPair rotates the agent key pair
func (m *KeyManager) RotateAgentKeyPair(ctx context.Context, namespace string) (*KeyPair, error) {
	// Generate new key pair
	keyPair, err := GenerateKeyPair(DefaultKeyBits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create secret
	if err := m.CreateAgentKeySecret(ctx, namespace, keyPair); err != nil {
		return nil, fmt.Errorf("failed to create agent key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   namespace,
		"fingerprint": keyPair.Fingerprint,
	}).Info("Rotated agent key pair")

	return keyPair, nil
}

// RotateTempKeyPair rotates the temporary pod key pair
func (m *KeyManager) RotateTempKeyPair(ctx context.Context, namespace, name string) (*KeyPair, error) {
	// Generate new key pair
	keyPair, err := GenerateKeyPair(DefaultKeyBits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create secret
	if err := m.CreateTempKeySecret(ctx, namespace, name, keyPair); err != nil {
		return nil, fmt.Errorf("failed to create temp key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   namespace,
		"name":        name,
		"fingerprint": keyPair.Fingerprint,
	}).Info("Rotated temp key pair")

	return keyPair, nil
}
