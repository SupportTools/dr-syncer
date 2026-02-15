package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers"
	"github.com/supporttools/dr-syncer/pkg/logging"
)

// maxStderrLen is the maximum number of characters from stderr included in error
// messages. This prevents unbounded strings from being written to etcd via status updates.
const maxStderrLen = 512

// KopiaRepositoryClient implements controllers.RepositoryClient by wrapping
// the Kopia CLI binary. Credentials are read from Kubernetes secrets and
// passed via environment variables to avoid writing them to disk.
type KopiaRepositoryClient struct {
	client client.Client
	// kopiaBinary allows overriding the kopia binary path for testing
	kopiaBinary string
}

// NewKopiaRepositoryClient creates a new KopiaRepositoryClient.
func NewKopiaRepositoryClient(c client.Client) *KopiaRepositoryClient {
	return &KopiaRepositoryClient{
		client:      c,
		kopiaBinary: "kopia",
	}
}

// Compile-time check that KopiaRepositoryClient satisfies RepositoryClient.
var _ controllers.RepositoryClient = (*KopiaRepositoryClient)(nil)

// InitializeRepository creates or connects to a Kopia repository in S3.
// It first attempts to connect to an existing repository. If that fails
// (because the repository doesn't exist yet), it creates a new one.
func (k *KopiaRepositoryClient) InitializeRepository(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) error {
	env, cleanup, err := k.buildEnv(ctx, s3Config, kopiaConfig, namespace)
	if err != nil {
		return fmt.Errorf("failed to build credentials environment: %w", err)
	}
	defer cleanup()

	s3Args := buildS3Args(s3Config)

	// Try connecting to existing repository first
	connectArgs := make([]string, 0, 3+len(s3Args)+2)
	connectArgs = append(connectArgs, "repository", "connect", "s3")
	connectArgs = append(connectArgs, s3Args...)
	connectArgs = append(connectArgs, "--override-hostname=dr-syncer", "--override-username=dr-syncer")

	_, connectErr := k.executeKopia(ctx, env, connectArgs)
	if connectErr == nil {
		logging.LogInfo(nil, fmt.Sprintf("connected to existing Kopia repository at s3://%s/%s", s3Config.Bucket, s3Config.PathPrefix))
		return nil
	}

	// Log the connect error for debugging before falling through to create
	logging.LogDetail(nil, fmt.Sprintf("repository connect failed (will attempt create): %v", connectErr))

	// Connection failed — create a new repository
	logging.LogInfo(nil, fmt.Sprintf("creating new Kopia repository at s3://%s/%s", s3Config.Bucket, s3Config.PathPrefix))

	createArgs := make([]string, 0, 3+len(s3Args)+2)
	createArgs = append(createArgs, "repository", "create", "s3")
	createArgs = append(createArgs, s3Args...)
	createArgs = append(createArgs, "--override-hostname=dr-syncer", "--override-username=dr-syncer")

	if _, err := k.executeKopia(ctx, env, createArgs); err != nil {
		return fmt.Errorf("failed to create kopia repository (connect error: %v): %w", connectErr, err)
	}

	logging.LogInfo(nil, fmt.Sprintf("Kopia repository created at s3://%s/%s", s3Config.Bucket, s3Config.PathPrefix))
	return nil
}

// CheckHealth verifies repository connectivity and returns repository stats
// (total size and snapshot count).
func (k *KopiaRepositoryClient) CheckHealth(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) (*controllers.RepositoryStats, error) {
	env, cleanup, err := k.buildEnv(ctx, s3Config, kopiaConfig, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to build credentials environment: %w", err)
	}
	defer cleanup()

	s3Args := buildS3Args(s3Config)

	// Connect to the repository (ensures connectivity)
	connectArgs := make([]string, 0, 3+len(s3Args)+2)
	connectArgs = append(connectArgs, "repository", "connect", "s3")
	connectArgs = append(connectArgs, s3Args...)
	connectArgs = append(connectArgs, "--override-hostname=dr-syncer", "--override-username=dr-syncer")

	if _, err := k.executeKopia(ctx, env, connectArgs); err != nil {
		return nil, fmt.Errorf("failed to connect to repository for health check: %w", err)
	}

	stats := &controllers.RepositoryStats{}

	// Get snapshot count
	snapshotOutput, err := k.executeKopia(ctx, env, []string{"snapshot", "list", "--all", "--json"})
	if err != nil {
		logging.LogWarn(nil, fmt.Sprintf("failed to list snapshots: %v", err))
	} else {
		count, err := parseSnapshotCount(snapshotOutput)
		if err != nil {
			logging.LogWarn(nil, fmt.Sprintf("failed to parse snapshot list: %v", err))
		} else {
			stats.SnapshotCount = int32(count)
		}
	}

	// Estimate repository size from blob stats
	blobOutput, err := k.executeKopia(ctx, env, []string{"blob", "stats", "--json"})
	if err != nil {
		logging.LogWarn(nil, fmt.Sprintf("failed to get blob stats: %v", err))
	} else {
		size, err := parseBlobSize(blobOutput)
		if err != nil {
			logging.LogWarn(nil, fmt.Sprintf("failed to parse blob stats: %v", err))
		} else {
			stats.RepositorySize = size
		}
	}

	// Record repository metrics (same package, no import cycle)
	repoName := s3Config.Bucket
	if s3Config.PathPrefix != "" {
		repoName = s3Config.Bucket + "/" + s3Config.PathPrefix
	}
	RecordRepositoryStats(repoName, stats.RepositorySize, stats.SnapshotCount)

	return stats, nil
}

// RunMaintenance triggers full Kopia repository maintenance including
// index compaction, blob garbage collection, and snapshot garbage collection.
func (k *KopiaRepositoryClient) RunMaintenance(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) error {
	env, cleanup, err := k.buildEnv(ctx, s3Config, kopiaConfig, namespace)
	if err != nil {
		return fmt.Errorf("failed to build credentials environment: %w", err)
	}
	defer cleanup()

	s3Args := buildS3Args(s3Config)

	// Connect to the repository first
	connectArgs := make([]string, 0, 3+len(s3Args)+2)
	connectArgs = append(connectArgs, "repository", "connect", "s3")
	connectArgs = append(connectArgs, s3Args...)
	connectArgs = append(connectArgs, "--override-hostname=dr-syncer", "--override-username=dr-syncer")

	if _, err := k.executeKopia(ctx, env, connectArgs); err != nil {
		return fmt.Errorf("failed to connect to repository for maintenance: %w", err)
	}

	// Run full maintenance with default safety (protects in-progress snapshots)
	logging.LogInfo(nil, "running Kopia repository maintenance")
	if _, err := k.executeKopia(ctx, env, []string{"maintenance", "run", "--full"}); err != nil {
		return fmt.Errorf("repository maintenance failed: %w", err)
	}

	logging.LogInfo(nil, "Kopia repository maintenance completed")
	return nil
}

// buildEnv reads S3 and Kopia encryption credentials from Kubernetes secrets,
// creates a per-invocation temporary directory for Kopia config isolation, and
// returns an environment slice and a cleanup function.
func (k *KopiaRepositoryClient) buildEnv(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) ([]string, func(), error) {
	accessKey, secretKey, err := k.getS3Credentials(ctx, s3Config.CredentialsSecretRef, namespace)
	if err != nil {
		return nil, nil, err
	}

	password, err := k.getKopiaPassword(ctx, kopiaConfig.EncryptionSecretRef, namespace)
	if err != nil {
		return nil, nil, err
	}

	// Create a unique temporary directory per invocation so concurrent reconciliations
	// don't overwrite each other's Kopia config files (~/.config/kopia/repository.config).
	tmpDir, err := os.MkdirTemp("", "kopia-dr-syncer-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp dir for kopia config: %w", err)
	}
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	env := []string{
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", accessKey),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", secretKey),
		fmt.Sprintf("KOPIA_PASSWORD=%s", password),
		"KOPIA_CHECK_FOR_UPDATES=false",
		fmt.Sprintf("HOME=%s", tmpDir),
	}

	// Inherit PATH and SSL-related env vars from the parent process so that
	// the kopia binary can be found and custom CA certificates work.
	for _, key := range []string{"PATH", "SSL_CERT_DIR", "SSL_CERT_FILE"} {
		if val := os.Getenv(key); val != "" {
			env = append(env, fmt.Sprintf("%s=%s", key, val))
		}
	}

	return env, cleanup, nil
}

// getS3Credentials reads the S3 access credentials from a Kubernetes secret.
// If the SecretReference has no namespace, the provided fallback namespace is used.
func (k *KopiaRepositoryClient) getS3Credentials(ctx context.Context, ref drv1alpha1.SecretReference, fallbackNamespace string) (accessKey, secretKey string, err error) {
	ns := ref.Namespace
	if ns == "" {
		ns = fallbackNamespace
	}

	var secret corev1.Secret
	if err := k.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, &secret); err != nil {
		return "", "", fmt.Errorf("failed to get S3 credentials secret %s/%s: %w", ns, ref.Name, err)
	}

	ak, ok := secret.Data["accessKeyID"]
	if !ok {
		return "", "", fmt.Errorf("S3 credentials secret %s/%s missing 'accessKeyID' key", ns, ref.Name)
	}
	sk, ok := secret.Data["secretAccessKey"]
	if !ok {
		return "", "", fmt.Errorf("S3 credentials secret %s/%s missing 'secretAccessKey' key", ns, ref.Name)
	}

	return string(ak), string(sk), nil
}

// getKopiaPassword reads the Kopia repository encryption password from a Kubernetes secret.
func (k *KopiaRepositoryClient) getKopiaPassword(ctx context.Context, ref drv1alpha1.SecretReference, fallbackNamespace string) (string, error) {
	ns := ref.Namespace
	if ns == "" {
		ns = fallbackNamespace
	}

	var secret corev1.Secret
	if err := k.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, &secret); err != nil {
		return "", fmt.Errorf("failed to get kopia encryption secret %s/%s: %w", ns, ref.Name, err)
	}

	pw, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("kopia encryption secret %s/%s missing 'password' key", ns, ref.Name)
	}

	return string(pw), nil
}

// executeKopia runs a Kopia CLI command with the given environment and arguments.
// It returns stdout on success, or an error with truncated stderr context.
func (k *KopiaRepositoryClient) executeKopia(ctx context.Context, env []string, args []string) (string, error) {
	binary := k.kopiaBinary
	if binary == "" {
		binary = "kopia"
	}

	logging.LogDetail(nil, fmt.Sprintf("executing: %s %s", binary, strings.Join(args, " ")))

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if len(stderrStr) > maxStderrLen {
			stderrStr = stderrStr[:maxStderrLen] + "...(truncated)"
		}
		if stderrStr != "" {
			return "", fmt.Errorf("kopia %s failed: %w: %s", args[0], err, stderrStr)
		}
		return "", fmt.Errorf("kopia %s failed: %w", args[0], err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// buildS3Args constructs the S3 backend arguments for Kopia CLI commands.
func buildS3Args(s3Config drv1alpha1.S3Config) []string {
	args := []string{
		"--bucket=" + s3Config.Bucket,
		"--endpoint=" + s3Config.Endpoint,
	}

	if s3Config.Region != "" {
		args = append(args, "--region="+s3Config.Region)
	}
	if s3Config.PathPrefix != "" {
		args = append(args, "--prefix="+s3Config.PathPrefix)
	}

	return args
}

// --- Kopia JSON output parsing ---

// parseSnapshotCount parses the JSON output of `kopia snapshot list --all --json`
// and returns the total number of snapshots.
func parseSnapshotCount(jsonOutput string) (int, error) {
	if jsonOutput == "" || jsonOutput == "null" {
		return 0, nil
	}

	// kopia snapshot list --json returns an array of snapshot manifests
	var snapshots []json.RawMessage
	if err := json.Unmarshal([]byte(jsonOutput), &snapshots); err != nil {
		return 0, fmt.Errorf("failed to parse snapshot list JSON: %w", err)
	}
	return len(snapshots), nil
}

// parseBlobSize parses the JSON output of `kopia blob stats --json`
// and returns the total repository size in bytes.
func parseBlobSize(jsonOutput string) (int64, error) {
	if jsonOutput == "" {
		return 0, nil
	}

	// kopia blob stats --json output varies by version; try common fields
	var result map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		return 0, fmt.Errorf("failed to parse blob stats JSON: %w", err)
	}

	// Look for "totalSize" or "total_size" field
	for _, key := range []string{"totalSize", "total_size", "TotalSize"} {
		if val, ok := result[key]; ok {
			switch v := val.(type) {
			case float64:
				return int64(v), nil
			case string:
				n, err := strconv.ParseInt(v, 10, 64)
				if err == nil {
					return n, nil
				}
			}
		}
	}

	return 0, nil
}
