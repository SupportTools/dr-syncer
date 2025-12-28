package syncer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

func TestDeploymentScale_Struct(t *testing.T) {
	now := metav1.Now()
	scale := DeploymentScale{
		Name:     "my-deployment",
		Replicas: 3,
		SyncTime: now,
	}

	assert.Equal(t, "my-deployment", scale.Name)
	assert.Equal(t, int32(3), scale.Replicas)
	assert.Equal(t, now, scale.SyncTime)
}

func TestDeploymentScale_ZeroReplicas(t *testing.T) {
	scale := DeploymentScale{
		Name:     "scaled-down-deployment",
		Replicas: 0,
		SyncTime: metav1.Now(),
	}

	assert.Equal(t, "scaled-down-deployment", scale.Name)
	assert.Equal(t, int32(0), scale.Replicas)
}

func TestDeploymentScale_HighReplicas(t *testing.T) {
	scale := DeploymentScale{
		Name:     "high-replica-deployment",
		Replicas: 100,
		SyncTime: metav1.Now(),
	}

	assert.Equal(t, int32(100), scale.Replicas)
}

func TestDeploymentScale_MultipleDeployments(t *testing.T) {
	now := metav1.Now()
	scales := []DeploymentScale{
		{Name: "app-1", Replicas: 3, SyncTime: now},
		{Name: "app-2", Replicas: 1, SyncTime: now},
		{Name: "app-3", Replicas: 0, SyncTime: now},
	}

	assert.Len(t, scales, 3)
	assert.Equal(t, "app-1", scales[0].Name)
	assert.Equal(t, int32(3), scales[0].Replicas)
	assert.Equal(t, "app-2", scales[1].Name)
	assert.Equal(t, int32(1), scales[1].Replicas)
	assert.Equal(t, "app-3", scales[2].Name)
	assert.Equal(t, int32(0), scales[2].Replicas)
}

func TestResourceSyncer_Struct(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := &ResourceSyncer{
		scheme: scheme,
	}

	assert.NotNil(t, syncer)
	assert.NotNil(t, syncer.scheme)
	assert.Nil(t, syncer.ctrlClient)
	assert.Nil(t, syncer.sourceDynamic)
	assert.Nil(t, syncer.destDynamic)
	assert.Nil(t, syncer.sourceClient)
	assert.Nil(t, syncer.destClient)
	assert.Nil(t, syncer.sourceConfig)
	assert.Nil(t, syncer.destConfig)
}

func TestNewResourceSyncer(t *testing.T) {
	scheme := runtime.NewScheme()

	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	assert.NotNil(t, syncer)
	assert.Equal(t, scheme, syncer.scheme)
	assert.Nil(t, syncer.ctrlClient)
	assert.Nil(t, syncer.sourceDynamic)
	assert.Nil(t, syncer.destDynamic)
	assert.Nil(t, syncer.sourceClient)
	assert.Nil(t, syncer.destClient)
}

func TestResourceSyncer_SetConfigs(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	sourceConfig := &rest.Config{Host: "https://source:6443"}
	destConfig := &rest.Config{Host: "https://dest:6443"}

	syncer.SetConfigs(sourceConfig, destConfig)

	assert.Equal(t, sourceConfig, syncer.sourceConfig)
	assert.Equal(t, destConfig, syncer.destConfig)
	assert.Equal(t, "https://source:6443", syncer.sourceConfig.Host)
	assert.Equal(t, "https://dest:6443", syncer.destConfig.Host)
}

func TestResourceSyncer_SetConfigs_Nil(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	syncer.SetConfigs(nil, nil)

	assert.Nil(t, syncer.sourceConfig)
	assert.Nil(t, syncer.destConfig)
}

func TestResourceSyncer_SetConfigs_OnlySource(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	sourceConfig := &rest.Config{Host: "https://source:6443"}

	syncer.SetConfigs(sourceConfig, nil)

	assert.Equal(t, sourceConfig, syncer.sourceConfig)
	assert.Nil(t, syncer.destConfig)
}

func TestResourceSyncer_SetConfigs_OnlyDest(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	destConfig := &rest.Config{Host: "https://dest:6443"}

	syncer.SetConfigs(nil, destConfig)

	assert.Nil(t, syncer.sourceConfig)
	assert.Equal(t, destConfig, syncer.destConfig)
}

func TestDeploymentScale_SyncTimeCompare(t *testing.T) {
	time1 := metav1.NewTime(time.Now())
	time2 := metav1.NewTime(time.Now().Add(1 * time.Hour))

	scale1 := DeploymentScale{
		Name:     "deployment-1",
		Replicas: 3,
		SyncTime: time1,
	}
	scale2 := DeploymentScale{
		Name:     "deployment-2",
		Replicas: 3,
		SyncTime: time2,
	}

	// Verify time comparison works
	assert.True(t, scale1.SyncTime.Before(&scale2.SyncTime), "scale1 should be before scale2")
}

func TestDeploymentScale_RealisticNames(t *testing.T) {
	testCases := []struct {
		name     string
		replicas int32
	}{
		{"nginx-deployment", 3},
		{"api-server-v2", 5},
		{"worker-pool-processor", 10},
		{"redis-cache", 1},
		{"postgres-primary-0", 1},
		{"frontend-app-canary", 2},
	}

	for _, tc := range testCases {
		scale := DeploymentScale{
			Name:     tc.name,
			Replicas: tc.replicas,
			SyncTime: metav1.Now(),
		}

		assert.Equal(t, tc.name, scale.Name)
		assert.Equal(t, tc.replicas, scale.Replicas)
	}
}

func TestResourceSyncer_ConfigWithTLS(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	sourceConfig := &rest.Config{
		Host:        "https://source:6443",
		BearerToken: "test-token",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: false,
			CAData:   []byte("ca-cert-data"),
		},
	}
	destConfig := &rest.Config{
		Host:        "https://dest:6443",
		BearerToken: "dest-token",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	syncer.SetConfigs(sourceConfig, destConfig)

	assert.Equal(t, "https://source:6443", syncer.sourceConfig.Host)
	assert.Equal(t, "test-token", syncer.sourceConfig.BearerToken)
	assert.False(t, syncer.sourceConfig.TLSClientConfig.Insecure)

	assert.Equal(t, "https://dest:6443", syncer.destConfig.Host)
	assert.Equal(t, "dest-token", syncer.destConfig.BearerToken)
	assert.True(t, syncer.destConfig.TLSClientConfig.Insecure)
}

func TestResourceSyncer_NewWithScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	// Add a known type to the scheme
	err := metav1.AddMetaToScheme(scheme)
	assert.NoError(t, err)

	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	assert.NotNil(t, syncer)
	assert.NotNil(t, syncer.scheme)
}

func TestDeploymentScale_EmptyName(t *testing.T) {
	scale := DeploymentScale{
		Name:     "",
		Replicas: 1,
		SyncTime: metav1.Now(),
	}

	assert.Equal(t, "", scale.Name)
	assert.Equal(t, int32(1), scale.Replicas)
}

func TestDeploymentScale_NegativeReplicas(t *testing.T) {
	// While not realistic, the struct allows it
	scale := DeploymentScale{
		Name:     "test-deployment",
		Replicas: -1,
		SyncTime: metav1.Now(),
	}

	assert.Equal(t, int32(-1), scale.Replicas)
}

func TestResourceSyncer_UpdateConfigs(t *testing.T) {
	scheme := runtime.NewScheme()
	syncer := NewResourceSyncer(nil, nil, nil, nil, nil, scheme)

	// Set initial configs
	sourceConfig1 := &rest.Config{Host: "https://source1:6443"}
	destConfig1 := &rest.Config{Host: "https://dest1:6443"}
	syncer.SetConfigs(sourceConfig1, destConfig1)

	// Update configs
	sourceConfig2 := &rest.Config{Host: "https://source2:6443"}
	destConfig2 := &rest.Config{Host: "https://dest2:6443"}
	syncer.SetConfigs(sourceConfig2, destConfig2)

	// Verify configs were updated
	assert.Equal(t, "https://source2:6443", syncer.sourceConfig.Host)
	assert.Equal(t, "https://dest2:6443", syncer.destConfig.Host)
}
