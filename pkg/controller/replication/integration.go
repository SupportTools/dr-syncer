package replication

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// ReplicationManager connects the legacy and new replication systems
type ReplicationManager struct {
	// sourceClient is the client for the source cluster
	sourceClient client.Client
	
	// destClient is the client for the destination cluster
	destClient client.Client
	
	// sourceK8sClient is the Kubernetes client for the source cluster
	sourceK8sClient kubernetes.Interface
	
	// destK8sClient is the Kubernetes client for the destination cluster
	destK8sClient kubernetes.Interface
	
	// sourceConfig is the config for the source cluster
	sourceConfig *rest.Config
	
	// destConfig is the config for the destination cluster
	destConfig *rest.Config
	
	// replicationController is the new replication controller
	replicationController *RsyncReplicationController
}

// NewReplicationManager creates a new replication manager
func NewReplicationManager(sourceClient, destClient client.Client, sourceConfig, destConfig *rest.Config,
	sourceK8sClient, destK8sClient kubernetes.Interface) (*ReplicationManager, error) {
	
	// Initialize the replication controller
	controller, err := NewRsyncReplicationController(sourceClient, destClient, sourceConfig, destConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize replication controller: %v", err)
	}
	
	return &ReplicationManager{
		sourceClient:         sourceClient,
		destClient:           destClient,
		sourceK8sClient:      sourceK8sClient,
		destK8sClient:        destK8sClient,
		sourceConfig:         sourceConfig,
		destConfig:           destConfig,
		replicationController: controller,
	}, nil
}

// ReplicatePVC initiates PVC replication using the controller
func (m *ReplicationManager) ReplicatePVC(ctx context.Context, sourceNS, destNS, pvcName string) error {
	log := logrus.WithFields(logrus.Fields{
		"component":        "replication-manager",
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
	})
	
	log.Info("Initiating PVC replication")
	
	// Use the new controller for replication
	err := m.replicationController.ReplicatePVC(ctx, sourceNS, destNS, pvcName)
	if err != nil {
		log.WithField("error", err).Error("PVC replication failed")
		return err
	}
	
	log.Info("PVC replication completed successfully")
	return nil
}

// PerformNamespaceMappingSync performs sync for a namespace mapping
func (m *ReplicationManager) PerformNamespaceMappingSync(ctx context.Context, mapping *drv1alpha1.NamespaceMapping) error {
	log := logrus.WithFields(logrus.Fields{
		"component":        "replication-manager",
		"namespacemapping": mapping.Name,
		"source_namespace": mapping.Spec.SourceNamespace,
		"dest_namespace":   mapping.Spec.DestinationNamespace,
	})
	
	log.Info("Processing namespace mapping for PVC replication")
	
	// Use the new controller for namespace mapping processing
	err := m.replicationController.ProcessNamespaceMapping(ctx, mapping)
	if err != nil {
		log.WithField("error", err).Error("Namespace mapping processing failed")
		return err
	}
	
	log.Info("Namespace mapping processing completed successfully")
	return nil
}

// GetSourceClient returns the source client
func (m *ReplicationManager) GetSourceClient() client.Client {
	return m.sourceClient
}

// GetDestClient returns the destination client
func (m *ReplicationManager) GetDestClient() client.Client {
	return m.destClient
}

// GetSourceK8sClient returns the source Kubernetes client
func (m *ReplicationManager) GetSourceK8sClient() kubernetes.Interface {
	return m.sourceK8sClient
}

// GetDestK8sClient returns the destination Kubernetes client
func (m *ReplicationManager) GetDestK8sClient() kubernetes.Interface {
	return m.destK8sClient
}

// GetSourceConfig returns the source config
func (m *ReplicationManager) GetSourceConfig() *rest.Config {
	return m.sourceConfig
}

// GetDestConfig returns the destination config 
func (m *ReplicationManager) GetDestConfig() *rest.Config {
	return m.destConfig
}
