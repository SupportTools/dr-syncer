package replication

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// DefaultKeyBits is the default number of bits for RSA keys
	DefaultKeyBits = 4096
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

// GenerateKeyPair generates a new SSH key pair
func GenerateKeyPair() (*KeyPair, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, DefaultKeyBits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %v", err)
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

// EnsureTempPodKeys ensures that the namespace mapping has a temp pod key secret
func EnsureTempPodKeys(ctx context.Context, c client.Client, repl *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"namespace":        repl.Namespace,
	}).Info("Ensuring temp pod keys for namespace mapping")

	// If TempPodKeySecretRef is not set, generate a default name
	if repl.Spec.TempPodKeySecretRef == nil {
		repl.Spec.TempPodKeySecretRef = &drv1alpha1.SecretReference{
			Name:      fmt.Sprintf("dr-syncer-temp-key-%s", repl.Name),
			Namespace: repl.Namespace,
		}
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        repl.Spec.TempPodKeySecretRef.Namespace,
		}).Info("Generated default temp pod key secret reference")
	}

	// Check if secret already exists
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      repl.Spec.TempPodKeySecretRef.Name,
		Namespace: repl.Spec.TempPodKeySecretRef.Namespace,
	}, secret)

	// If secret exists, return it
	if err == nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        repl.Spec.TempPodKeySecretRef.Namespace,
		}).Info("Temp pod key secret already exists")
		return nil
	}

	// If error is not "not found", return the error
	if !errors.IsNotFound(err) {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        repl.Spec.TempPodKeySecretRef.Namespace,
			"error":            err,
		}).Error("Failed to check if temp pod key secret exists")
		return fmt.Errorf("failed to check if temp pod key secret exists: %v", err)
	}

	// Generate new key pair
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
	}).Info("Generating new key pair for temp pod")

	keyPair, err := GenerateKeyPair()
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"error":            err,
		}).Error("Failed to generate temp pod key pair")
		return fmt.Errorf("failed to generate temp pod key pair: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"fingerprint":      keyPair.Fingerprint,
	}).Info("Generated new key pair for temp pod")

	// Create secret
	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      repl.Spec.TempPodKeySecretRef.Name,
			Namespace: repl.Spec.TempPodKeySecretRef.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":        "dr-syncer",
				"app.kubernetes.io/part-of":     "dr-syncer",
				"app.kubernetes.io/managed-by":  "dr-syncer-controller",
				"dr-syncer.io/namespacemapping": repl.Name,
			},
			Annotations: map[string]string{
				"dr-syncer.io/key-fingerprint": keyPair.Fingerprint,
				"dr-syncer.io/key-created-at":  keyPair.CreatedAt.Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"id_rsa":          keyPair.PrivateKey,
			"id_rsa.pub":      keyPair.PublicKey,
			"authorized_keys": keyPair.AuthorizedKeys,
		},
	}

	// Create the secret
	if err := c.Create(ctx, secret); err != nil {
		return fmt.Errorf("failed to create temp pod key secret: %v", err)
	}

	return nil
}

// PushTempPodKeysToRemoteCluster pushes the temp pod key secret to the remote cluster
func PushTempPodKeysToRemoteCluster(ctx context.Context, c client.Client, remoteClient client.Client, repl *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"namespace":        repl.Namespace,
	}).Info("Pushing temp pod keys to remote cluster")

	if repl.Spec.TempPodKeySecretRef == nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
		}).Error("TempPodKeySecretRef is not set")
		return fmt.Errorf("TempPodKeySecretRef is not set")
	}

	// Get the secret from the controller cluster
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      repl.Spec.TempPodKeySecretRef.Name,
		Namespace: repl.Spec.TempPodKeySecretRef.Namespace,
	}, secret)
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        repl.Spec.TempPodKeySecretRef.Namespace,
			"error":            err,
		}).Error("Failed to get temp pod key secret")
		return fmt.Errorf("failed to get temp pod key secret: %v", err)
	}

	// Check if secret already exists in remote cluster
	remoteSecret := &corev1.Secret{}
	err = remoteClient.Get(ctx, client.ObjectKey{
		Name:      repl.Spec.TempPodKeySecretRef.Name,
		Namespace: "dr-syncer", // Use the agent namespace in the remote cluster
	}, remoteSecret)

	// Create a new secret for the remote cluster
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        repl.Spec.TempPodKeySecretRef.Name,
			Namespace:   "dr-syncer", // Use the agent namespace in the remote cluster
			Labels:      secret.Labels,
			Annotations: secret.Annotations,
		},
		Type: secret.Type,
		Data: secret.Data,
	}

	// If secret exists, update it
	if err == nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        "dr-syncer",
		}).Info("Updating existing temp pod key secret in remote cluster")

		remoteSecret.Data = newSecret.Data
		remoteSecret.Labels = newSecret.Labels
		remoteSecret.Annotations = newSecret.Annotations
		return remoteClient.Update(ctx, remoteSecret)
	}

	// If error is not "not found", return the error
	if !errors.IsNotFound(err) {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        "dr-syncer",
			"error":            err,
		}).Error("Failed to check if secret exists in remote cluster")
		return fmt.Errorf("failed to check if secret exists in remote cluster: %v", err)
	}

	// Secret doesn't exist, create it
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
		"namespace":        "dr-syncer",
	}).Info("Creating temp pod key secret in remote cluster")
	return remoteClient.Create(ctx, newSecret)
}

// AddTempPodKeyToAgent adds the temp pod key to the agent's authorized_keys
func AddTempPodKeyToAgent(ctx context.Context, c client.Client, repl *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"namespace":        repl.Namespace,
	}).Info("Adding temp pod key to agent's authorized_keys")

	if repl.Spec.TempPodKeySecretRef == nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
		}).Error("TempPodKeySecretRef is not set")
		return fmt.Errorf("TempPodKeySecretRef is not set")
	}

	// Get the temp pod key secret
	tempPodKeySecret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      repl.Spec.TempPodKeySecretRef.Name,
		Namespace: repl.Spec.TempPodKeySecretRef.Namespace,
	}, tempPodKeySecret)
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        repl.Spec.TempPodKeySecretRef.Namespace,
			"error":            err,
		}).Error("Failed to get temp pod key secret")
		return fmt.Errorf("failed to get temp pod key secret: %v", err)
	}

	// Get the public key
	publicKey, ok := tempPodKeySecret.Data["id_rsa.pub"]
	if !ok {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      repl.Spec.TempPodKeySecretRef.Name,
			"namespace":        repl.Spec.TempPodKeySecretRef.Namespace,
		}).Error("Public key not found in temp pod key secret")
		return fmt.Errorf("public key not found in temp pod key secret")
	}

	// Get the agent key secret
	agentKeySecret := &corev1.Secret{}
	err = c.Get(ctx, client.ObjectKey{
		Name:      "dr-syncer-agent-key",
		Namespace: "dr-syncer",
	}, agentKeySecret)
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      "dr-syncer-agent-key",
			"namespace":        "dr-syncer",
			"error":            err,
		}).Error("Failed to get agent key secret")
		return fmt.Errorf("failed to get agent key secret: %v", err)
	}

	// Get the authorized_keys
	authorizedKeys, ok := agentKeySecret.Data["authorized_keys"]
	if !ok {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      "dr-syncer-agent-key",
			"namespace":        "dr-syncer",
		}).Info("No authorized_keys found in agent key secret, creating new")
		authorizedKeys = []byte{}
	}

	// Check if key already exists
	if len(authorizedKeys) > 0 && string(authorizedKeys)[len(string(authorizedKeys))-1] != '\n' {
		authorizedKeys = append(authorizedKeys, '\n')
	}

	// Add the public key to the authorized_keys
	authorizedKeys = append(authorizedKeys, publicKey...)

	// Update the agent key secret
	agentKeySecret.Data["authorized_keys"] = authorizedKeys

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"secret_name":      "dr-syncer-agent-key",
		"namespace":        "dr-syncer",
	}).Info("Updating agent key secret with temp pod public key")

	// Update the secret
	if err := c.Update(ctx, agentKeySecret); err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"secret_name":      "dr-syncer-agent-key",
			"namespace":        "dr-syncer",
			"error":            err,
		}).Error("Failed to update agent key secret")
		return fmt.Errorf("failed to update agent key secret: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
	}).Info("Successfully added temp pod key to agent's authorized_keys")

	return nil
}
