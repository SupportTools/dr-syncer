---
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '30px'}}>
  <img src="https://cdn.support.tools/dr-syncer/logo_no_background.png" alt="DR-Syncer Logo" width="200"/>
</div>

# Introduction to DR-Syncer

Welcome to the DR-Syncer documentation. This guide provides comprehensive information about installation, configuration, usage, and troubleshooting of DR-Syncer.

## The Disaster Recovery Challenge in Kubernetes

Organizations running Kubernetes in production environments face significant challenges when establishing and maintaining disaster recovery (DR) capabilities:

### Manual Configuration Burden

- **Time-intensive setup**: Creating and configuring DR environments requires extensive manual effort
- **Error-prone processes**: Manual resource copying leads to inconsistencies and mistakes
- **Configuration drift**: Source and DR environments quickly become misaligned without constant maintenance
- **Complex resource dependencies**: Ensuring all dependent resources are properly synchronized

### Resource Management Complexity

- **Resource tracking**: Difficulty identifying which resources need replication and when
- **Version management**: Maintaining consistent resource versions across clusters
- **Selective synchronization**: Determining which resources should be replicated and which should be excluded
- **Resource transformation**: Adapting resources for DR environments (e.g., scaling down deployments)

### Operational Overhead

- **Manual updates**: Regular intervention required to keep DR environments current
- **Limited automation**: Traditional DR processes lack comprehensive automation
- **Maintenance burden**: Time-intensive DR testing and verification
- **Specialized knowledge**: Requires deep understanding of Kubernetes resources and their relationships

## What is DR-Syncer?

DR-Syncer is a Kubernetes controller designed to automate and simplify disaster recovery synchronization between Kubernetes clusters. It provides automated, scheduled synchronization of resources from source namespaces to destination namespaces in remote clusters.

By following Kubernetes controller patterns and leveraging Custom Resource Definitions (CRDs), DR-Syncer creates a declarative, Kubernetes-native approach to disaster recovery that integrates seamlessly with existing workflows.

## Key Concepts

### Custom Resources

DR-Syncer defines two primary custom resources:

1. **RemoteCluster**: Defines connection details and authentication for a remote cluster
   - Stores kubeconfig reference
   - Configures SSH access for PVC replication
   - Manages agent deployment

2. **Replication**: Defines synchronization configuration between namespaces
   - Specifies source and destination namespaces
   - Configures resource filtering rules
   - Sets synchronization schedule and mode
   - Tracks synchronization status

### Controller Components

- **Manager**: Handles controller lifecycle and shared dependencies
- **Reconcilers**: Implement controller business logic for each custom resource
- **Clients**: Interact with Kubernetes API in source and remote clusters
- **Resource Handlers**: Process different resource types appropriately

### PVC Sync Architecture

For persistent data replication, DR-Syncer deploys:

- **Agent DaemonSet**: Runs on remote clusters with SSH/rsync capability
- **SSH Security Model**: Secures communication with proper key management
- **Direct Access Pattern**: Agent accesses PVCs directly with minimal permissions

## How DR-Syncer Works

DR-Syncer operates as a Kubernetes controller, following a reconciliation pattern:

1. **Resource Watching**:
   - The controller watches for changes to custom resources
   - It monitors source namespaces for resource changes based on configuration

2. **Reconciliation Process**:
   - When changes are detected, reconciliation is triggered
   - The controller compares desired state with actual state in remote clusters
   - Necessary synchronization operations are identified

3. **Resource Processing**:
   - Resources are filtered based on type and exclusion rules
   - Resources are transformed as needed (e.g., scaling deployments to zero)
   - Network resources are adapted for the target environment
   - PVCs are synchronized with data replication when configured

4. **Synchronization Execution**:
   - Resources are applied to remote clusters
   - Status is updated with synchronization results
   - Metrics are published for monitoring

5. **Synchronization Modes**:
   - **Continuous Sync**: Constantly watches for changes and synchronizes them immediately
   - **Scheduled Sync**: Performs synchronization on a defined schedule (cron-based)
   - **Manual Sync**: Allows for on-demand synchronization when needed

6. **Error Handling**:
   - Failed operations are retried with exponential backoff
   - Detailed status reporting for debugging
   - Comprehensive logging of synchronization activities

## Key Benefits

### Operational Efficiency

- **Automated synchronization**: Eliminates manual resource copying and configuration
- **Reduced maintenance**: Minimizes ongoing operational overhead
- **Consistent environments**: Ensures DR clusters accurately mirror production

### Flexibility and Control

- **Granular resource selection**: Choose exactly which resources to synchronize
- **Customizable scheduling**: Control when synchronization occurs
- **Resource transformation**: Automatically adapt resources for DR environments
- **Multiple sync modes**: Choose the right synchronization strategy for your needs

### Enterprise-Ready Features

- **Multi-cluster support**: Manage multiple DR clusters from a single controller
- **Secure PVC replication**: Safely replicate persistent data with minimal privileges
- **Comprehensive monitoring**: Track synchronization status and health
- **Leader election**: Supports high availability deployments

## Getting Started

To get started with DR-Syncer, navigate to the [Installation & Configuration](/docs/installation) section to learn how to deploy the controller to your cluster.

For a complete overview of DR-Syncer's features, visit the [Features](/docs/features) section.

If you're looking for practical configuration examples, check out the [Examples](/docs/examples) section, which provides a variety of use cases and configuration templates.
