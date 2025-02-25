package ssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/ssh"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// SSH key size
	keySize = 4096

	// Secret keys
	privateKeyKey  = "ssh-private-key"
	publicKeyKey   = "ssh-public-key"
	authorizedKeys = "authorized_keys"
)

// KeyManager handles SSH key generation and management
type KeyManager struct {
	client client.Client
}

// NewKeyManager creates a new SSH key manager
func NewKeyManager(client client.Client) *KeyManager {
	return &KeyManager{
		client: client,
	}
}

// EnsureKeys ensures SSH keys exist for the remote cluster
func (k *KeyManager) EnsureKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil || rc.Spec.PVCSync.SSH.KeySecretRef == nil {
		return fmt.Errorf("PVCSync SSH configuration not found")
	}

	// Generate new key pair
	privateKey, publicKey, err := k.generateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create secret in controller cluster
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rc.Spec.PVCSync.SSH.KeySecretRef.Name,
			Namespace: rc.Spec.PVCSync.SSH.KeySecretRef.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "pvc-syncer-agent",
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
				"dr-syncer.io/remote-cluster":  rc.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			privateKeyKey:  privateKey,
			publicKeyKey:   publicKey,
			authorizedKeys: publicKey,
		},
	}

	return k.client.Create(ctx, secret)
}

// generateKeyPair generates a new SSH key pair
func (k *KeyManager) generateKeyPair() ([]byte, []byte, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %v", err)
	}

	// Convert private key to PEM format
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	// Generate public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate public key: %v", err)
	}

	// Convert public key to authorized_keys format
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	return privateKeyBytes, publicKeyBytes, nil
}

// DeleteKeys deletes SSH keys for the remote cluster
func (k *KeyManager) DeleteKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil || rc.Spec.PVCSync.SSH.KeySecretRef == nil {
		return nil // Nothing to delete
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rc.Spec.PVCSync.SSH.KeySecretRef.Name,
			Namespace: rc.Spec.PVCSync.SSH.KeySecretRef.Namespace,
		},
	}

	return k.client.Delete(ctx, secret)
}

// RotateKeys rotates SSH keys for the remote cluster
func (k *KeyManager) RotateKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Delete existing keys
	if err := k.DeleteKeys(ctx, rc); err != nil {
		return fmt.Errorf("failed to delete existing keys: %v", err)
	}

	// Create new keys
	if err := k.EnsureKeys(ctx, rc); err != nil {
		return fmt.Errorf("failed to create new keys: %v", err)
	}

	return nil
}
