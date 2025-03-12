---
sidebar_position: 2
---

# Automating DR Processes

This advanced tutorial explains how to automate disaster recovery operations using DR-Syncer CLI within scripts, CI/CD pipelines, and scheduled jobs.

## Overview

While DR-Syncer CLI provides powerful manual control for disaster recovery operations, true operational efficiency comes from automation. By automating DR processes, you can:

- Ensure consistent execution
- Reduce human error
- Schedule regular synchronizations
- Integrate DR into your existing CI/CD pipelines
- Implement automated testing of your DR capabilities

This tutorial provides practical guidance on automating DR operations for production environments.

## Prerequisites

Before automating DR processes, ensure you have:

- Completed the [Getting Started with DR-Syncer CLI](../tutorial-basics/getting-started-cli.md) tutorial
- Successfully performed manual DR operations
- Basic understanding of shell scripting
- Familiarity with CI/CD concepts (for pipeline integration)
- Access to both source and destination clusters

## Creating Basic Automation Scripts

Let's start by creating scripts for common DR operations.

### Script Structure and Best Practices

DR automation scripts should follow these practices:

1. **Clear Parameter Handling**: Use command-line arguments or environment variables
2. **Error Handling**: Include proper error detection and reporting
3. **Logging**: Implement detailed logging for troubleshooting
4. **Validation**: Verify prerequisites and results
5. **Idempotence**: Scripts should be safe to run multiple times
6. **Exit Codes**: Return appropriate exit codes for integration with other tools

### Basic Stage Script

Create a shell script for staging DR resources:

```bash
#!/bin/bash
# dr-stage.sh - Automated DR staging script

# Script parameters
SOURCE_KUBECONFIG=${1:-"$HOME/.kube/source-config"}
DEST_KUBECONFIG=${2:-"$HOME/.kube/dest-config"}
SOURCE_NAMESPACE=${3:-"my-app"}
DEST_NAMESPACE=${4:-"my-app-dr"}
LOG_FILE="dr-stage-$(date +%Y%m%d-%H%M%S).log"

# Function for logging
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# Validate parameters
if [ ! -f "$SOURCE_KUBECONFIG" ]; then
  log "ERROR: Source kubeconfig not found: $SOURCE_KUBECONFIG"
  exit 1
fi

if [ ! -f "$DEST_KUBECONFIG" ]; then
  log "ERROR: Destination kubeconfig not found: $DEST_KUBECONFIG"
  exit 1
fi

# Verify source namespace exists
if ! KUBECONFIG="$SOURCE_KUBECONFIG" kubectl get namespace "$SOURCE_NAMESPACE" &> /dev/null; then
  log "ERROR: Source namespace does not exist: $SOURCE_NAMESPACE"
  exit 1
fi

# Create destination namespace if it doesn't exist
if ! KUBECONFIG="$DEST_KUBECONFIG" kubectl get namespace "$DEST_NAMESPACE" &> /dev/null; then
  log "Creating destination namespace: $DEST_NAMESPACE"
  KUBECONFIG="$DEST_KUBECONFIG" kubectl create namespace "$DEST_NAMESPACE"
fi

# Run DR-Syncer CLI
log "Starting DR staging operation..."
bin/dr-syncer-cli \
  --source-kubeconfig="$SOURCE_KUBECONFIG" \
  --dest-kubeconfig="$DEST_KUBECONFIG" \
  --source-namespace="$SOURCE_NAMESPACE" \
  --dest-namespace="$DEST_NAMESPACE" \
  --mode=Stage \
  --log-level=info

RESULT=$?
if [ $RESULT -eq 0 ]; then
  log "DR staging completed successfully."
else
  log "ERROR: DR staging failed with exit code $RESULT."
  exit $RESULT
fi

# Verify resources in destination
DEPLOYMENT_COUNT=$(KUBECONFIG="$DEST_KUBECONFIG" kubectl get deployments -n "$DEST_NAMESPACE" -o name | wc -l)
log "Found $DEPLOYMENT_COUNT deployments in destination namespace."

# All done!
log "DR staging process completed."
exit 0
```

Save this as `dr-stage.sh` and make it executable:

```bash
chmod +x dr-stage.sh
```

Usage:

```bash
./dr-stage.sh /path/to/source/kubeconfig /path/to/dest/kubeconfig my-app my-app-dr
```

### Cutover Script

Next, create a script for DR cutover:

```bash
#!/bin/bash
# dr-cutover.sh - Automated DR cutover script

# Script parameters
SOURCE_KUBECONFIG=${1:-"$HOME/.kube/source-config"}
DEST_KUBECONFIG=${2:-"$HOME/.kube/dest-config"}
SOURCE_NAMESPACE=${3:-"my-app"}
DEST_NAMESPACE=${4:-"my-app-dr"}
MIGRATE_PVC_DATA=${5:-"false"}
LOG_FILE="dr-cutover-$(date +%Y%m%d-%H%M%S).log"

# Function for logging
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# Validate parameters and connections
log "Validating parameters and connections..."
if [ ! -f "$SOURCE_KUBECONFIG" ] || [ ! -f "$DEST_KUBECONFIG" ]; then
  log "ERROR: Kubeconfig file(s) not found."
  exit 1
fi

# Check if both clusters are accessible
if ! KUBECONFIG="$SOURCE_KUBECONFIG" kubectl get ns &> /dev/null; then
  log "ERROR: Cannot connect to source cluster."
  exit 1
fi

if ! KUBECONFIG="$DEST_KUBECONFIG" kubectl get ns &> /dev/null; then
  log "ERROR: Cannot connect to destination cluster."
  exit 1
fi

# Log pre-cutover state for reference
log "Source deployments before cutover:"
KUBECONFIG="$SOURCE_KUBECONFIG" kubectl get deployments -n "$SOURCE_NAMESPACE" -o wide

log "Destination deployments before cutover:"
KUBECONFIG="$DEST_KUBECONFIG" kubectl get deployments -n "$DEST_NAMESPACE" -o wide

# Confirm cutover
log "!!!! INITIATING DR CUTOVER !!!!"
log "This will scale down applications in the source environment and activate them in DR."

# Build command with conditional PVC migration
CMD="bin/dr-syncer-cli \
  --source-kubeconfig=$SOURCE_KUBECONFIG \
  --dest-kubeconfig=$DEST_KUBECONFIG \
  --source-namespace=$SOURCE_NAMESPACE \
  --dest-namespace=$DEST_NAMESPACE \
  --mode=Cutover \
  --log-level=info"

if [ "$MIGRATE_PVC_DATA" = "true" ]; then
  CMD="$CMD --migrate-pvc-data=true"
  log "PVC data migration enabled."
else
  log "PVC data migration disabled."
fi

# Execute cutover
log "Executing cutover command..."
eval "$CMD"

RESULT=$?
if [ $RESULT -eq 0 ]; then
  log "DR cutover command completed successfully."
else
  log "ERROR: DR cutover command failed with exit code $RESULT."
  exit $RESULT
fi

# Verify cutover
log "Verifying source deployments scaled down..."
RUNNING_PODS=$(KUBECONFIG="$SOURCE_KUBECONFIG" kubectl get pods -n "$SOURCE_NAMESPACE" --field-selector=status.phase=Running 2>/dev/null | grep -v NAME | wc -l)
if [ "$RUNNING_PODS" -gt 0 ]; then
  log "WARNING: Source environment still has $RUNNING_PODS running pods."
else
  log "Source environment successfully scaled down."
fi

log "Verifying destination deployments scaled up..."
RUNNING_PODS_DR=$(KUBECONFIG="$DEST_KUBECONFIG" kubectl get pods -n "$DEST_NAMESPACE" --field-selector=status.phase=Running 2>/dev/null | grep -v NAME | wc -l)
if [ "$RUNNING_PODS_DR" -eq 0 ]; then
  log "WARNING: No running pods found in destination environment."
else
  log "Destination environment has $RUNNING_PODS_DR running pods."
fi

log "DR cutover process completed. Post-cutover verification is recommended."
exit 0
```

Save as `dr-cutover.sh` and make it executable:

```bash
chmod +x dr-cutover.sh
```

Usage:

```bash
./dr-cutover.sh /path/to/source/kubeconfig /path/to/dest/kubeconfig my-app my-app-dr true
```

## Scheduling Regular DR Sync Operations

For maintaining an up-to-date DR environment, schedule regular synchronization operations using cron or another scheduler.

### Cron Job Example

Create a script for a cron job:

```bash
#!/bin/bash
# dr-sync-cron.sh - Scheduled DR sync script

# Configuration - customize these variables
SOURCE_KUBECONFIG="/path/to/source/kubeconfig"
DEST_KUBECONFIG="/path/to/dest/kubeconfig"
SOURCE_NAMESPACE="my-app"
DEST_NAMESPACE="my-app-dr"
LOG_DIR="/var/log/dr-syncer"
MIGRATE_PVC=${MIGRATE_PVC:-"false"}  # Set to "true" to include PVC data migration

# Ensure log directory exists
mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/dr-sync-$(date +%Y%m%d-%H%M%S).log"

# Function for logging
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# Start logging
log "Starting scheduled DR sync operation"

# Build command
CMD="bin/dr-syncer-cli \
  --source-kubeconfig=$SOURCE_KUBECONFIG \
  --dest-kubeconfig=$DEST_KUBECONFIG \
  --source-namespace=$SOURCE_NAMESPACE \
  --dest-namespace=$DEST_NAMESPACE \
  --mode=Stage \
  --log-level=info"

if [ "$MIGRATE_PVC" = "true" ]; then
  CMD="$CMD --migrate-pvc-data=true"
  log "PVC data migration enabled."
else
  log "PVC data migration disabled."
fi

# Execute sync
log "Executing sync command..."
eval "$CMD"

RESULT=$?
if [ $RESULT -eq 0 ]; then
  log "DR sync completed successfully."
else
  log "ERROR: DR sync failed with exit code $RESULT."
  
  # Send alert if configured
  # (Insert notification code here)
  
  exit $RESULT
fi

# Log completion
log "Scheduled DR sync completed at $(date)"
exit 0
```

Save as `dr-sync-cron.sh`, make it executable, and add to crontab:

```bash
chmod +x dr-sync-cron.sh

# Add to crontab (daily at 2 AM)
(crontab -l 2>/dev/null; echo "0 2 * * * /path/to/dr-sync-cron.sh") | crontab -
```

### Configuring Cron for Different Schedules

Depending on your requirements, you might want different schedules:

1. **Daily Sync** (recommended for most environments):
   ```
   0 2 * * * /path/to/dr-sync-cron.sh
   ```

2. **Hourly Sync** (for environments with frequent changes):
   ```
   0 * * * * /path/to/dr-sync-cron.sh
   ```

3. **Weekly Sync** (for stable environments with infrequent changes):
   ```
   0 2 * * 0 /path/to/dr-sync-cron.sh
   ```

4. **Business Hours Only** (for dev/test environments):
   ```
   0 9-17 * * 1-5 /path/to/dr-sync-cron.sh
   ```

## Integrating with CI/CD Pipelines

Incorporating DR operations into your CI/CD pipeline ensures that your DR environment stays in sync with your production changes.

### GitHub Actions Example

Create a GitHub Actions workflow file (`.github/workflows/dr-sync.yml`):

```yaml
name: DR Synchronization

on:
  # Sync after production deployments
  workflow_run:
    workflows: ["Deploy to Production"]
    types:
      - completed
  # Allow manual trigger
  workflow_dispatch:
  # Regular schedule (daily at 2 AM UTC)
  schedule:
    - cron: '0 2 * * *'

jobs:
  dr-sync:
    runs-on: ubuntu-latest
    if: ${{ github.event.workflow_run.conclusion == 'success' || github.event_name != 'workflow_run' }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v3

      - name: Download DR-Syncer CLI
        run: |
          # Download the latest DR-Syncer CLI release
          mkdir -p bin
          curl -L -o bin/dr-syncer-cli https://github.com/supporttools/dr-syncer/releases/latest/download/dr-syncer-cli-linux-amd64
          chmod +x bin/dr-syncer-cli

      - name: Configure Kubeconfigs
        env:
          SOURCE_KUBECONFIG_DATA: ${{ secrets.SOURCE_KUBECONFIG_DATA }}
          DEST_KUBECONFIG_DATA: ${{ secrets.DEST_KUBECONFIG_DATA }}
        run: |
          mkdir -p ~/.kube
          echo "$SOURCE_KUBECONFIG_DATA" > ~/.kube/source-config
          echo "$DEST_KUBECONFIG_DATA" > ~/.kube/dest-config
          chmod 600 ~/.kube/source-config ~/.kube/dest-config

      - name: Perform DR Sync
        run: |
          bin/dr-syncer-cli \
            --source-kubeconfig=~/.kube/source-config \
            --dest-kubeconfig=~/.kube/dest-config \
            --source-namespace=${{ secrets.SOURCE_NAMESPACE }} \
            --dest-namespace=${{ secrets.DEST_NAMESPACE }} \
            --mode=Stage \
            --log-level=info

      - name: Report Status
        if: always()
        run: |
          if [ ${{ job.status }} == "success" ]; then
            echo "DR Sync completed successfully."
          else
            echo "DR Sync failed."
          fi
```

### Jenkins Pipeline Example

For Jenkins, create a Jenkinsfile:

```groovy
pipeline {
    agent any
    
    triggers {
        // Run daily at 2 AM
        cron('0 2 * * *')
        // Run after production deployment pipeline
        upstream(upstreamProjects: 'deploy-to-production', threshold: hudson.model.Result.SUCCESS)
    }
    
    environment {
        SOURCE_KUBECONFIG = credentials('source-kubeconfig')
        DEST_KUBECONFIG = credentials('dest-kubeconfig')
        SOURCE_NAMESPACE = 'my-app'
        DEST_NAMESPACE = 'my-app-dr'
    }
    
    stages {
        stage('Setup') {
            steps {
                sh 'mkdir -p bin'
                sh 'curl -L -o bin/dr-syncer-cli https://github.com/supporttools/dr-syncer/releases/latest/download/dr-syncer-cli-linux-amd64'
                sh 'chmod +x bin/dr-syncer-cli'
            }
        }
        
        stage('Verify Connectivity') {
            steps {
                sh 'KUBECONFIG=$SOURCE_KUBECONFIG kubectl get ns'
                sh 'KUBECONFIG=$DEST_KUBECONFIG kubectl get ns'
            }
        }
        
        stage('DR Sync') {
            steps {
                sh '''
                bin/dr-syncer-cli \
                  --source-kubeconfig=$SOURCE_KUBECONFIG \
                  --dest-kubeconfig=$DEST_KUBECONFIG \
                  --source-namespace=$SOURCE_NAMESPACE \
                  --dest-namespace=$DEST_NAMESPACE \
                  --mode=Stage \
                  --log-level=info
                '''
            }
        }
    }
    
    post {
        success {
            echo 'DR Sync completed successfully'
            // Add notification code here
        }
        failure {
            echo 'DR Sync failed'
            // Add alert code here
        }
    }
}
```

## Automated DR Testing

Regular testing of your DR capabilities is essential. Create a script to verify DR readiness:

```bash
#!/bin/bash
# dr-test.sh - Automated DR testing script

# Configuration - customize these variables
SOURCE_KUBECONFIG="/path/to/source/kubeconfig"
DEST_KUBECONFIG="/path/to/dest/kubeconfig"
SOURCE_NAMESPACE="my-app"
DEST_NAMESPACE="my-app-dr"
TEST_DEPLOYMENT="test-deployment"  # Deployment to test scaling
LOG_FILE="dr-test-$(date +%Y%m%d-%H%M%S).log"

# Function for logging
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# Start test
log "Starting DR test procedure"

# Step 1: Ensure latest sync
log "Step 1: Performing sync to ensure DR environment is current"
bin/dr-syncer-cli \
  --source-kubeconfig="$SOURCE_KUBECONFIG" \
  --dest-kubeconfig="$DEST_KUBECONFIG" \
  --source-namespace="$SOURCE_NAMESPACE" \
  --dest-namespace="$DEST_NAMESPACE" \
  --mode=Stage

if [ $? -ne 0 ]; then
  log "ERROR: Initial sync failed. Aborting test."
  exit 1
fi

# Step 2: Test scaling up a deployment in DR
log "Step 2: Testing deployment scaling in DR environment"

# Get current replica count
ORIGINAL_REPLICAS=$(KUBECONFIG="$DEST_KUBECONFIG" kubectl get deployment "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" -o jsonpath='{.spec.replicas}')
log "Original replicas: $ORIGINAL_REPLICAS"

# Scale up to 1 replica
log "Scaling deployment to 1 replica in DR environment"
KUBECONFIG="$DEST_KUBECONFIG" kubectl scale deployment "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" --replicas=1

# Wait for pods to be ready
log "Waiting for pods to be ready..."
KUBECONFIG="$DEST_KUBECONFIG" kubectl rollout status deployment/"$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" --timeout=2m

if [ $? -ne 0 ]; then
  log "ERROR: Deployment did not scale up successfully in DR environment."
  # Restore original state
  KUBECONFIG="$DEST_KUBECONFIG" kubectl scale deployment "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" --replicas="$ORIGINAL_REPLICAS"
  exit 1
fi

# Step 3: Test basic functionality
log "Step 3: Testing basic application functionality"

# This will depend on your application - customize as needed
# Example: Test a service endpoint
if KUBECONFIG="$DEST_KUBECONFIG" kubectl get svc "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" &> /dev/null; then
  # Get service port
  SVC_PORT=$(KUBECONFIG="$DEST_KUBECONFIG" kubectl get svc "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" -o jsonpath='{.spec.ports[0].port}')
  
  # Forward port
  KUBECONFIG="$DEST_KUBECONFIG" kubectl port-forward svc/"$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" 8080:"$SVC_PORT" &
  PF_PID=$!
  sleep 3
  
  # Test endpoint
  RESPONSE=$(curl -s http://localhost:8080/health)
  kill $PF_PID
  
  if [[ "$RESPONSE" == *"ok"* ]]; then
    log "Application health check passed"
  else
    log "ERROR: Application health check failed"
    # Restore original state
    KUBECONFIG="$DEST_KUBECONFIG" kubectl scale deployment "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" --replicas="$ORIGINAL_REPLICAS"
    exit 1
  fi
else
  log "Service $TEST_DEPLOYMENT not found, skipping endpoint test"
fi

# Step 4: Restore original state
log "Step 4: Restoring original state"
KUBECONFIG="$DEST_KUBECONFIG" kubectl scale deployment "$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" --replicas="$ORIGINAL_REPLICAS"

# Wait for restoration to complete
KUBECONFIG="$DEST_KUBECONFIG" kubectl rollout status deployment/"$TEST_DEPLOYMENT" -n "$DEST_NAMESPACE" --timeout=2m

# Test completed
log "DR test completed successfully"
exit 0
```

Schedule this test to run regularly to ensure your DR environment remains functional.

## Creating a Comprehensive DR Automation Package

For a production environment, combine these scripts into a DR automation package:

### Directory Structure

```
dr-automation/
├── bin/                     # DR-Syncer CLI binary
├── scripts/
│   ├── dr-stage.sh          # Basic staging script
│   ├── dr-cutover.sh        # Cutover script
│   ├── dr-failback.sh       # Failback script
│   ├── dr-sync-cron.sh      # Scheduled sync script
│   └── dr-test.sh           # DR testing script
├── config/
│   ├── environments/        # Environment-specific configurations
│   │   ├── prod-to-dr.conf
│   │   └── dr-to-prod.conf
│   └── kubeconfig/          # Kubeconfig files
│       ├── production.conf
│       └── dr.conf
├── logs/                    # Log directory
└── README.md                # Documentation
```

### Configuration Files

Create environment-specific configuration files:

```bash
# prod-to-dr.conf
SOURCE_KUBECONFIG="../config/kubeconfig/production.conf"
DEST_KUBECONFIG="../config/kubeconfig/dr.conf"
SOURCE_NAMESPACE="my-app"
DEST_NAMESPACE="my-app-dr"
MIGRATE_PVC="false"
```

### Wrapper Script

Create a main wrapper script:

```bash
#!/bin/bash
# dr-manager.sh - Main DR automation script

# Load defaults
CONFIG_DIR="./config/environments"
DEFAULT_CONFIG="prod-to-dr.conf"
SCRIPTS_DIR="./scripts"

# Parse arguments
ACTION="$1"
CONFIG_FILE="${2:-$DEFAULT_CONFIG}"

# Load configuration
if [ -f "$CONFIG_DIR/$CONFIG_FILE" ]; then
  source "$CONFIG_DIR/$CONFIG_FILE"
else
  echo "ERROR: Configuration file not found: $CONFIG_DIR/$CONFIG_FILE"
  exit 1
fi

# Execute requested action
case "$ACTION" in
  stage)
    $SCRIPTS_DIR/dr-stage.sh "$SOURCE_KUBECONFIG" "$DEST_KUBECONFIG" "$SOURCE_NAMESPACE" "$DEST_NAMESPACE"
    ;;
  cutover)
    $SCRIPTS_DIR/dr-cutover.sh "$SOURCE_KUBECONFIG" "$DEST_KUBECONFIG" "$SOURCE_NAMESPACE" "$DEST_NAMESPACE" "$MIGRATE_PVC"
    ;;
  failback)
    $SCRIPTS_DIR/dr-failback.sh "$SOURCE_KUBECONFIG" "$DEST_KUBECONFIG" "$SOURCE_NAMESPACE" "$DEST_NAMESPACE"
    ;;
  test)
    $SCRIPTS_DIR/dr-test.sh "$SOURCE_KUBECONFIG" "$DEST_KUBECONFIG" "$SOURCE_NAMESPACE" "$DEST_NAMESPACE"
    ;;
  sync)
    $SCRIPTS_DIR/dr-sync-cron.sh
    ;;
  *)
    echo "Usage: $0 {stage|cutover|failback|test|sync} [config_file]"
    echo "Available configurations:"
    ls -1 "$CONFIG_DIR"
    exit 1
    ;;
esac
```

Usage:

```bash
./dr-manager.sh stage
./dr-manager.sh cutover
./dr-manager.sh failback
./dr-manager.sh test
./dr-manager.sh sync
```

## Advanced Monitoring and Alerting

Enhance your DR automation with monitoring and alerting:

### Basic Slack Notification

Add notification functions to your scripts:

```bash
# Function to send Slack notification
send_slack_notification() {
  local message="$1"
  local webhook_url="$SLACK_WEBHOOK_URL"
  
  if [ -z "$webhook_url" ]; then
    log "Slack webhook URL not configured, skipping notification"
    return
  fi
  
  curl -s -X POST -H 'Content-type: application/json' \
    --data "{\"text\":\"$message\"}" \
    "$webhook_url"
}

# Use in your script
if [ $RESULT -ne 0 ]; then
  log "DR sync failed with exit code $RESULT"
  send_slack_notification "❌ DR sync failed: $ERROR_MESSAGE"
else
  log "DR sync completed successfully"
  send_slack_notification "✅ DR sync completed successfully"
fi
```

### Prometheus Metrics

For more advanced monitoring, create a simple metrics endpoint:

```bash
# Write Prometheus metrics file
write_metrics() {
  local metrics_file="/var/lib/node_exporter/dr_syncer_metrics.prom"
  local status="$1"  # 1 for success, 0 for failure
  local duration="$2"
  
  mkdir -p "$(dirname "$metrics_file")"
  
  # Write metrics
  cat > "$metrics_file" << EOF
# HELP dr_syncer_last_run_status Last DR sync status (1=success, 0=failure)
# TYPE dr_syncer_last_run_status gauge
dr_syncer_last_run_status $status
# HELP dr_syncer_last_run_timestamp_seconds Last DR sync timestamp
# TYPE dr_syncer_last_run_timestamp_seconds gauge
dr_syncer_last_run_timestamp_seconds $(date +%s)
# HELP dr_syncer_last_run_duration_seconds Last DR sync duration in seconds
# TYPE dr_syncer_last_run_duration_seconds gauge
dr_syncer_last_run_duration_seconds $duration
EOF
}
```

## Best Practices for DR Automation

1. **Test Thoroughly**: Regularly test your automation scripts in a controlled environment
2. **Version Control**: Store all scripts and configurations in a version control system
3. **Secure Credentials**: Never hardcode credentials in scripts; use environment variables or secret management tools
4. **Validation**: Include validation steps before and after each operation
5. **Logging**: Implement comprehensive logging for all operations
6. **Alerting**: Set up notifications for failed DR operations
7. **Documentation**: Document the purpose and usage of each script
8. **Emergency Procedures**: Create documented manual procedures for emergencies
9. **Regular Reviews**: Periodically review and update your automation scripts
10. **Role-Based Access**: Limit script execution to authorized personnel

## Next Steps

Now that you've implemented comprehensive DR automation, consider:

- Integrating with your incident response system
- Creating a DR runbook that combines automated and manual procedures
- Implementing multi-region or multi-cloud DR strategies
- Performing regular DR simulations to verify your automation

By following this guide, you've developed a robust automation framework for DR operations, ensuring that your disaster recovery capabilities are always ready when needed.
