#!/bin/bash
set -e

CONTROLLER_KUBECONFIG="/home/mmattox/.kube/mattox/a1-rancher-prd_fqdn"
PROD_KUBECONFIG="/home/mmattox/.kube/mattox/dr-syncer-nyc3-kubeconfig.yaml"
DR_KUBECONFIG="/home/mmattox/.kube/mattox/dr-syncer-sfo3-kubeconfig.yaml"

echo "Creating secret in controller cluster"
kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer create secret generic dr-syncer-nyc3-kubeconfig --from-file=kubeconfig=${PROD_KUBECONFIG} --dry-run=client -o yaml | kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -
kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer create secret generic dr-syncer-sfo3-kubeconfig --from-file=kubeconfig=${DR_KUBECONFIG} --dry-run=client -o yaml | kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -

echo "Creating RemoteClusters in controller cluster"
kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} -n dr-syncer create -f test/remote-clusters.yaml --dry-run=client -o yaml | kubectl --kubeconfig ${CONTROLLER_KUBECONFIG} apply -f -

# Create all required namespaces
echo "Creating test namespaces in Production and DR clusters"
NAMESPACES=(
    "dr-sync-test-case01"
    "dr-sync-test-case02"
    "dr-sync-test-case03"
    "dr-sync-test-case04"
    "dr-sync-test-case05"
    "dr-sync-test-case06"
    "dr-sync-test-case07"
    "source-namespace"
    "mapped-namespace"
)

for ns in "${NAMESPACES[@]}"; do
    kubectl --kubeconfig ${PROD_KUBECONFIG} create namespace $ns --dry-run=client -o yaml | kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f -
    kubectl --kubeconfig ${DR_KUBECONFIG} create namespace $ns --dry-run=client -o yaml | kubectl --kubeconfig ${DR_KUBECONFIG} apply -f -
done

echo "Creating test resources from test cases"
for testcase in test/cases/*.yaml; do
    echo "Applying test case: $testcase"
    kubectl --kubeconfig ${PROD_KUBECONFIG} apply -f $testcase
done

echo "Waiting for replication to complete..."
sleep 10

echo "=== Testing Resource Filtering (Case 05) ==="
echo "1. Checking ConfigMap (should exist)..."
if kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case05 get configmap should-sync-configmap &> /dev/null; then
    echo "✅ should-sync-configmap exists (correct)"
else
    echo "❌ should-sync-configmap does not exist (should be replicated)"
fi

echo "2. Checking Deployment (should not exist)..."
if ! kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case05 get deployment should-not-sync-deployment &> /dev/null; then
    echo "✅ should-not-sync-deployment does not exist (correct)"
else
    echo "❌ should-not-sync-deployment exists (should not be replicated)"
fi

echo -e "\n=== Testing Service Recreation (Case 06) ==="
echo "1. Checking Service metadata..."
LABELS=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case06 get service test-service -o jsonpath='{.metadata.labels}')
if [[ $LABELS == *"environment: production"* ]] && [[ $LABELS == *"team: platform"* ]]; then
    echo "✅ Service labels preserved correctly"
else
    echo "❌ Service labels not preserved"
fi

echo -e "\n=== Testing Ingress Handling (Case 07) ==="
echo "1. Checking Ingress..."
if kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case07 get ingress web-ingress &> /dev/null; then
    echo "✅ web-ingress exists"
    ANNOTATIONS=$(kubectl --kubeconfig ${DR_KUBECONFIG} -n dr-sync-test-case07 get ingress web-ingress -o jsonpath='{.metadata.annotations}')
    if [[ $ANNOTATIONS == *"nginx.ingress.kubernetes.io/rewrite-target"* ]]; then
        echo "✅ Ingress annotations preserved"
    else
        echo "❌ Ingress annotations not preserved"
    fi
else
    echo "❌ web-ingress does not exist"
fi

echo -e "\n=== Testing Namespace Mapping (Case 08) ==="
echo "1. Checking resources in mapped namespace..."
if kubectl --kubeconfig ${DR_KUBECONFIG} -n mapped-namespace get configmap test-config &> /dev/null; then
    echo "✅ ConfigMap replicated to mapped namespace"
else
    echo "❌ ConfigMap not found in mapped namespace"
fi

if kubectl --kubeconfig ${DR_KUBECONFIG} -n mapped-namespace get deployment test-app &> /dev/null; then
    echo "✅ Deployment replicated to mapped namespace"
else
    echo "❌ Deployment not found in mapped namespace"
fi

if kubectl --kubeconfig ${DR_KUBECONFIG} -n mapped-namespace get secret test-secret &> /dev/null; then
    echo "✅ Secret replicated to mapped namespace"
else
    echo "❌ Secret not found in mapped namespace"
fi

echo -e "\nTest suite completed."
