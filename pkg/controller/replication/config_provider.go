package replication

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ConfigProvider defines methods for accessing configs and clients
type ConfigProvider interface {
	GetSourceConfig() *rest.Config
	GetDestinationConfig() *rest.Config
	GetSourceClient() kubernetes.Interface
	GetDestinationClient() kubernetes.Interface
}

// GetSourceConfig returns the source cluster configuration
func (p *PVCSyncer) GetSourceConfig() *rest.Config {
	return p.SourceConfig
}

// GetDestinationConfig returns the destination cluster configuration
func (p *PVCSyncer) GetDestinationConfig() *rest.Config {
	return p.DestinationConfig
}

// GetSourceClient returns the source Kubernetes client
func (p *PVCSyncer) GetSourceClient() kubernetes.Interface {
	return p.SourceK8sClient
}

// GetDestinationClient returns the destination Kubernetes client
func (p *PVCSyncer) GetDestinationClient() kubernetes.Interface {
	return p.DestinationK8sClient
}
