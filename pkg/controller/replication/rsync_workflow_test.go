package replication

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
)

func TestCleanupResources_NilDeployment(t *testing.T) {
	// Create a minimal PVCSyncer
	syncer := &PVCSyncer{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		DestinationConfig:    &rest.Config{Host: "https://test-cluster:6443"},
	}

	// This should not panic when passed nil
	ctx := context.Background()
	syncer.cleanupResources(ctx, nil)

	// If we get here, the function handled nil correctly
	assert.True(t, true, "cleanupResources should handle nil deployment gracefully")
}

func TestPVCSyncer_NamespaceAssignment(t *testing.T) {
	syncer := &PVCSyncer{}

	// Test that namespaces can be set
	syncer.SourceNamespace = "source-namespace"
	syncer.DestinationNamespace = "destination-namespace"

	assert.Equal(t, "source-namespace", syncer.SourceNamespace)
	assert.Equal(t, "destination-namespace", syncer.DestinationNamespace)
}

func TestPVCSyncer_ClientsNil(t *testing.T) {
	syncer := &PVCSyncer{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
	}

	// Verify clients are nil by default
	assert.Nil(t, syncer.SourceClient)
	assert.Nil(t, syncer.DestinationClient)
	assert.Nil(t, syncer.SourceConfig)
	assert.Nil(t, syncer.DestinationConfig)
}

func TestPVCSyncer_WithConfig(t *testing.T) {
	sourceConfig := &rest.Config{
		Host: "https://source-cluster:6443",
	}
	destConfig := &rest.Config{
		Host: "https://dest-cluster:6443",
	}

	syncer := &PVCSyncer{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		SourceConfig:         sourceConfig,
		DestinationConfig:    destConfig,
	}

	assert.Equal(t, "https://source-cluster:6443", syncer.SourceConfig.Host)
	assert.Equal(t, "https://dest-cluster:6443", syncer.DestinationConfig.Host)
}

func TestSyncerContextKey(t *testing.T) {
	// Verify the syncer context key is defined and can be used
	ctx := context.Background()

	// Create a test syncer
	syncer := &PVCSyncer{
		SourceNamespace: "test-ns",
	}

	// Put the syncer in context using the exported key
	syncerCtx := context.WithValue(ctx, syncerKey, syncer)

	// Retrieve and verify
	retrieved := syncerCtx.Value(syncerKey)
	assert.NotNil(t, retrieved)

	// Type assert and verify
	if s, ok := retrieved.(*PVCSyncer); ok {
		assert.Equal(t, "test-ns", s.SourceNamespace)
	} else {
		t.Fatal("Failed to retrieve PVCSyncer from context")
	}
}

func TestPVCSyncer_AllFields(t *testing.T) {
	sourceConfig := &rest.Config{Host: "https://source:6443"}
	destConfig := &rest.Config{Host: "https://dest:6443"}

	syncer := &PVCSyncer{
		SourceNamespace:      "prod-app",
		DestinationNamespace: "dr-app",
		SourceConfig:         sourceConfig,
		DestinationConfig:    destConfig,
	}

	// Verify all fields
	assert.Equal(t, "prod-app", syncer.SourceNamespace)
	assert.Equal(t, "dr-app", syncer.DestinationNamespace)
	assert.Equal(t, "https://source:6443", syncer.SourceConfig.Host)
	assert.Equal(t, "https://dest:6443", syncer.DestinationConfig.Host)
}

func TestCleanupResources_WithContext(t *testing.T) {
	// Create a PVCSyncer with destination config
	syncer := &PVCSyncer{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		DestinationConfig:    &rest.Config{Host: "https://test-cluster:6443"},
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Call cleanupResources with nil - should not panic even with cancelled context
	cancel()
	syncer.cleanupResources(ctx, nil)

	// If we get here, the function handled nil correctly even with cancelled context
	assert.True(t, true, "cleanupResources should handle nil deployment with cancelled context")
}

func TestPVCSyncer_EmptyNamespaces(t *testing.T) {
	syncer := &PVCSyncer{
		SourceNamespace:      "",
		DestinationNamespace: "",
	}

	// Empty strings are valid
	assert.Equal(t, "", syncer.SourceNamespace)
	assert.Equal(t, "", syncer.DestinationNamespace)
}

func TestPVCSyncer_RealisticNamespaces(t *testing.T) {
	testCases := []struct {
		sourceNS string
		destNS   string
	}{
		{"production-app", "dr-production-app"},
		{"kube-system", "dr-kube-system"},
		{"my-app-v2", "my-app-v2-dr"},
		{"app.example.com", "dr-app.example.com"},
		{"ns-with-numbers-123", "dr-ns-123"},
	}

	for _, tc := range testCases {
		syncer := &PVCSyncer{
			SourceNamespace:      tc.sourceNS,
			DestinationNamespace: tc.destNS,
		}

		assert.Equal(t, tc.sourceNS, syncer.SourceNamespace, "source namespace mismatch for %s", tc.sourceNS)
		assert.Equal(t, tc.destNS, syncer.DestinationNamespace, "dest namespace mismatch for %s", tc.destNS)
	}
}
