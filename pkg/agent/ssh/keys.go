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

	// Host keys
	hostRsaKey        = "ssh_host_rsa_key"
	hostRsaKeyPub     = "ssh_host_rsa_key.pub"
	hostEcdsaKey      = "ssh_host_ecdsa_key"
	hostEcdsaKeyPub   = "ssh_host_ecdsa_key.pub"
	hostEd25519Key    = "ssh_host_ed25519_key"
	hostEd25519KeyPub = "ssh_host_ed25519_key.pub"
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

// EnsureKeys ensures SSH keys exist for the remote cluster and returns the secret
func (k *KeyManager) EnsureKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) (*corev1.Secret, error) {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil {
		return nil, fmt.Errorf("PVCSync SSH configuration not found")
	}

	// Use a default secret name if keySecretRef is not specified
	secretName := "pvc-syncer-agent-keys"
	secretNamespace := "dr-syncer"
	
	// If keySecretRef is specified, use those values
	if rc.Spec.PVCSync.SSH.KeySecretRef != nil {
		secretName = rc.Spec.PVCSync.SSH.KeySecretRef.Name
		secretNamespace = rc.Spec.PVCSync.SSH.KeySecretRef.Namespace
	}

	// Check if secret already exists
	existingSecret := &corev1.Secret{}
	err := k.client.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: secretNamespace,
	}, existingSecret)

	// If secret exists, return it
	if err == nil {
		log.Infof("SSH key secret %s/%s already exists for cluster %s",
			secretNamespace,
			secretName,
			rc.Name)
		return existingSecret, nil
	}

	// If error is not "not found", return the error
	if client.IgnoreNotFound(err) != nil {
		return nil, fmt.Errorf("failed to check if secret exists: %v", err)
	}

	// Generate client key pair
	privateKey, publicKey, err := k.generateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client key pair: %v", err)
	}

	// Generate host keys
	hostKeys, err := k.generateHostKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate host keys: %v", err)
	}

	// Create secret data with both client and host keys
	// Store keys under both naming conventions for compatibility
	secretData := map[string][]byte{
		privateKeyKey:  privateKey,
		"id_rsa":       privateKey,
		publicKeyKey:   publicKey,
		"id_rsa.pub":   publicKey,
		authorizedKeys: publicKey,
	}

	// Add host keys to secret data
	for key, value := range hostKeys {
		secretData[key] = value
	}

	// Create secret in controller cluster
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "pvc-syncer-agent",
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
				"dr-syncer.io/remote-cluster":  rc.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	log.Infof("Creating SSH key secret %s/%s for cluster %s with host keys",
		secretNamespace,
		secretName,
		rc.Name)

	err = k.client.Create(ctx, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH key secret: %v", err)
	}

	// Get the created secret to ensure we have the latest version
	createdSecret := &corev1.Secret{}
	err = k.client.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: secretNamespace,
	}, createdSecret)

	if err != nil {
		return nil, fmt.Errorf("failed to get created SSH key secret: %v", err)
	}

	return createdSecret, nil
}

// generateKeyPair generates a new SSH key pair for client authentication
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

// generateHostKeys generates SSH host keys (RSA, ECDSA, ED25519)
func (k *KeyManager) generateHostKeys() (map[string][]byte, error) {
	hostKeys := make(map[string][]byte)

	// Generate RSA host key
	rsaPrivateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA host key: %v", err)
	}

	// Convert RSA private key to PEM format
	rsaPrivateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rsaPrivateKey),
	}
	rsaPrivateKeyBytes := pem.EncodeToMemory(rsaPrivateKeyPEM)

	// Generate RSA public key
	rsaPublicKey, err := ssh.NewPublicKey(&rsaPrivateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA host public key: %v", err)
	}

	// Convert RSA public key to authorized_keys format
	rsaPublicKeyBytes := ssh.MarshalAuthorizedKey(rsaPublicKey)

	// Store RSA host keys
	hostKeys[hostRsaKey] = rsaPrivateKeyBytes
	hostKeys[hostRsaKeyPub] = rsaPublicKeyBytes

	// For ECDSA and ED25519 keys, we'll use the ssh-keygen command
	// This is a simplified implementation that only generates RSA keys
	// In a production environment, you would want to generate all three types

	// For now, we'll use the same RSA key for all host key types
	// This is not ideal for production but will work for development
	hostKeys[hostEcdsaKey] = rsaPrivateKeyBytes
	hostKeys[hostEcdsaKeyPub] = rsaPublicKeyBytes
	hostKeys[hostEd25519Key] = rsaPrivateKeyBytes
	hostKeys[hostEd25519KeyPub] = rsaPublicKeyBytes

	return hostKeys, nil
}

// DeleteKeys deletes SSH keys for the remote cluster
func (k *KeyManager) DeleteKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil {
		return nil // Nothing to delete
	}

	// Use a default secret name if keySecretRef is not specified
	secretName := "pvc-syncer-agent-keys"
	secretNamespace := "dr-syncer"
	
	// If keySecretRef is specified, use those values
	if rc.Spec.PVCSync.SSH.KeySecretRef != nil {
		secretName = rc.Spec.PVCSync.SSH.KeySecretRef.Name
		secretNamespace = rc.Spec.PVCSync.SSH.KeySecretRef.Namespace
	}

	// Delete the main secret with private keys
	mainSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
		},
	}
	
	mainErr := k.client.Delete(ctx, mainSecret)
	if mainErr != nil && client.IgnoreNotFound(mainErr) != nil {
		log.Errorf("Failed to delete main SSH key secret %s/%s: %v", secretNamespace, secretName, mainErr)
		return fmt.Errorf("failed to delete main SSH key secret: %v", mainErr)
	}
	
	// Also delete the controller cluster public key secret
	controllerSecretName := "dr-syncer-sshkey-" + rc.Name
	controllerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controllerSecretName,
			Namespace: "dr-syncer",
		},
	}
	
	controllerErr := k.client.Delete(ctx, controllerSecret)
	if controllerErr != nil && client.IgnoreNotFound(controllerErr) != nil {
		log.Errorf("Failed to delete controller SSH key secret %s/%s: %v", "dr-syncer", controllerSecretName, controllerErr)
		// Don't return an error if we can't delete the controller key secret, as it might not exist
	}
	
	return nil
}

// RotateKeys rotates SSH keys for the remote cluster
func (k *KeyManager) RotateKeys(ctx context.Context, rc *drv1alpha1.RemoteCluster) (*corev1.Secret, error) {
	// Delete existing keys
	if err := k.DeleteKeys(ctx, rc); err != nil {
		return nil, fmt.Errorf("failed to delete existing keys: %v", err)
	}

	// Create new keys
	secret, err := k.EnsureKeys(ctx, rc)
	if err != nil {
		return nil, fmt.Errorf("failed to create new keys: %v", err)
	}

	return secret, nil
}

// PushKeysToRemoteCluster pushes the SSH key secret to the remote cluster
func (k *KeyManager) PushKeysToRemoteCluster(ctx context.Context, rc *drv1alpha1.RemoteCluster, remoteClient client.Client, secret *corev1.Secret) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil {
		return fmt.Errorf("PVCSync SSH configuration not found")
	}

	// Use the secret name and namespace from the provided secret
	secretName := secret.Name
	secretNamespace := secret.Namespace

	// Check if secret already exists in remote cluster
	remoteSecret := &corev1.Secret{}
	err := remoteClient.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: secretNamespace,
	}, remoteSecret)

	// Create a new secret for the remote cluster
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: secret.Namespace,
			Labels:    secret.Labels,
		},
		Type: secret.Type,
		Data: secret.Data,
	}

	// If secret exists, update it
	if err == nil {
		log.Infof("Updating SSH key secret %s/%s in remote cluster %s",
			newSecret.Namespace, newSecret.Name, rc.Name)

		// Update the existing secret
		remoteSecret.Data = newSecret.Data
		remoteSecret.Labels = newSecret.Labels
		return remoteClient.Update(ctx, remoteSecret)
	}

	// If error is not "not found", return the error
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to check if secret exists in remote cluster: %v", err)
	}

	// Secret doesn't exist, create it
	log.Infof("Creating SSH key secret %s/%s in remote cluster %s",
		newSecret.Namespace, newSecret.Name, rc.Name)
	return remoteClient.Create(ctx, newSecret)
}

// EnsureKeysInControllerCluster ensures SSH public keys exist in the controller cluster
func (k *KeyManager) EnsureKeysInControllerCluster(ctx context.Context, rc *drv1alpha1.RemoteCluster, secret *corev1.Secret) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.SSH == nil {
		return fmt.Errorf("PVCSync SSH configuration not found")
	}

	// Use a standardized naming convention for the SSH key secrets in the controller cluster
	secretName := "dr-syncer-sshkey-" + rc.Name
	secretNamespace := "dr-syncer" // Use the dr-syncer namespace for all controller cluster secrets

	log.Infof("EnsureKeysInControllerCluster: Checking for public key secret %s/%s in controller cluster for remote cluster %s",
		secretNamespace, secretName, rc.Name)

	// Check if secret already exists in controller cluster
	controllerSecret := &corev1.Secret{}
	err := k.client.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: secretNamespace,
	}, controllerSecret)

	// Create a new data map with only public keys
	publicKeyData := map[string][]byte{}

	// Log the keys found in the secret
	log.Infof("EnsureKeysInControllerCluster: Secret %s/%s for remote cluster %s has keys: %v",
		secretNamespace, secretName, rc.Name, getKeysFromSecret(secret))

	// Add public keys only - check for both naming conventions
	if publicKey, ok := secret.Data[publicKeyKey]; ok {
		log.Infof("EnsureKeysInControllerCluster: Found %s key in secret", publicKeyKey)
		publicKeyData[publicKeyKey] = publicKey
	} else if publicKey, ok := secret.Data["id_rsa.pub"]; ok {
		log.Infof("EnsureKeysInControllerCluster: Found id_rsa.pub key in secret")
		// If using the alternative naming convention, store under both names for compatibility
		publicKeyData[publicKeyKey] = publicKey
		publicKeyData["id_rsa.pub"] = publicKey
	} else {
		log.Warnf("EnsureKeysInControllerCluster: No public key found in secret %s/%s for remote cluster %s",
			secretNamespace, secretName, rc.Name)
	}
	
	if authorizedKey, ok := secret.Data[authorizedKeys]; ok {
		log.Infof("EnsureKeysInControllerCluster: Found %s key in secret", authorizedKeys)
		publicKeyData[authorizedKeys] = authorizedKey
	}
	if hostRsaKeyPubData, ok := secret.Data[hostRsaKeyPub]; ok {
		log.Infof("EnsureKeysInControllerCluster: Found %s key in secret", hostRsaKeyPub)
		publicKeyData[hostRsaKeyPub] = hostRsaKeyPubData
	}
	if hostEcdsaKeyPubData, ok := secret.Data[hostEcdsaKeyPub]; ok {
		log.Infof("EnsureKeysInControllerCluster: Found %s key in secret", hostEcdsaKeyPub)
		publicKeyData[hostEcdsaKeyPub] = hostEcdsaKeyPubData
	}
	if hostEd25519KeyPubData, ok := secret.Data[hostEd25519KeyPub]; ok {
		log.Infof("EnsureKeysInControllerCluster: Found %s key in secret", hostEd25519KeyPub)
		publicKeyData[hostEd25519KeyPub] = hostEd25519KeyPubData
	}

	// Log the public keys we're going to store
	log.Infof("EnsureKeysInControllerCluster: Will store public keys in controller cluster: %v",
		getMapKeys(publicKeyData))

	// If secret exists, update it
	if err == nil {
		log.Infof("EnsureKeysInControllerCluster: Updating SSH public key secret %s/%s in controller cluster for remote cluster %s",
			secretNamespace, secretName, rc.Name)

		// Update the existing secret with only public keys
		controllerSecret.Data = publicKeyData
		controllerSecret.Labels = secret.Labels
		
		updateErr := k.client.Update(ctx, controllerSecret)
		if updateErr != nil {
			log.Errorf("EnsureKeysInControllerCluster: Failed to update secret %s/%s in controller cluster: %v",
				secretNamespace, secretName, updateErr)
			return fmt.Errorf("failed to update secret in controller cluster: %v", updateErr)
		}
		
		log.Infof("EnsureKeysInControllerCluster: Successfully updated secret %s/%s in controller cluster",
			secretNamespace, secretName)
		return nil
	}

	// If error is not "not found", return the error
	if client.IgnoreNotFound(err) != nil {
		log.Errorf("EnsureKeysInControllerCluster: Error getting secret %s/%s from controller cluster: %v",
			secretNamespace, secretName, err)
		return fmt.Errorf("failed to check if secret exists in controller cluster: %v", err)
	}

	// Secret doesn't exist, create it
	log.Infof("EnsureKeysInControllerCluster: Creating SSH public key secret %s/%s in controller cluster for remote cluster %s",
		secretNamespace, secretName, rc.Name)

	// Create a new secret for the controller cluster with only public keys
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "pvc-syncer-agent",
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
				"dr-syncer.io/remote-cluster":  rc.Name,
				"dr-syncer.io/public-keys":     "true", // Add a label to identify this as a public key secret
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: publicKeyData,
	}

	createErr := k.client.Create(ctx, newSecret)
	if createErr != nil {
		log.Errorf("EnsureKeysInControllerCluster: Failed to create secret %s/%s in controller cluster: %v",
			secretNamespace, secretName, createErr)
		return fmt.Errorf("failed to create secret in controller cluster: %v", createErr)
	}
	
	log.Infof("EnsureKeysInControllerCluster: Successfully created secret %s/%s in controller cluster",
		secretNamespace, secretName)
	return nil
}

// Helper function to get keys from a secret for logging
func getKeysFromSecret(secret *corev1.Secret) []string {
	keys := []string{}
	for k := range secret.Data {
		keys = append(keys, k)
	}
	return keys
}

// Helper function to get keys from a map for logging
func getMapKeys(m map[string][]byte) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
