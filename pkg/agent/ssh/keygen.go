package ssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
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

	// DefaultPrivateKeyName is the default name for the private key in secrets
	DefaultPrivateKeyName = "id_rsa"

	// DefaultPublicKeyName is the default name for the public key in secrets
	DefaultPublicKeyName = "id_rsa.pub"

	// DefaultAuthorizedKeysName is the default name for the authorized_keys file in secrets
	DefaultAuthorizedKeysName = "authorized_keys"
)

// SimpleKeyPair represents an SSH key pair
type SimpleKeyPair struct {
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

// GenerateKeyPair generates a new SSH key pair
func GenerateKeyPair(bits int) (privateKey, publicKey []byte, fingerprint string, err error) {
	if bits <= 0 {
		bits = DefaultKeyBits
	}

	// Generate private key
	rsaKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to generate private key: %v", err)
	}

	// Convert private key to PEM
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	// Generate public key
	publicSSHKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to generate public key: %v", err)
	}

	// Get public key in authorized_keys format
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicSSHKey)

	// Get fingerprint
	fingerprint = ssh.FingerprintSHA256(publicSSHKey)

	return privateKeyBytes, publicKeyBytes, fingerprint, nil
}

// CreateKeySecret creates a secret for SSH keys
func CreateKeySecret(client kubernetes.Interface, namespace, name string, privateKey, publicKey []byte) error {
	// Create secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "dr-syncer",
				"component": "ssh-key",
			},
			Annotations: map[string]string{
				"dr-syncer.io/created-at": time.Now().Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			DefaultPrivateKeyName:     privateKey,
			DefaultPublicKeyName:      publicKey,
			DefaultAuthorizedKeysName: publicKey,
		},
	}

	// Create or update secret
	_, err := client.CoreV1().Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing secret
			_, err = client.CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update key secret: %v", err)
			}
			log.Info("Updated SSH key secret")
			return nil
		}
		return fmt.Errorf("failed to create key secret: %v", err)
	}

	log.Info("Created SSH key secret")
	return nil
}

// AddPublicKeyToAuthorizedKeys adds a public key to an authorized_keys file in a secret
func AddPublicKeyToAuthorizedKeys(client kubernetes.Interface, namespace, secretName string, publicKey []byte) error {
	// Get the secret
	secret, err := client.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret: %v", err)
	}

	// Get current authorized_keys
	authorizedKeys, ok := secret.Data[DefaultAuthorizedKeysName]
	if !ok {
		authorizedKeys = []byte{}
	}

	// Append the new public key
	authorizedKeys = append(authorizedKeys, publicKey...)
	if len(authorizedKeys) > 0 && authorizedKeys[len(authorizedKeys)-1] != '\n' {
		authorizedKeys = append(authorizedKeys, '\n')
	}

	// Update the secret
	secret.Data[DefaultAuthorizedKeysName] = authorizedKeys
	_, err = client.CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret: %v", err)
	}

	log.Info("Added public key to authorized_keys")
	return nil
}

// ScheduleKeyRotation sets up a goroutine to rotate keys at the specified interval
func ScheduleKeyRotation(ctx context.Context, client kubernetes.Interface, namespace, secretName string, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultKeyRotationInterval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Generate new keys
				privateKey, publicKey, _, err := GenerateKeyPair(DefaultKeyBits)
				if err != nil {
					log.Errorf("Key rotation failed: %v", err)
					continue
				}

				// Update the secret
				secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					log.Errorf("Failed to get secret for key rotation: %v", err)
					continue
				}

				secret.Data[DefaultPrivateKeyName] = privateKey
				secret.Data[DefaultPublicKeyName] = publicKey
				secret.Data[DefaultAuthorizedKeysName] = publicKey

				_, err = client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
				if err != nil {
					log.Errorf("Failed to update secret for key rotation: %v", err)
					continue
				}

				log.Info("Rotated SSH keys")
			}
		}
	}()

	log.Infof("Scheduled key rotation with interval %s", interval)
}
