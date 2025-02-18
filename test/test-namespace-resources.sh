#!/bin/bash
set -e

# Create test namespaces
kubectl create namespace source-ns --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace dest-ns --dry-run=client -o yaml | kubectl apply -f -

# Create test clusters
cat <<EOF | kubectl apply -f -
apiVersion: dr-syncer.io/v1alpha1
kind: Cluster
metadata:
  name: source-cluster
  namespace: default
spec:
  kubeconfigSecretRef:
    name: source-kubeconfig
    namespace: default
---
apiVersion: dr-syncer.io/v1alpha1
kind: Cluster
metadata:
  name: dest-cluster
  namespace: default
spec:
  kubeconfigSecretRef:
    name: dest-kubeconfig
    namespace: default
EOF

echo "Testing standard resources..."
# Apply standard resources test
kubectl apply -f standard-resources-test.yaml

echo "Waiting for standard resources to sync..."
sleep 5

# Verify standard resources
echo "Verifying standard resources in destination namespace..."
kubectl get configmap test-configmap -n dest-ns
kubectl get secret test-secret -n dest-ns
kubectl get deployment test-deployment -n dest-ns
kubectl get service test-service -n dest-ns
kubectl get ingress test-ingress -n dest-ns

echo "Testing custom resources..."
# Apply CRDs and custom resources test
kubectl apply -f custom-resources-test.yaml

echo "Waiting for custom resources to sync..."
sleep 5

# Verify custom resources
echo "Verifying custom resources in destination namespace..."
kubectl get widget test-widget-1 -n dest-ns
kubectl get widget test-widget-2 -n dest-ns
kubectl get database test-db -n dest-ns

# Verify resource contents
echo "Verifying resource contents..."

# Check ConfigMap data
echo "Checking ConfigMap data..."
SOURCE_CM_DATA=$(kubectl get configmap test-configmap -n source-ns -o jsonpath='{.data}')
DEST_CM_DATA=$(kubectl get configmap test-configmap -n dest-ns -o jsonpath='{.data}')
if [ "$SOURCE_CM_DATA" = "$DEST_CM_DATA" ]; then
    echo "ConfigMap data matches ✓"
else
    echo "ConfigMap data mismatch ✗"
fi

# Check Widget data
echo "Checking Widget data..."
SOURCE_WIDGET_DATA=$(kubectl get widget test-widget-1 -n source-ns -o jsonpath='{.spec}')
DEST_WIDGET_DATA=$(kubectl get widget test-widget-1 -n dest-ns -o jsonpath='{.spec}')
if [ "$SOURCE_WIDGET_DATA" = "$DEST_WIDGET_DATA" ]; then
    echo "Widget data matches ✓"
else
    echo "Widget data mismatch ✗"
fi

# Check Database data
echo "Checking Database data..."
SOURCE_DB_DATA=$(kubectl get database test-db -n source-ns -o jsonpath='{.spec}')
DEST_DB_DATA=$(kubectl get database test-db -n dest-ns -o jsonpath='{.spec}')
if [ "$SOURCE_DB_DATA" = "$DEST_DB_DATA" ]; then
    echo "Database data matches ✓"
else
    echo "Database data mismatch ✗"
fi

echo "Tests completed!"
