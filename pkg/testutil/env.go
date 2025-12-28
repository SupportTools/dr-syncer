// Package testutil provides shared test utilities for DR-Syncer unit tests.
package testutil

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// TestEnv provides a pre-configured test environment for unit tests.
type TestEnv struct {
	// Scheme is the runtime scheme with all required types registered
	Scheme *runtime.Scheme

	// Ctx is a context with a reasonable timeout for tests
	Ctx context.Context

	// Cancel is the cancel function for Ctx
	Cancel context.CancelFunc

	// T is the testing.T instance
	T *testing.T
}

// NewTestEnv creates a new test environment with pre-configured scheme and context.
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	scheme := runtime.NewScheme()

	// Register standard Kubernetes types
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add client-go scheme: %v", err)
	}

	// Register DR-Syncer CRD types
	if err := drv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add DR-Syncer scheme: %v", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	// Register cleanup
	t.Cleanup(func() {
		cancel()
	})

	return &TestEnv{
		Scheme: scheme,
		Ctx:    ctx,
		Cancel: cancel,
		T:      t,
	}
}

// NewFakeClient creates a fake controller-runtime client with the test scheme.
// Optional objects can be provided to pre-populate the client.
func (e *TestEnv) NewFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(e.Scheme).
		WithObjects(objs...).
		WithStatusSubresource(&drv1alpha1.RemoteCluster{}, &drv1alpha1.NamespaceMapping{}, &drv1alpha1.ClusterMapping{}).
		Build()
}

// NewFakeClientWithStatus creates a fake client with status subresource support.
// Use this when testing status updates.
func (e *TestEnv) NewFakeClientWithStatus(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(e.Scheme).
		WithObjects(objs...).
		WithStatusSubresource(&drv1alpha1.RemoteCluster{}, &drv1alpha1.NamespaceMapping{}, &drv1alpha1.ClusterMapping{}).
		Build()
}

// ContextWithTimeout returns a new context with the specified timeout.
func (e *TestEnv) ContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.Ctx, timeout)
}

// Ptr returns a pointer to the given value.
// Useful for setting optional pointer fields in test objects.
func Ptr[T any](v T) *T {
	return &v
}

// BoolPtr returns a pointer to a bool value.
func BoolPtr(v bool) *bool {
	return &v
}

// Int32Ptr returns a pointer to an int32 value.
func Int32Ptr(v int32) *int32 {
	return &v
}

// Int64Ptr returns a pointer to an int64 value.
func Int64Ptr(v int64) *int64 {
	return &v
}

// StringPtr returns a pointer to a string value.
func StringPtr(v string) *string {
	return &v
}
