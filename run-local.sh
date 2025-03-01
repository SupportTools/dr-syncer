#!/bin/bash
set -e

# Ensure we have the controller kubeconfig
if [ -z "$1" ]; then
  echo "Usage: $0 <path-to-controller-kubeconfig>"
  echo "Example: $0 ./kubeconfig/controller.yaml"
  exit 1
fi

KUBECONFIG_PATH="$1"

# Check if kubeconfig exists
if [ ! -f "$KUBECONFIG_PATH" ]; then
  echo "Error: Kubeconfig file not found at $KUBECONFIG_PATH"
  exit 1
fi

# Check if we can access the cluster with the provided kubeconfig
if ! KUBECONFIG="$KUBECONFIG_PATH" kubectl get nodes &>/dev/null; then
  echo "Error: Unable to access the cluster with the provided kubeconfig"
  echo "Please check if the kubeconfig is valid and has the necessary permissions"
  exit 1
fi

# Get the controller deployment namespace
NAMESPACE=$(KUBECONFIG="$KUBECONFIG_PATH" kubectl get deployment -A --selector=app=dr-syncer-controller -o jsonpath='{.items[0].metadata.namespace}')

if [ -z "$NAMESPACE" ]; then
  echo "Warning: Could not find dr-syncer-controller deployment. Assuming namespace 'dr-syncer'"
  NAMESPACE="dr-syncer"
fi

DEPLOYMENT=$(KUBECONFIG="$KUBECONFIG_PATH" kubectl get deployment -n $NAMESPACE --selector=app=dr-syncer-controller -o jsonpath='{.items[0].metadata.name}')

if [ -z "$DEPLOYMENT" ]; then
  echo "Warning: Could not find dr-syncer-controller deployment in namespace $NAMESPACE."
  echo "Proceeding without scaling down any deployment."
else
  # Get current replica count before scaling down
  CURRENT_REPLICAS=$(KUBECONFIG="$KUBECONFIG_PATH" kubectl get deployment -n $NAMESPACE $DEPLOYMENT -o jsonpath='{.spec.replicas}')
  echo "Current replica count for $DEPLOYMENT in namespace $NAMESPACE: $CURRENT_REPLICAS"
  
  # Scale down the controller deployment
  echo "Scaling down deployment $DEPLOYMENT in namespace $NAMESPACE..."
  KUBECONFIG="$KUBECONFIG_PATH" kubectl scale deployment -n $NAMESPACE $DEPLOYMENT --replicas=0
  echo "Deployment scaled down successfully"
fi

# Function to restore the deployment when script exits
function cleanup {
  if [ ! -z "$DEPLOYMENT" ]; then
    echo "Restoring deployment $DEPLOYMENT in namespace $NAMESPACE to $CURRENT_REPLICAS replicas..."
    KUBECONFIG="$KUBECONFIG_PATH" kubectl scale deployment -n $NAMESPACE $DEPLOYMENT --replicas=$CURRENT_REPLICAS
    echo "Deployment restored successfully"
  fi
}

# Set up trap to restore deployment on script exit
trap cleanup EXIT

# Run the controller locally with the provided kubeconfig
echo "Starting local controller with kubeconfig $KUBECONFIG_PATH..."
echo "Press Ctrl+C to stop the controller and restore the deployment"
echo

export KUBECONFIG="$KUBECONFIG_PATH"
go run .
