---
sidebar_position: 1
---

# Getting Started with DR-Syncer CLI

This tutorial guides you through setting up and using the DR-Syncer CLI tool for your disaster recovery operations.

## Overview

The DR-Syncer CLI allows you to perform disaster recovery operations directly from the command line without deploying the controller. It's ideal for:

- Performing one-off DR operations
- Testing DR configurations
- Executing manual cutover or failback procedures
- Integrating DR operations into scripts or automation tools

## Prerequisites

Before starting, ensure you have:

- Access to both source and destination Kubernetes clusters
- Kubeconfig files for both clusters with sufficient permissions
- The applications you want to replicate deployed in the source cluster
- Sufficient privileges to create and modify resources in both clusters

## Installation

### Building from Source

The simplest way to install the DR-Syncer CLI is to build it from source:

```bash
# Clone the repository
git clone https://github.com/supporttools/dr-syncer.git
cd dr-syncer

# Build the binaries
make build
```

This will create both the `dr-syncer` controller and `dr-syncer-cli` binaries in the `bin/` directory.

### Verifying Your Installation

To verify that the CLI tool is working correctly:

```bash
bin/dr-syncer-cli --help
```

You should see output showing all available options and commands.

## Basic Configuration

The DR-Syncer CLI requires several key pieces of information to function:

1. **Source and destination kubeconfig files** - Connect to both clusters
2. **Source and destination namespaces** - Define what to replicate and where
3. **Operation mode** - Determine what action to take (Stage, Cutover, or Failback)

A minimal configuration looks like this:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage
```

## Testing Connectivity

Before performing any actual DR operations, it's a good practice to verify connectivity to both clusters:

```bash
# Test source cluster connectivity
KUBECONFIG=/path/to/source/kubeconfig kubectl get ns

# Test destination cluster connectivity
KUBECONFIG=/path/to/destination/kubeconfig kubectl get ns
```

Make sure you can access both clusters and have the necessary permissions.

## Understanding CLI Command Structure

The DR-Syncer CLI follows a straightforward command structure with flags controlling its behavior:

```
dr-syncer-cli [flags]
```

All configuration is passed through command-line flags. The three essential flag groups are:

### 1. Cluster Configuration Flags

These flags tell the CLI how to connect to your clusters:

```bash
--source-kubeconfig=/path/to/source/kubeconfig
--dest-kubeconfig=/path/to/destination/kubeconfig
```

### 2. Namespace Configuration Flags

These flags define which namespaces to work with:

```bash
--source-namespace=my-app
--dest-namespace=my-app-dr
```

### 3. Operation Mode Flag

This flag defines what operation to perform:

```bash
--mode=Stage  # Options: Stage, Cutover, Failback
```

## Running Your First DR Staging

Let's try a simple DR staging operation, which will synchronize resources to the destination cluster but keep deployments scaled to zero:

```bash
bin/dr-syncer-cli \
  --source-kubeconfig=/path/to/source/kubeconfig \
  --dest-kubeconfig=/path/to/destination/kubeconfig \
  --source-namespace=my-app \
  --dest-namespace=my-app-dr \
  --mode=Stage \
  --log-level=debug
```

This command will:
1. Connect to both clusters
2. Identify resources in the source namespace
3. Create or update corresponding resources in the destination namespace
4. Ensure that workloads (Deployments, StatefulSets) are scaled to zero in the destination
5. Report progress and results

## Inspecting the Results

After running the DR staging, verify the results:

```bash
# Check resources in the destination namespace
KUBECONFIG=/path/to/destination/kubeconfig kubectl get all -n my-app-dr
```

You should see all the resources from your source namespace now present in the destination namespace, with workloads scaled to zero.

## Common Issues and Troubleshooting

### Permission Problems

If you encounter permission errors, verify that your kubeconfig files have sufficient permissions:

```bash
# Check permissions in source cluster
KUBECONFIG=/path/to/source/kubeconfig kubectl auth can-i list deployments -n my-app

# Check permissions in destination cluster
KUBECONFIG=/path/to/destination/kubeconfig kubectl auth can-i create deployments -n my-app-dr
```

### Missing Destination Namespace

If the destination namespace doesn't exist, create it first:

```bash
KUBECONFIG=/path/to/destination/kubeconfig kubectl create ns my-app-dr
```

### Logging and Debugging

For more detailed output, use the debug log level:

```bash
--log-level=debug
```

This will show every action the CLI takes, helping you identify any issues.

## Next Steps

Now that you've successfully performed a basic DR staging operation, you're ready to explore more advanced features:

- [Setting Up Disaster Recovery Environment](setting-up-dr-environment.md) - Learn how to prepare a complete DR environment
- [Performing a DR Cutover](performing-dr-cutover.md) - Follow the steps for a full cutover to your DR environment
- [CLI Usage Reference](../cli-usage.md) - See the complete documentation for all CLI options

In the next tutorial, we'll cover how to set up a comprehensive disaster recovery environment using the DR-Syncer CLI.
