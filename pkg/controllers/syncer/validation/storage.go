package validation

import (
	"context"
	"fmt"

	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ValidateStorageClass checks if a storage class exists in the destination cluster
func ValidateStorageClass(ctx context.Context, client kubernetes.Interface, storageClassName *string) error {
	if storageClassName == nil || *storageClassName == "" {
		return nil // No storage class specified, using cluster default
	}

	_, err := client.StorageV1().StorageClasses().Get(ctx, *storageClassName, metav1.GetOptions{})
	if err != nil {
		return syncerrors.NewWaitForNextSyncError(
			fmt.Errorf("storage class validation failed: %w", err),
			fmt.Sprintf("StorageClass/%s", *storageClassName),
		)
	}

	return nil
}
