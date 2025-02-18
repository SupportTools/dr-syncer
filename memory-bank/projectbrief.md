# Project Brief: DR Syncer Controller

## Core Mission
DR Syncer is a Kubernetes controller designed to automate and simplify disaster recovery synchronization between Kubernetes clusters. It provides automated, scheduled synchronization of resources from source namespaces to destination namespaces in remote clusters.

## Key Requirements

1. Resource Synchronization
   - Synchronize multiple resource types (ConfigMaps, Secrets, Deployments, Services, Ingresses)
   - Maintain resource state and metadata
   - Handle deployment replicas appropriately for DR scenarios

2. Cluster Management
   - Support multiple remote clusters
   - Secure cluster authentication via kubeconfig
   - Health monitoring and readiness checks

3. Configuration Flexibility
   - Namespace-level configuration
   - Customizable sync schedules
   - Resource type filtering
   - Resource exclusion capabilities

4. Operational Requirements
   - Metrics exposure for monitoring
   - Leader election for high availability
   - Health and readiness probes
   - Logging and error handling

## Project Scope

### In Scope
- Kubernetes resource synchronization
- Cron-based scheduling
- Multi-cluster support
- Resource filtering and exclusion
- Deployment with zero replicas in DR
- Service and Ingress handling
- Metrics and monitoring
- High availability support

### Out of Scope
- Data replication (databases, storage)
- Network configuration beyond Services/Ingresses
- Application-level DR testing
- Automatic failover
- Cross-cloud provider management

## Success Criteria
1. Successful resource synchronization between clusters
2. Minimal operational overhead for DR setup
3. Reliable scheduling and execution
4. Clear monitoring and status reporting
5. Production-grade reliability and error handling
