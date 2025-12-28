package testutil

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// ClientMocks holds all mock clients needed for testing DR-Syncer components.
type ClientMocks struct {
	// CtrlClient is the controller-runtime fake client
	CtrlClient client.Client

	// SourceTyped is a fake kubernetes.Interface for the source cluster
	SourceTyped kubernetes.Interface

	// DestTyped is a fake kubernetes.Interface for the destination cluster
	DestTyped kubernetes.Interface

	// SourceDynamic is a fake dynamic.Interface for the source cluster
	SourceDynamic dynamic.Interface

	// DestDynamic is a fake dynamic.Interface for the destination cluster
	DestDynamic dynamic.Interface

	// Scheme is the runtime scheme with all types registered
	Scheme *runtime.Scheme
}

// ClientMocksOption is a functional option for configuring ClientMocks.
type ClientMocksOption func(*clientMocksConfig)

type clientMocksConfig struct {
	ctrlObjects      []client.Object
	sourceObjects    []runtime.Object
	destObjects      []runtime.Object
	sourceDynObjects []runtime.Object
	destDynObjects   []runtime.Object
}

// WithCtrlObjects adds objects to the controller-runtime fake client.
func WithCtrlObjects(objs ...client.Object) ClientMocksOption {
	return func(c *clientMocksConfig) {
		c.ctrlObjects = append(c.ctrlObjects, objs...)
	}
}

// WithSourceObjects adds objects to the source typed client.
func WithSourceObjects(objs ...runtime.Object) ClientMocksOption {
	return func(c *clientMocksConfig) {
		c.sourceObjects = append(c.sourceObjects, objs...)
	}
}

// WithDestObjects adds objects to the destination typed client.
func WithDestObjects(objs ...runtime.Object) ClientMocksOption {
	return func(c *clientMocksConfig) {
		c.destObjects = append(c.destObjects, objs...)
	}
}

// WithSourceDynamicObjects adds objects to the source dynamic client.
func WithSourceDynamicObjects(objs ...runtime.Object) ClientMocksOption {
	return func(c *clientMocksConfig) {
		c.sourceDynObjects = append(c.sourceDynObjects, objs...)
	}
}

// WithDestDynamicObjects adds objects to the destination dynamic client.
func WithDestDynamicObjects(objs ...runtime.Object) ClientMocksOption {
	return func(c *clientMocksConfig) {
		c.destDynObjects = append(c.destDynObjects, objs...)
	}
}

// NewClientMocks creates a new ClientMocks with all mock clients initialized.
// Use functional options to pre-populate clients with objects.
func NewClientMocks(env *TestEnv, opts ...ClientMocksOption) *ClientMocks {
	cfg := &clientMocksConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return &ClientMocks{
		CtrlClient:    newCtrlClient(env.Scheme, cfg.ctrlObjects...),
		SourceTyped:   fake.NewSimpleClientset(cfg.sourceObjects...),
		DestTyped:     fake.NewSimpleClientset(cfg.destObjects...),
		SourceDynamic: dynamicfake.NewSimpleDynamicClient(env.Scheme, cfg.sourceDynObjects...),
		DestDynamic:   dynamicfake.NewSimpleDynamicClient(env.Scheme, cfg.destDynObjects...),
		Scheme:        env.Scheme,
	}
}

// newCtrlClient creates a controller-runtime fake client with status subresource support.
func newCtrlClient(scheme *runtime.Scheme, objs ...client.Object) client.Client {
	return fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(
			&drv1alpha1.RemoteCluster{},
			&drv1alpha1.NamespaceMapping{},
			&drv1alpha1.ClusterMapping{},
		).
		Build()
}

// TypedClientFactory creates typed kubernetes.Interface clients.
type TypedClientFactory struct {
	scheme *runtime.Scheme
}

// NewTypedClientFactory creates a new TypedClientFactory.
func NewTypedClientFactory(scheme *runtime.Scheme) *TypedClientFactory {
	return &TypedClientFactory{scheme: scheme}
}

// Create creates a new fake kubernetes.Interface with the given objects.
func (f *TypedClientFactory) Create(objs ...runtime.Object) kubernetes.Interface {
	return fake.NewSimpleClientset(objs...)
}

// DynamicClientFactory creates dynamic.Interface clients.
type DynamicClientFactory struct {
	scheme *runtime.Scheme
}

// NewDynamicClientFactory creates a new DynamicClientFactory.
func NewDynamicClientFactory(scheme *runtime.Scheme) *DynamicClientFactory {
	return &DynamicClientFactory{scheme: scheme}
}

// Create creates a new fake dynamic.Interface with the given objects.
func (f *DynamicClientFactory) Create(objs ...runtime.Object) dynamic.Interface {
	return dynamicfake.NewSimpleDynamicClient(f.scheme, objs...)
}

// CtrlClientFactory creates controller-runtime client.Client instances.
type CtrlClientFactory struct {
	scheme *runtime.Scheme
}

// NewCtrlClientFactory creates a new CtrlClientFactory.
func NewCtrlClientFactory(scheme *runtime.Scheme) *CtrlClientFactory {
	return &CtrlClientFactory{scheme: scheme}
}

// Create creates a new fake client.Client with the given objects.
func (f *CtrlClientFactory) Create(objs ...client.Object) client.Client {
	return newCtrlClient(f.scheme, objs...)
}

// SyncerMocks provides mocks specifically for ResourceSyncer testing.
// This mirrors the structure in pkg/controllers/syncer/types.go.
type SyncerMocks struct {
	CtrlClient    client.Client
	SourceDynamic dynamic.Interface
	DestDynamic   dynamic.Interface
	SourceClient  kubernetes.Interface
	DestClient    kubernetes.Interface
	Scheme        *runtime.Scheme
}

// NewSyncerMocks creates mocks for testing ResourceSyncer.
func NewSyncerMocks(env *TestEnv, opts ...ClientMocksOption) *SyncerMocks {
	mocks := NewClientMocks(env, opts...)
	return &SyncerMocks{
		CtrlClient:    mocks.CtrlClient,
		SourceDynamic: mocks.SourceDynamic,
		DestDynamic:   mocks.DestDynamic,
		SourceClient:  mocks.SourceTyped,
		DestClient:    mocks.DestTyped,
		Scheme:        mocks.Scheme,
	}
}
