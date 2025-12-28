package contextkeys

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test ContextKey type
func TestContextKey_String(t *testing.T) {
	key := ContextKey("test-key")
	assert.Equal(t, "test-key", key.String())
}

func TestContextKey_EmptyString(t *testing.T) {
	key := ContextKey("")
	assert.Equal(t, "", key.String())
}

// Test constant values
func TestContextKey_Constants(t *testing.T) {
	assert.Equal(t, ContextKey("k8s-config"), K8sConfigKey)
	assert.Equal(t, ContextKey("k8s-config"), ConfigKey)
	assert.Equal(t, ContextKey("pvcsync"), SyncerKey)
	assert.Equal(t, ContextKey("source-cluster"), SourceClusterKey)
	assert.Equal(t, ContextKey("dest-cluster"), DestClusterKey)
	assert.Equal(t, ContextKey("cluster-type"), ClusterTypeKey)
}

func TestContextKey_StringValues(t *testing.T) {
	assert.Equal(t, "k8s-config", K8sConfigKey.String())
	assert.Equal(t, "k8s-config", ConfigKey.String())
	assert.Equal(t, "pvcsync", SyncerKey.String())
	assert.Equal(t, "source-cluster", SourceClusterKey.String())
	assert.Equal(t, "dest-cluster", DestClusterKey.String())
	assert.Equal(t, "cluster-type", ClusterTypeKey.String())
}

// Test that K8sConfigKey and ConfigKey are equal (backward compatibility)
func TestContextKey_BackwardCompatibility(t *testing.T) {
	assert.Equal(t, K8sConfigKey, ConfigKey)
	assert.Equal(t, K8sConfigKey.String(), ConfigKey.String())
}

// Test using context keys with context.Context
func TestContextKey_WithContext(t *testing.T) {
	ctx := context.Background()

	// Store a value using our key
	ctx = context.WithValue(ctx, K8sConfigKey, "test-config")

	// Retrieve it using the same key
	val := ctx.Value(K8sConfigKey)
	assert.Equal(t, "test-config", val)
}

func TestContextKey_WithContext_AllKeys(t *testing.T) {
	ctx := context.Background()

	// Store values for all keys
	ctx = context.WithValue(ctx, K8sConfigKey, "config-value")
	ctx = context.WithValue(ctx, SyncerKey, "syncer-value")
	ctx = context.WithValue(ctx, SourceClusterKey, "source-cluster")
	ctx = context.WithValue(ctx, DestClusterKey, "dest-cluster")
	ctx = context.WithValue(ctx, ClusterTypeKey, "source")

	// Verify all values
	assert.Equal(t, "config-value", ctx.Value(K8sConfigKey))
	assert.Equal(t, "syncer-value", ctx.Value(SyncerKey))
	assert.Equal(t, "source-cluster", ctx.Value(SourceClusterKey))
	assert.Equal(t, "dest-cluster", ctx.Value(DestClusterKey))
	assert.Equal(t, "source", ctx.Value(ClusterTypeKey))
}

func TestContextKey_NoCollision(t *testing.T) {
	ctx := context.Background()

	// Different keys should not collide
	ctx = context.WithValue(ctx, SourceClusterKey, "source")
	ctx = context.WithValue(ctx, DestClusterKey, "dest")

	assert.Equal(t, "source", ctx.Value(SourceClusterKey))
	assert.Equal(t, "dest", ctx.Value(DestClusterKey))
}

func TestContextKey_TypeSafety(t *testing.T) {
	ctx := context.Background()

	// Using our typed key
	ctx = context.WithValue(ctx, K8sConfigKey, "typed-value")

	// Plain string key should not match
	val := ctx.Value("k8s-config")
	assert.Nil(t, val, "Plain string key should not match ContextKey")

	// Our typed key should match
	typedVal := ctx.Value(K8sConfigKey)
	assert.Equal(t, "typed-value", typedVal)
}

func TestContextKey_NilValue(t *testing.T) {
	ctx := context.Background()

	// Key not set should return nil
	val := ctx.Value(K8sConfigKey)
	assert.Nil(t, val)
}

func TestContextKey_CustomKey(t *testing.T) {
	// Users can create their own keys
	customKey := ContextKey("custom-key")

	ctx := context.Background()
	ctx = context.WithValue(ctx, customKey, "custom-value")

	assert.Equal(t, "custom-value", ctx.Value(customKey))
}

// Test that keys are unique
func TestContextKey_Uniqueness(t *testing.T) {
	keys := []ContextKey{
		K8sConfigKey,
		SyncerKey,
		SourceClusterKey,
		DestClusterKey,
		ClusterTypeKey,
	}

	// Check all pairs
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			// ConfigKey is intentionally equal to K8sConfigKey, so skip that comparison
			if keys[i] == ConfigKey && keys[j] == K8sConfigKey {
				continue
			}
			if keys[j] == ConfigKey && keys[i] == K8sConfigKey {
				continue
			}
			assert.NotEqual(t, keys[i], keys[j], "Keys should be unique: %s vs %s", keys[i], keys[j])
		}
	}
}
