package syncer

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureNamespaceExists ensures the destination namespace exists
func EnsureNamespaceExists(ctx context.Context, client kubernetes.Interface, namespace, sourceNamespace string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check namespace existence: %w", err)
	}

	// Create namespace with labels from source if available
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"dr-syncer.io/source-namespace": sourceNamespace,
			},
		},
	}

	_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	return nil
}

// DeploymentSyncResult stores information about a synced deployment
type DeploymentSyncResult struct {
	Name     string
	Replicas int32
	SyncTime metav1.Time
}
