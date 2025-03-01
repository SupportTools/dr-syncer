# Active Context: DR Syncer Controller

## Current State

### Implementation Status
1. Core Controller
   - Manager implementation complete
   - Reconciler patterns established
   - Resource handling implemented
   - Health checks integrated

2. PVC Sync Agent Implementation
   - Status: In Progress
   - Branch: feature/pvc-sync-agent
   - Components:
     * Agent DaemonSet with SSH/rsync capability
     * Controller extension for agent management
     * SSH key management
     * Remote cluster deployment handling
     * Temporary PVC mount pods
   - Key Features:
     * Cross-cluster PVC data replication
     * Secure SSH-based rsync
     * Configurable concurrency and retry
     * Automated agent deployment
     * Enhanced security model without root filesystem access

3. Implementation Plan:
   - Phase 1: Basic Infrastructure
     * Agent Dockerfile and build process
     * SSH/rsync service setup
     * Controller CRD extensions
   - Phase 2: Deployment Logic
     * Remote cluster agent deployment
     * SSH key management
     * RBAC setup
   - Phase 3: Sync Implementation
     * PVC discovery and mapping
     * Rsync operations
     * Status tracking
   - Phase 4: Security Enhancement
     * Implement bastion/proxy pattern for agent
     * Create temporary pods with specific PVC mounts
     * Configure SSH forwarding between components
     * Implement dual-layer SSH key management

4. Improved Security Architecture:
   - Bastion/Proxy Pattern:
     * Agent pod acts as SSH proxy/bastion
     * No root filesystem access required
     * SSH forwarding to temporary pods
   - Temporary PVC Mount Pods:
     * Created on-demand for specific PVCs
     * Node affinity to run on same node as PVC
     * Direct PVC mount with minimal permissions
     * Runs rsync server for data access
   - Data Flow:
     * DR replication pod → Agent SSH (port 2222)
     * Agent → Temp Pod rsync (internal port)
     * Direct node-to-node path with minimal network overhead
   - SSH Key Management:
     * Two-layer authentication system
     * DR→Agent: Cluster-level keys stored in secrets
     * Agent→Temp: Operation-specific internal keys
     * Automated key generation and rotation

2. Custom Resources
   - RemoteCluster CRD implemented and validated
   - Replication CRD implemented and validated
   - Resource definitions streamlined and optimized

3. Synchronization
   - Resource type filtering working
   - Deployment handling implemented
   - Service/Ingress management functional
   - Scheduling system operational

### Active Development Areas

1. Test Suite Standardization
   - Current Focus: Standardizing all test cases to match test case 00 format
   - Status: Initial test cases completed, proceeding with remaining cases
   - Priority: High
   - Progress Tracking:
     * [x] Test case 00 (Reference implementation)
       - Enhanced with thorough resource validation
       - Added detailed metadata comparison
       - Improved error reporting
     * [x] Test case 01 (Wildcard namespace selection)
       - Implemented with same validation depth as case 00
       - Added thorough resource verification
       - Standardized test structure
     * [x] Test case 02 (Ignore label)
       - Added verification for ignored resources
       - Enhanced resource validation
       - Improved error reporting
     * [x] Test case 03 (Scale down)
       - Added specific scale down verification
       - Verifies source has 3 replicas
       - Verifies DR has 0 replicas
       - Ensures all other specs match
     * [x] Test case 04 (Scale override)
       - Added scale override label verification
       - Verifies non-overridden deployment scales to 0
       - Verifies overridden deployment maintains replicas
       - Enhanced deployment verification with replica checks
     * [x] Test case 05 (Resource filtering)
       - Added resource type filtering verification
       - Verifies ConfigMaps and Secrets are synced
       - Verifies other resources are filtered out
       - Enhanced status verification for filtered resources
     * [x] Test case 06 (Service recreation)
       - Added service type-specific verification
       - Verifies ClusterIP, NodePort, LoadBalancer services
       - Verifies headless service configuration
       - Enhanced service recreation validation
     * [x] Test case 07 (Ingress handling)
       - Added ingress type-specific verification
       - Verifies basic, complex, and annotated ingresses
       - Verifies TLS and backend configurations
       - Enhanced ingress validation with detailed checks
     * [x] Test case 08 (Namespace mapping)
       - Added direct and wildcard namespace mapping
       - Verifies namespace labels and annotations
       - Verifies resource references are updated
       - Enhanced namespace-specific validation
       - Validates namespace creation if it doesn't exist
       - Confirms proper mapping between Namespace-Prod and Namespace-DR
       - Verifies all resource types are properly synchronized with namespace updates
     * [] Test case 09 (PVC handling)
       - Added PVC type-specific verification
       - Verifies storage class mapping
       - Verifies access mode preservation
       - Enhanced PVC validation with detailed checks
     * [] Test case 10 (PVC basic sync)
       - Added basic PVC synchronization
       - Verifies exact attribute preservation
       - Verifies volume modes and access
       - Enhanced volume mount validation
     * [] Test case 11 (PVC storage class mapping)
       - Added storage class mapping verification
       - Verifies class translation
       - Verifies attribute preservation
       - Enhanced storage validation
     * [] Test case 12 (PVC access mode mapping)
       - Added access mode mapping verification
       - Verifies mode translation
       - Verifies mount configurations
       - Enhanced access validation
     * [] Test case 13 (PVC preserve attributes)
       - Added attribute preservation verification
       - Verifies all PVC attributes
       - Verifies complex configurations
       - Enhanced attribute validation
     * [] Test case 14 (PVC sync persistent volumes)
       - Added PV synchronization verification
       - Verifies multiple volume types
       - Verifies binding relationships
       - Enhanced volume validation
     * [] Test case 15 (PVC combined features)
       - Added combined feature verification
       - Verifies feature interactions
       - Verifies complex configurations
       - Enhanced validation coverage
     * [] Test case 16 (Replication modes)
       - Added replication mode verification
       - Verifies mode transitions
       - Verifies sync behaviors
       - Enhanced mode validation

   Key Improvements Made:
   - Added jq-based deep comparison of resources
   - Implemented thorough metadata validation
   - Added detailed error reporting with diffs
   - Enhanced status verification
   - Standardized test script structure
   - Added resource-specific validation functions

2. Test Implementation Requirements
   - Each test case must include:
     * README.md with consistent documentation
     * controller.yaml for Replication resource
     * remote.yaml for source resources
     * test.sh with standardized structure
   - Standard test script components:
     * Color coding and result tracking
     * Resource verification functions
     * Status checking
     * Detailed result reporting
     * Log command best practices:
       - Always use --tail to limit log output (e.g., kubectl logs pod-name --tail=100)
       - Never use -f/--follow flags in test scripts as they will never return
       - Use appropriate tail sizes based on verbosity needs (100 for normal, 1000 for debugging)

3. Test Categories to Standardize
   a. Scale Tests (03, 04)
      * Scale down functionality
      * Scale override via labels
      * Replica preservation
      * Status verification

   b. Resource Tests (05)
      * Resource type filtering
      * Resource exclusion
      * Filter validation
      * Status tracking

   c. Network Resource Tests (06, 07)
      * Service recreation
      * Service configuration
      * Ingress handling
      * Network validation

   d. Namespace Tests (08)
      * Namespace mapping
      * Namespace creation
      * Permission verification
      * Resource placement

   e. PVC Tests (09-15)
      * Basic PVC synchronization
      * Storage class mapping
      * Access mode handling
      * Attribute preservation
      * Volume synchronization
      * Combined features

   f. Mode Tests (16)
      * Replication mode verification
      * Schedule handling
      * Mode transitions
      * Status updates

## Recent Changes

1. Build System Improvements
   - Added DEBUG variable to Makefile to control log output verbosity
   - Updated kubeconfig settings to use files in the kubeconfig directory
   - Added conditional output based on DEBUG value for Docker, Helm, and kubectl commands
   - Added new deployment targets for different environments (deploy-dr, deploy-prod)
   - Updated test scripts to use the same kubeconfig files from the project directory
   - Enhanced test environment setup to copy kubeconfig files to the project directory

2. Logging System Improvements
   - Implemented package-level logger initialization via logger.go
   - Removed redundant internal logging packages
   - Standardized logging interface across all packages
   - Centralized logging configuration
   - Enhanced logging consistency and maintainability
   - Added controller-runtime logging integration
     * Created LogrusLogAdapter to bridge logrus with controller-runtime
     * Implemented logr.LogSink interface for proper integration
     * Added SetupControllerRuntimeLogging function to initialize controller-runtime logging
     * Fixed panic caused by uninitialized controller-runtime logger

2. Core Features
   - Simplified CRD architecture to two core CRDs
   - Enhanced Replication CRD with comprehensive status fields
   - Integrated namespace mapping into Replication CRD
   - Added IngressConfig to Replication CRD
     * Supports preserveAnnotations, preserveTLS, preserveBackends
     * Implemented following CRD update workflow:
       1. Added IngressConfig struct to types.go
       2. Added field to ReplicationSpec
       3. Generated CRDs with `make manifests`
       4. Applied updated CRD to cluster
     * Successfully validated with test case 07
   - Added phase tracking (Pending, Running, Completed, Failed)
   - Added sync statistics tracking
   - Added per-resource status tracking
   - Added detailed error reporting
   - Improved kubectl visibility with new columns
   - Enhanced status update conflict logging with resource version tracking
   - Added debug logging for resource version changes during reconciliation

2. Resource Management
   - Added deployment replica handling
   - Implemented service recreation
   - Enhanced resource filtering
   - Added exclusion capabilities

3. Documentation
   - Updated API documentation
   - Enhanced deployment guides
   - Added troubleshooting section
   - Improved examples
   - Documented CRD update process
   - Enhanced CRD documentation:
     * Added comprehensive file update guide
     * Documented required changes for spec vs status updates
     * Listed all affected files and their purposes
     * Included auto-generated file handling

4. CRD Management
   - Streamlined CRD management with automated sync
   - Established Go types as single source of truth
   - Enhanced Helm chart CRD integration
   - Automated CRD sync via `make manifests`
   - Reduced CRD complexity by maintaining only remoteclusters and replications

## Active Decisions

1. Architecture
   - Using controller-runtime framework
   - Implementing custom reconcilers
   - Leveraging Kubernetes native patterns
   - Following operator best practices

2. Implementation
   - Go as primary language
   - CRDs for configuration
   - Helm for deployment
   - Prometheus for metrics

3. Resource Handling
   - Zero replicas in DR clusters (with label override support)
   - Preserving deployment metadata
   - Managing service recreation
   - Handling ingress configuration
   - Scale override via 'dr-syncer.io/scale-override' label
   - Resource exclusion via 'dr-syncer.io/ignore' label

## Current Considerations

1. Technical
   - Performance optimization
   - Resource usage monitoring
   - Error handling improvements
   - Testing coverage

2. Operational
   - Deployment strategies
   - Monitoring setup
   - Backup procedures
   - Upgrade processes

3. Documentation
   - API reference updates
   - Example enhancements
   - Troubleshooting guides
   - Best practices

4. CRD Updates
   - Go types are now single source of truth
   - CRDs automatically sync to Helm chart
   - Helm templating automatically applied
   - Only two CRDs maintained: remoteclusters and replications

## Current Implementation Tasks

### Task 1: Agent SSH Proxy Implementation
- Status: Completed
- Files to Modify:
  * build/sshd_config
  * build/entrypoint.sh
  * cmd/agent/main.go
- Key Changes:
  * Enable SSH forwarding
  * Configure proxy settings
  * Add connection validation
- Success Criteria:
  * Agent can accept SSH connections
  * Port forwarding works
  * Connection logging in place

### Task 2: Temporary Pod Management
- Status: Completed
- Files to Create/Modify:
  * New pkg/agent/tempod package
  * Controller code updates
- Key Changes:
  * Pod template with PVC mount
  * Node affinity rules
  * Rsync server setup
  * Lifecycle management
- Success Criteria:
  * Pods created on correct nodes
  * PVCs mounted correctly
  * Cleanup works reliably

### Task 3: SSH Key Management System
- Status: Completed
- Files Created/Modified:
  * New pkg/agent/sshkeys package
  * New pkg/controller/replication/keys.go
  * New pkg/controller/replication/log.go
  * New pkg/controller/replication/pvc_sync.go
  * Controller code updates
- Key Changes:
  * Two-layer key generation
  * Key rotation logic
  * Secret management
  * Secure SSH key handling
  * Replication-level key management
  * Temporary pod key integration
- Success Criteria:
  * Keys generated securely
  * Rotation works smoothly
  * Secrets properly managed
  * Secure communication between pods
  * Proper key isolation between replications

### Task 4: Integration and Testing
- Status: Completed
- Files to Create/Modify:
  * test/cases/21_pvc_sync_security
  * Documentation updates
- Key Changes:
  * New test scenarios
  * Security validation
  * Performance metrics
- Success Criteria:
  * All tests pass
  * Documentation complete
  * Performance verified

## Next Steps

1. Short Term
   - Standardize test case implementations
   - Enhance test validation
   - Update test documentation
   - Implement consistent error handling
   - Complete PVC sync agent security enhancement tasks
   - Improve test environment setup automation

2. Medium Term
   - Add advanced test scenarios
   - Improve test coverage
   - Enhance test reporting
   - Add performance tests

3. Long Term
   - Scale testing improvements
   - Security test cases
   - Feature coverage expansion
   - Test automation enhancements
   - Label-based namespace replication:
     * Automatic replication based on namespace labels
     * Dynamic namespace discovery with label selectors
     * Destination namespace suffix pattern
     * Test case for label-based replication (20_label_based_replication)

## Recent Improvements

### Test Environment Setup Automation
- Status: Completed
- Files Created:
  * test/setup-test-clusters.sh: Script to automate test cluster setup
  * test/README.md: Documentation for test environment setup and usage
- Key Features:
  * Automated kubeconfig secret creation from local kubeconfig files
  * RemoteCluster and ClusterMapping resource application
  * Environment verification and validation
  * Idempotent execution (can be run multiple times)
- Benefits:
  * Simplified test environment setup
  * Consistent configuration across test runs
  * Reduced manual steps for developers
  * Improved documentation of test prerequisites
- Implementation Notes:
  * SSH key generation is handled by the controller itself
  * Each RemoteCluster must have a unique SSH key secret name
  * The controller creates and manages SSH key secrets for each RemoteCluster
  * The controller pushes SSH keys to remote clusters
  * The controller verifies SSH connectivity between clusters
