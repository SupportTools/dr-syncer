package replication

import (
	"k8s.io/client-go/rest"
)

// GetSourceConfig returns the source cluster configuration
func (p *PVCSyncer) GetSourceConfig() *rest.Config {
	return p.SourceConfig
}

// GetDestinationConfig returns the destination cluster configuration
func (p *PVCSyncer) GetDestinationConfig() *rest.Config {
	return p.DestinationConfig
}
