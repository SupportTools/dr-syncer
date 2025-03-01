package sshkeys

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KeySystem manages the two-layer key system
type KeySystem struct {
	// KeyManager is the key manager
	KeyManager *KeyManager

	// Namespace is the namespace for the keys
	Namespace string
}

// NewKeySystem creates a new key system
func NewKeySystem(client kubernetes.Interface, namespace string) *KeySystem {
	return &KeySystem{
		KeyManager: NewKeyManager(client),
		Namespace:  namespace,
	}
}

// InitializeKeys initializes the key system
func (s *KeySystem) InitializeKeys(ctx context.Context) error {
	// Ensure agent key pair
	agentKeyPair, err := s.KeyManager.EnsureAgentKeyPair(ctx, s.Namespace)
	if err != nil {
		return fmt.Errorf("failed to ensure agent key pair: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   s.Namespace,
		"fingerprint": agentKeyPair.Fingerprint,
	}).Info("Initialized agent key pair")

	return nil
}

// CreateTempPodKeys creates keys for a temporary pod
func (s *KeySystem) CreateTempPodKeys(ctx context.Context, podName string) (*KeyPair, error) {
	// Ensure temp key pair
	tempKeyPair, err := s.KeyManager.EnsureTempKeyPair(ctx, s.Namespace, podName)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure temp key pair: %v", err)
	}

	// Get agent key pair
	agentKeyPair, err := s.KeyManager.GetAgentKeyPair(ctx, s.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent key pair: %v", err)
	}

	// Add agent's public key to temp's authorized_keys
	if err := s.KeyManager.AddAuthorizedKey(ctx, s.Namespace, podName, agentKeyPair.PublicKey); err != nil {
		return nil, fmt.Errorf("failed to add agent key to temp authorized_keys: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   s.Namespace,
		"pod":         podName,
		"fingerprint": tempKeyPair.Fingerprint,
	}).Info("Created temporary pod keys")

	return tempKeyPair, nil
}

// CleanupTempPodKeys cleans up keys for a temporary pod
func (s *KeySystem) CleanupTempPodKeys(ctx context.Context, podName string) error {
	secretName := fmt.Sprintf("%s-%s", DefaultTempKeySecretName, podName)

	// Delete the secret
	err := s.KeyManager.Client.CoreV1().Secrets(s.Namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete temp key secret: %v", err)
		}
		// Secret not found, already deleted
		return nil
	}

	log.WithFields(map[string]interface{}{
		"namespace": s.Namespace,
		"pod":       podName,
	}).Info("Cleaned up temporary pod keys")

	return nil
}

// RotateKeys rotates all keys in the system
func (s *KeySystem) RotateKeys(ctx context.Context) error {
	// Rotate agent key pair
	agentKeyPair, err := s.KeyManager.RotateAgentKeyPair(ctx, s.Namespace)
	if err != nil {
		return fmt.Errorf("failed to rotate agent key pair: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":   s.Namespace,
		"fingerprint": agentKeyPair.Fingerprint,
	}).Info("Rotated all keys")

	return nil
}

// ScheduleKeyRotation schedules key rotation
func (s *KeySystem) ScheduleKeyRotation(ctx context.Context, interval time.Duration) {
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
				if err := s.RotateKeys(ctx); err != nil {
					log.WithFields(map[string]interface{}{
						"namespace": s.Namespace,
						"error":     err,
					}).Error("Failed to rotate keys")
				}
			}
		}
	}()

	log.WithFields(map[string]interface{}{
		"namespace": s.Namespace,
		"interval":  interval,
	}).Info("Scheduled key rotation")
}

// GetAgentKeyPair gets the agent key pair
func (s *KeySystem) GetAgentKeyPair(ctx context.Context) (*KeyPair, error) {
	return s.KeyManager.GetAgentKeyPair(ctx, s.Namespace)
}

// GetTempKeyPair gets a temporary pod key pair
func (s *KeySystem) GetTempKeyPair(ctx context.Context, podName string) (*KeyPair, error) {
	return s.KeyManager.GetTempKeyPair(ctx, s.Namespace, podName)
}

// CreateKeySecret creates a secret with the provided key data
func (s *KeySystem) CreateKeySecret(ctx context.Context, secretName string, data map[string][]byte) error {
	// Create secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.Namespace,
			Labels: map[string]string{
				"app":                          "pvc-syncer-agent",
				"app.kubernetes.io/name":       "pvc-syncer-agent",
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-agent",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	// Create the secret
	_, err := s.KeyManager.Client.CoreV1().Secrets(s.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create key secret: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace": s.Namespace,
		"name":      secretName,
	}).Info("Created key secret")

	return nil
}

// EstablishTrustRelationship establishes a trust relationship between two key pairs
func (s *KeySystem) EstablishTrustRelationship(ctx context.Context, sourceKeyPair, targetKeyPair *KeyPair, targetName string) error {
	// Add source public key to target authorized_keys
	if err := s.KeyManager.AddAuthorizedKey(ctx, s.Namespace, targetName, sourceKeyPair.PublicKey); err != nil {
		return fmt.Errorf("failed to add source key to target authorized_keys: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace":          s.Namespace,
		"target":             targetName,
		"source_fingerprint": sourceKeyPair.Fingerprint,
		"target_fingerprint": targetKeyPair.Fingerprint,
	}).Info("Established trust relationship")

	return nil
}
