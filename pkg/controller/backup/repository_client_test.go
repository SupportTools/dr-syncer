package backup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers"
)

// --- parseSnapshotCount Tests ---

func TestParseSnapshotCount_ValidJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty array", "[]", 0},
		{"single snapshot", `[{"id":"abc123"}]`, 1},
		{"multiple snapshots", `[{"id":"1"},{"id":"2"},{"id":"3"}]`, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, err := parseSnapshotCount(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, count)
		})
	}
}

func TestParseSnapshotCount_EmptyString(t *testing.T) {
	count, err := parseSnapshotCount("")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestParseSnapshotCount_NullString(t *testing.T) {
	count, err := parseSnapshotCount("null")
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestParseSnapshotCount_InvalidJSON(t *testing.T) {
	_, err := parseSnapshotCount("not json at all")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse snapshot list JSON")
}

func TestParseSnapshotCount_ObjectInsteadOfArray(t *testing.T) {
	_, err := parseSnapshotCount(`{"key": "value"}`)
	assert.Error(t, err)
}

// --- parseBlobSize Tests ---

func TestParseBlobSize_TotalSizeFloat(t *testing.T) {
	size, err := parseBlobSize(`{"totalSize": 1048576}`)
	assert.NoError(t, err)
	assert.Equal(t, int64(1048576), size)
}

func TestParseBlobSize_TotalSizeString(t *testing.T) {
	size, err := parseBlobSize(`{"totalSize": "2097152"}`)
	assert.NoError(t, err)
	assert.Equal(t, int64(2097152), size)
}

func TestParseBlobSize_TotalSizeUnderscore(t *testing.T) {
	size, err := parseBlobSize(`{"total_size": 512000}`)
	assert.NoError(t, err)
	assert.Equal(t, int64(512000), size)
}

func TestParseBlobSize_TotalSizePascalCase(t *testing.T) {
	size, err := parseBlobSize(`{"TotalSize": 999}`)
	assert.NoError(t, err)
	assert.Equal(t, int64(999), size)
}

func TestParseBlobSize_MissingKey(t *testing.T) {
	size, err := parseBlobSize(`{"blobCount": 42}`)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestParseBlobSize_EmptyString(t *testing.T) {
	size, err := parseBlobSize("")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestParseBlobSize_InvalidJSON(t *testing.T) {
	_, err := parseBlobSize("not json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse blob stats JSON")
}

func TestParseBlobSize_NonNumericStringValue(t *testing.T) {
	// totalSize is a string that can't be parsed as int — should return 0 with no error
	size, err := parseBlobSize(`{"totalSize": "not-a-number"}`)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

// --- buildS3Args Tests ---

func TestBuildS3Args_MinimalConfig(t *testing.T) {
	config := drv1alpha1.S3Config{
		Bucket:   "my-bucket",
		Endpoint: "s3.amazonaws.com",
	}

	args := buildS3Args(config)

	assert.Contains(t, args, "--bucket=my-bucket")
	assert.Contains(t, args, "--endpoint=s3.amazonaws.com")
	assert.Len(t, args, 2)
}

func TestBuildS3Args_WithRegion(t *testing.T) {
	config := drv1alpha1.S3Config{
		Bucket:   "my-bucket",
		Endpoint: "s3.amazonaws.com",
		Region:   "us-west-2",
	}

	args := buildS3Args(config)

	assert.Contains(t, args, "--bucket=my-bucket")
	assert.Contains(t, args, "--endpoint=s3.amazonaws.com")
	assert.Contains(t, args, "--region=us-west-2")
	assert.Len(t, args, 3)
}

func TestBuildS3Args_WithPrefix(t *testing.T) {
	config := drv1alpha1.S3Config{
		Bucket:     "my-bucket",
		Endpoint:   "minio.local:9000",
		PathPrefix: "dr-syncer/backups",
	}

	args := buildS3Args(config)

	assert.Contains(t, args, "--bucket=my-bucket")
	assert.Contains(t, args, "--endpoint=minio.local:9000")
	assert.Contains(t, args, "--prefix=dr-syncer/backups")
	assert.Len(t, args, 3)
}

func TestBuildS3Args_FullConfig(t *testing.T) {
	config := drv1alpha1.S3Config{
		Bucket:     "my-bucket",
		Endpoint:   "minio.local:9000",
		Region:     "us-east-1",
		PathPrefix: "backups/prod",
	}

	args := buildS3Args(config)

	assert.Contains(t, args, "--bucket=my-bucket")
	assert.Contains(t, args, "--endpoint=minio.local:9000")
	assert.Contains(t, args, "--region=us-east-1")
	assert.Contains(t, args, "--prefix=backups/prod")
	assert.Len(t, args, 4)
}

// --- Credential Retrieval Tests ---

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = drv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

func TestGetS3Credentials_Success(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: "default"},
		Data: map[string][]byte{
			"accessKeyID":     []byte("AKIATEST"),
			"secretAccessKey": []byte("SECRETTEST"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	accessKey, secretKey, err := client.getS3Credentials(context.Background(), drv1alpha1.SecretReference{
		Name:      "s3-creds",
		Namespace: "default",
	}, "fallback-ns")

	assert.NoError(t, err)
	assert.Equal(t, "AKIATEST", accessKey)
	assert.Equal(t, "SECRETTEST", secretKey)
}

func TestGetS3Credentials_FallbackNamespace(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: "my-ns"},
		Data: map[string][]byte{
			"accessKeyID":     []byte("AKIATEST"),
			"secretAccessKey": []byte("SECRETTEST"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	accessKey, secretKey, err := client.getS3Credentials(context.Background(), drv1alpha1.SecretReference{
		Name:      "s3-creds",
		Namespace: "", // empty -> fallback
	}, "my-ns")

	assert.NoError(t, err)
	assert.Equal(t, "AKIATEST", accessKey)
	assert.Equal(t, "SECRETTEST", secretKey)
}

func TestGetS3Credentials_SecretNotFound(t *testing.T) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	_, _, err := client.getS3Credentials(context.Background(), drv1alpha1.SecretReference{
		Name:      "missing-secret",
		Namespace: "default",
	}, "default")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get S3 credentials secret")
}

func TestGetS3Credentials_MissingAccessKeyID(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: "default"},
		Data:       map[string][]byte{"secretAccessKey": []byte("key")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	_, _, err := client.getS3Credentials(context.Background(), drv1alpha1.SecretReference{
		Name:      "s3-creds",
		Namespace: "default",
	}, "default")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'accessKeyID' key")
}

func TestGetS3Credentials_MissingSecretAccessKey(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: "default"},
		Data:       map[string][]byte{"accessKeyID": []byte("key")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	_, _, err := client.getS3Credentials(context.Background(), drv1alpha1.SecretReference{
		Name:      "s3-creds",
		Namespace: "default",
	}, "default")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'secretAccessKey' key")
}

func TestGetKopiaPassword_Success(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kopia-enc", Namespace: "default"},
		Data:       map[string][]byte{"password": []byte("my-passphrase")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	pw, err := client.getKopiaPassword(context.Background(), drv1alpha1.SecretReference{
		Name:      "kopia-enc",
		Namespace: "default",
	}, "fallback")

	assert.NoError(t, err)
	assert.Equal(t, "my-passphrase", pw)
}

func TestGetKopiaPassword_FallbackNamespace(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kopia-enc", Namespace: "my-ns"},
		Data:       map[string][]byte{"password": []byte("pass123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	pw, err := client.getKopiaPassword(context.Background(), drv1alpha1.SecretReference{
		Name:      "kopia-enc",
		Namespace: "",
	}, "my-ns")

	assert.NoError(t, err)
	assert.Equal(t, "pass123", pw)
}

func TestGetKopiaPassword_SecretNotFound(t *testing.T) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	_, err := client.getKopiaPassword(context.Background(), drv1alpha1.SecretReference{
		Name:      "missing",
		Namespace: "default",
	}, "default")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get kopia encryption secret")
}

func TestGetKopiaPassword_MissingPasswordKey(t *testing.T) {
	scheme := newTestScheme()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kopia-enc", Namespace: "default"},
		Data:       map[string][]byte{"wrong-key": []byte("value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	client := &KopiaRepositoryClient{client: fakeClient}

	_, err := client.getKopiaPassword(context.Background(), drv1alpha1.SecretReference{
		Name:      "kopia-enc",
		Namespace: "default",
	}, "default")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'password' key")
}

// --- NewKopiaRepositoryClient Test ---

func TestNewKopiaRepositoryClient(t *testing.T) {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	client := NewKopiaRepositoryClient(fakeClient)

	require.NotNil(t, client)
	assert.Equal(t, "kopia", client.kopiaBinary)
	assert.NotNil(t, client.client)
}

// --- Interface compliance ---

func TestKopiaRepositoryClient_ImplementsInterface(t *testing.T) {
	// Compile-time check is already in repository_client.go, but verify at test time too
	var _ controllers.RepositoryClient = (*KopiaRepositoryClient)(nil)
}

// --- maxStderrLen constant ---

func TestMaxStderrLen(t *testing.T) {
	assert.Equal(t, 512, maxStderrLen)
}
