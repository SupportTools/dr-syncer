package ssh

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Tests for keygen.go constants

func TestKeygen_Constants(t *testing.T) {
	assert.Equal(t, 2048, DefaultKeyBits)
	assert.Equal(t, 24*time.Hour, DefaultKeyRotationInterval)
	assert.Equal(t, "id_rsa", DefaultPrivateKeyName)
	assert.Equal(t, "id_rsa.pub", DefaultPublicKeyName)
	assert.Equal(t, "authorized_keys", DefaultAuthorizedKeysName)
}

// Tests for keys.go constants

func TestKeys_Constants(t *testing.T) {
	assert.Equal(t, 4096, keySize)
	assert.Equal(t, "ssh-private-key", privateKeyKey)
	assert.Equal(t, "ssh-public-key", publicKeyKey)
	assert.Equal(t, "authorized_keys", authorizedKeys)
	assert.Equal(t, "ssh_host_rsa_key", hostRsaKey)
	assert.Equal(t, "ssh_host_rsa_key.pub", hostRsaKeyPub)
	assert.Equal(t, "ssh_host_ecdsa_key", hostEcdsaKey)
	assert.Equal(t, "ssh_host_ecdsa_key.pub", hostEcdsaKeyPub)
	assert.Equal(t, "ssh_host_ed25519_key", hostEd25519Key)
	assert.Equal(t, "ssh_host_ed25519_key.pub", hostEd25519KeyPub)
}

// Tests for SimpleKeyPair struct

func TestSimpleKeyPair_Struct(t *testing.T) {
	now := time.Now()
	keyPair := SimpleKeyPair{
		PrivateKey:     []byte("private-key-data"),
		PublicKey:      []byte("public-key-data"),
		AuthorizedKeys: []byte("authorized-keys-data"),
		Fingerprint:    "SHA256:abc123",
		CreatedAt:      now,
	}

	assert.Equal(t, []byte("private-key-data"), keyPair.PrivateKey)
	assert.Equal(t, []byte("public-key-data"), keyPair.PublicKey)
	assert.Equal(t, []byte("authorized-keys-data"), keyPair.AuthorizedKeys)
	assert.Equal(t, "SHA256:abc123", keyPair.Fingerprint)
	assert.Equal(t, now, keyPair.CreatedAt)
}

func TestSimpleKeyPair_Empty(t *testing.T) {
	keyPair := SimpleKeyPair{}

	assert.Nil(t, keyPair.PrivateKey)
	assert.Nil(t, keyPair.PublicKey)
	assert.Nil(t, keyPair.AuthorizedKeys)
	assert.Empty(t, keyPair.Fingerprint)
	assert.True(t, keyPair.CreatedAt.IsZero())
}

// Tests for GenerateKeyPair function

func TestGenerateKeyPair_DefaultBits(t *testing.T) {
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(0)

	require.NoError(t, err)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, publicKey)
	assert.NotEmpty(t, fingerprint)

	// Verify private key is PEM encoded
	assert.Contains(t, string(privateKey), "-----BEGIN RSA PRIVATE KEY-----")
	assert.Contains(t, string(privateKey), "-----END RSA PRIVATE KEY-----")

	// Verify public key is in SSH format
	assert.Contains(t, string(publicKey), "ssh-rsa ")

	// Verify fingerprint starts with SHA256
	assert.True(t, strings.HasPrefix(fingerprint, "SHA256:"))
}

func TestGenerateKeyPair_2048Bits(t *testing.T) {
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(2048)

	require.NoError(t, err)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, publicKey)
	assert.NotEmpty(t, fingerprint)
}

func TestGenerateKeyPair_4096Bits(t *testing.T) {
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(4096)

	require.NoError(t, err)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, publicKey)
	assert.NotEmpty(t, fingerprint)

	// 4096 bit key should be larger than 2048 bit key
	privateKey2048, _, _, err := GenerateKeyPair(2048)
	require.NoError(t, err)
	assert.Greater(t, len(privateKey), len(privateKey2048))
}

func TestGenerateKeyPair_NegativeBits(t *testing.T) {
	// Negative bits should use default
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(-1)

	require.NoError(t, err)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, publicKey)
	assert.NotEmpty(t, fingerprint)
}

func TestGenerateKeyPair_UniqueKeys(t *testing.T) {
	// Generate two key pairs
	privateKey1, publicKey1, fingerprint1, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	privateKey2, publicKey2, fingerprint2, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	// Keys should be different
	assert.NotEqual(t, privateKey1, privateKey2)
	assert.NotEqual(t, publicKey1, publicKey2)
	assert.NotEqual(t, fingerprint1, fingerprint2)
}

func TestGenerateKeyPair_MultipleGenerations(t *testing.T) {
	// Generate multiple key pairs
	for i := 0; i < 3; i++ {
		privateKey, publicKey, fingerprint, err := GenerateKeyPair(2048)

		require.NoError(t, err, "Generation %d failed", i)
		assert.NotNil(t, privateKey, "Generation %d: private key is nil", i)
		assert.NotNil(t, publicKey, "Generation %d: public key is nil", i)
		assert.NotEmpty(t, fingerprint, "Generation %d: fingerprint is empty", i)
	}
}

// Tests for KeyManager

func TestNewKeyManager(t *testing.T) {
	manager := NewKeyManager(nil)

	assert.NotNil(t, manager)
	assert.Nil(t, manager.client)
}

func TestKeyManager_Struct(t *testing.T) {
	manager := &KeyManager{}

	assert.NotNil(t, manager)
	assert.Nil(t, manager.client)
}

// Tests for Server struct

func TestServer_Struct(t *testing.T) {
	server := &Server{
		port:     2222,
		keyPath:  "/etc/ssh/keys",
		hostKeys: []string{"/etc/ssh/keys/ssh_host_rsa_key"},
	}

	assert.Equal(t, 2222, server.port)
	assert.Equal(t, "/etc/ssh/keys", server.keyPath)
	assert.Len(t, server.hostKeys, 1)
}

func TestServer_Port(t *testing.T) {
	server := &Server{port: 2222}
	assert.Equal(t, 2222, server.Port())

	server2 := &Server{port: 22}
	assert.Equal(t, 22, server2.Port())
}

func TestServer_PortZero(t *testing.T) {
	server := &Server{port: 0}
	assert.Equal(t, 0, server.Port())
}

func TestServer_EmptyHostKeys(t *testing.T) {
	server := &Server{
		port:     2222,
		hostKeys: []string{},
	}

	assert.Empty(t, server.hostKeys)
}

// Tests for getKeysFromSecret helper

func TestGetKeysFromSecret_Empty(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{},
	}

	keys := getKeysFromSecret(secret)
	assert.Empty(t, keys)
}

func TestGetKeysFromSecret_SingleKey(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"id_rsa": []byte("private-key"),
		},
	}

	keys := getKeysFromSecret(secret)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, "id_rsa")
}

func TestGetKeysFromSecret_MultipleKeys(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"id_rsa":          []byte("private-key"),
			"id_rsa.pub":      []byte("public-key"),
			"authorized_keys": []byte("authorized-keys"),
		},
	}

	keys := getKeysFromSecret(secret)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "id_rsa")
	assert.Contains(t, keys, "id_rsa.pub")
	assert.Contains(t, keys, "authorized_keys")
}

func TestGetKeysFromSecret_RealisticSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-syncer-agent-keys",
			Namespace: "dr-syncer",
		},
		Data: map[string][]byte{
			"ssh-private-key":       []byte("private-key"),
			"ssh-public-key":        []byte("public-key"),
			"id_rsa":                []byte("private-key"),
			"id_rsa.pub":            []byte("public-key"),
			"authorized_keys":       []byte("public-key"),
			"ssh_host_rsa_key":      []byte("host-key"),
			"ssh_host_rsa_key.pub":  []byte("host-key-pub"),
			"ssh_host_ecdsa_key":    []byte("host-key"),
			"ssh_host_ecdsa_key.pub": []byte("host-key-pub"),
			"ssh_host_ed25519_key":  []byte("host-key"),
			"ssh_host_ed25519_key.pub": []byte("host-key-pub"),
		},
	}

	keys := getKeysFromSecret(secret)
	assert.Len(t, keys, 11)
}

func TestGetKeysFromSecret_NilData(t *testing.T) {
	secret := &corev1.Secret{
		Data: nil,
	}

	keys := getKeysFromSecret(secret)
	assert.Empty(t, keys)
}

// Tests for getMapKeys helper

func TestGetMapKeys_Empty(t *testing.T) {
	m := map[string][]byte{}

	keys := getMapKeys(m)
	assert.Empty(t, keys)
}

func TestGetMapKeys_SingleKey(t *testing.T) {
	m := map[string][]byte{
		"key1": []byte("value1"),
	}

	keys := getMapKeys(m)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, "key1")
}

func TestGetMapKeys_MultipleKeys(t *testing.T) {
	m := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	keys := getMapKeys(m)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
	assert.Contains(t, keys, "key3")
}

func TestGetMapKeys_Nil(t *testing.T) {
	var m map[string][]byte

	keys := getMapKeys(m)
	assert.Empty(t, keys)
}

// Tests for key format validation

func TestGenerateKeyPair_PrivateKeyFormat(t *testing.T) {
	privateKey, _, _, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	privateKeyStr := string(privateKey)

	// Check PEM format
	assert.Contains(t, privateKeyStr, "-----BEGIN RSA PRIVATE KEY-----")
	assert.Contains(t, privateKeyStr, "-----END RSA PRIVATE KEY-----")

	// Check that it has content between headers
	lines := strings.Split(privateKeyStr, "\n")
	assert.Greater(t, len(lines), 2, "Private key should have more than just headers")
}

func TestGenerateKeyPair_PublicKeyFormat(t *testing.T) {
	_, publicKey, _, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	publicKeyStr := string(publicKey)

	// Check SSH public key format
	assert.True(t, strings.HasPrefix(publicKeyStr, "ssh-rsa "), "Public key should start with 'ssh-rsa '")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(publicKeyStr), "\n") || len(strings.TrimSpace(publicKeyStr)) > 0,
		"Public key should have content")

	// Should be a single line (with possible trailing newline)
	lines := strings.Split(strings.TrimSpace(publicKeyStr), "\n")
	assert.Len(t, lines, 1, "SSH public key should be a single line")
}

func TestGenerateKeyPair_FingerprintFormat(t *testing.T) {
	_, _, fingerprint, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	// SHA256 fingerprint format: SHA256:<base64>
	assert.True(t, strings.HasPrefix(fingerprint, "SHA256:"))

	// Remove prefix and check that remainder is non-empty
	remainder := strings.TrimPrefix(fingerprint, "SHA256:")
	assert.NotEmpty(t, remainder)
}

// Tests for Server Stop method (always returns nil)

func TestServer_Stop(t *testing.T) {
	server := &Server{port: 2222}

	err := server.Stop()
	assert.NoError(t, err)
}

// Integration-like tests

func TestGenerateKeyPair_UsableForSSH(t *testing.T) {
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	// Verify the keys can be used to create a SimpleKeyPair
	keyPair := SimpleKeyPair{
		PrivateKey:     privateKey,
		PublicKey:      publicKey,
		AuthorizedKeys: publicKey, // In most cases, authorized_keys is the same as public key
		Fingerprint:    fingerprint,
		CreatedAt:      time.Now(),
	}

	assert.NotNil(t, keyPair.PrivateKey)
	assert.NotNil(t, keyPair.PublicKey)
	assert.NotNil(t, keyPair.AuthorizedKeys)
	assert.NotEmpty(t, keyPair.Fingerprint)
	assert.False(t, keyPair.CreatedAt.IsZero())
}

func TestGenerateKeyPair_KeysCanBeStoredInSecret(t *testing.T) {
	privateKey, publicKey, _, err := GenerateKeyPair(2048)
	require.NoError(t, err)

	// Create a secret with the generated keys
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ssh-keys",
			Namespace: "dr-syncer",
		},
		Data: map[string][]byte{
			DefaultPrivateKeyName:     privateKey,
			DefaultPublicKeyName:      publicKey,
			DefaultAuthorizedKeysName: publicKey,
		},
	}

	assert.Equal(t, "test-ssh-keys", secret.Name)
	assert.Equal(t, "dr-syncer", secret.Namespace)
	assert.Len(t, secret.Data, 3)

	// Verify keys can be retrieved
	keys := getKeysFromSecret(secret)
	assert.Len(t, keys, 3)
}

// Tests for different key sizes

func TestGenerateKeyPair_SmallKeySize(t *testing.T) {
	// Note: 1024 bit keys are now considered insecure, but the function should still work
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(1024)

	require.NoError(t, err)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, publicKey)
	assert.NotEmpty(t, fingerprint)
}

func TestGenerateKeyPair_LargeKeySize(t *testing.T) {
	// 8192 bit key - larger but slower
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(8192)

	require.NoError(t, err)
	assert.NotNil(t, privateKey)
	assert.NotNil(t, publicKey)
	assert.NotEmpty(t, fingerprint)

	// Should be larger than 4096 bit key
	privateKey4096, _, _, _ := GenerateKeyPair(4096)
	assert.Greater(t, len(privateKey), len(privateKey4096))
}

// Test realistic SSH key scenarios

func TestSimpleKeyPair_RealisticUsage(t *testing.T) {
	privateKey, publicKey, fingerprint, err := GenerateKeyPair(DefaultKeyBits)
	require.NoError(t, err)

	keyPair := SimpleKeyPair{
		PrivateKey:     privateKey,
		PublicKey:      publicKey,
		AuthorizedKeys: publicKey,
		Fingerprint:    fingerprint,
		CreatedAt:      time.Now(),
	}

	// Verify the key pair looks correct
	assert.True(t, strings.Contains(string(keyPair.PrivateKey), "BEGIN RSA PRIVATE KEY"))
	assert.True(t, strings.HasPrefix(string(keyPair.PublicKey), "ssh-rsa"))
	assert.True(t, strings.HasPrefix(keyPair.Fingerprint, "SHA256:"))
}

// Test Server struct with various configurations

func TestServer_MultipleHostKeys(t *testing.T) {
	server := &Server{
		port:    2222,
		keyPath: "/etc/ssh/keys",
		hostKeys: []string{
			"/etc/ssh/keys/ssh_host_rsa_key",
			"/etc/ssh/keys/ssh_host_ecdsa_key",
			"/etc/ssh/keys/ssh_host_ed25519_key",
		},
	}

	assert.Len(t, server.hostKeys, 3)
	assert.Contains(t, server.hostKeys[0], "rsa")
	assert.Contains(t, server.hostKeys[1], "ecdsa")
	assert.Contains(t, server.hostKeys[2], "ed25519")
}

func TestServer_CustomPort(t *testing.T) {
	testCases := []int{22, 2222, 8022, 30022, 65535}

	for _, port := range testCases {
		server := &Server{port: port}
		assert.Equal(t, port, server.Port(), "Port should be %d", port)
	}
}
