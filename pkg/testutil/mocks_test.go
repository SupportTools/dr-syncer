package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

func TestNewClientMocks(t *testing.T) {
	env := NewTestEnv(t)

	mocks := NewClientMocks(env)

	assert.NotNil(t, mocks.CtrlClient, "CtrlClient should not be nil")
	assert.NotNil(t, mocks.SourceTyped, "SourceTyped should not be nil")
	assert.NotNil(t, mocks.DestTyped, "DestTyped should not be nil")
	assert.NotNil(t, mocks.SourceDynamic, "SourceDynamic should not be nil")
	assert.NotNil(t, mocks.DestDynamic, "DestDynamic should not be nil")
	assert.NotNil(t, mocks.Scheme, "Scheme should not be nil")
}

func TestNewClientMocksWithCtrlObjects(t *testing.T) {
	env := NewTestEnv(t)

	rc := NewRemoteCluster("test-cluster").Build()
	mocks := NewClientMocks(env, WithCtrlObjects(rc))

	// Verify the object exists in the controller client
	var retrieved drv1alpha1.RemoteCluster
	err := mocks.CtrlClient.Get(env.Ctx, client.ObjectKey{
		Name:      "test-cluster",
		Namespace: "default",
	}, &retrieved)

	require.NoError(t, err, "Should find the RemoteCluster in ctrl client")
	assert.Equal(t, "test-cluster", retrieved.Name)
}

func TestNewClientMocksWithSourceObjects(t *testing.T) {
	env := NewTestEnv(t)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	}
	mocks := NewClientMocks(env, WithSourceObjects(ns))

	// Verify the object exists in the source typed client
	retrieved, err := mocks.SourceTyped.CoreV1().Namespaces().Get(
		context.Background(), "test-ns", metav1.GetOptions{},
	)

	require.NoError(t, err, "Should find the namespace in source client")
	assert.Equal(t, "test-ns", retrieved.Name)
}

func TestNewClientMocksWithDestObjects(t *testing.T) {
	env := NewTestEnv(t)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
		},
		Data: map[string]string{"key": "value"},
	}
	mocks := NewClientMocks(env, WithDestObjects(cm))

	// Verify the object exists in the dest typed client
	retrieved, err := mocks.DestTyped.CoreV1().ConfigMaps("default").Get(
		context.Background(), "test-cm", metav1.GetOptions{},
	)

	require.NoError(t, err, "Should find the configmap in dest client")
	assert.Equal(t, "value", retrieved.Data["key"])
}

func TestTypedClientFactory(t *testing.T) {
	env := NewTestEnv(t)
	factory := NewTypedClientFactory(env.Scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{"password": []byte("secret")},
	}

	typedClient := factory.Create(secret)

	retrieved, err := typedClient.CoreV1().Secrets("default").Get(
		context.Background(), "test-secret", metav1.GetOptions{},
	)

	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), retrieved.Data["password"])
}

func TestDynamicClientFactory(t *testing.T) {
	env := NewTestEnv(t)
	factory := NewDynamicClientFactory(env.Scheme)

	// Create an empty dynamic client
	dynClient := factory.Create()

	assert.NotNil(t, dynClient, "Dynamic client should not be nil")
}

func TestCtrlClientFactory(t *testing.T) {
	env := NewTestEnv(t)
	factory := NewCtrlClientFactory(env.Scheme)

	nm := NewNamespaceMapping("test-mapping").
		WithSourceNamespace("source-ns").
		WithDestinationNamespace("dest-ns").
		Build()

	fakeClient := factory.Create(nm)

	var retrieved drv1alpha1.NamespaceMapping
	err := fakeClient.Get(env.Ctx, client.ObjectKey{
		Name:      "test-mapping",
		Namespace: "default",
	}, &retrieved)

	require.NoError(t, err)
	assert.Equal(t, "source-ns", retrieved.Spec.SourceNamespace)
	assert.Equal(t, "dest-ns", retrieved.Spec.DestinationNamespace)
}

func TestSyncerMocks(t *testing.T) {
	env := NewTestEnv(t)

	mocks := NewSyncerMocks(env)

	assert.NotNil(t, mocks.CtrlClient, "CtrlClient should not be nil")
	assert.NotNil(t, mocks.SourceDynamic, "SourceDynamic should not be nil")
	assert.NotNil(t, mocks.DestDynamic, "DestDynamic should not be nil")
	assert.NotNil(t, mocks.SourceClient, "SourceClient should not be nil")
	assert.NotNil(t, mocks.DestClient, "DestClient should not be nil")
	assert.NotNil(t, mocks.Scheme, "Scheme should not be nil")
}

func TestMultipleOptions(t *testing.T) {
	env := NewTestEnv(t)

	rc := NewRemoteCluster("cluster1").Build()
	srcNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "src-ns"},
	}
	destNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "dest-ns"},
	}

	mocks := NewClientMocks(env,
		WithCtrlObjects(rc),
		WithSourceObjects(srcNs),
		WithDestObjects(destNs),
	)

	// Verify all objects exist in their respective clients
	var retrievedRC drv1alpha1.RemoteCluster
	err := mocks.CtrlClient.Get(env.Ctx, client.ObjectKey{
		Name:      "cluster1",
		Namespace: "default",
	}, &retrievedRC)
	require.NoError(t, err, "RemoteCluster should exist in ctrl client")

	_, err = mocks.SourceTyped.CoreV1().Namespaces().Get(
		context.Background(), "src-ns", metav1.GetOptions{},
	)
	require.NoError(t, err, "Namespace should exist in source client")

	_, err = mocks.DestTyped.CoreV1().Namespaces().Get(
		context.Background(), "dest-ns", metav1.GetOptions{},
	)
	require.NoError(t, err, "Namespace should exist in dest client")
}
