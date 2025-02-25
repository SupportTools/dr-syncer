package syncer

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentScale represents a deployment's scale information
type DeploymentScale struct {
	Name     string
	Replicas int32
	SyncTime metav1.Time
}

// ResourceSyncer handles syncing resources between clusters
type ResourceSyncer struct {
	ctrlClient    client.Client
	sourceDynamic dynamic.Interface
	destDynamic   dynamic.Interface
	sourceClient  kubernetes.Interface
	destClient    kubernetes.Interface
	scheme        *runtime.Scheme
}

// NewResourceSyncer creates a new resource syncer
func NewResourceSyncer(ctrlClient client.Client, sourceDynamic, destDynamic dynamic.Interface, sourceClient, destClient kubernetes.Interface, scheme *runtime.Scheme) *ResourceSyncer {
	return &ResourceSyncer{
		ctrlClient:    ctrlClient,
		sourceDynamic: sourceDynamic,
		destDynamic:   destDynamic,
		sourceClient:  sourceClient,
		destClient:    destClient,
		scheme:        scheme,
	}
}
