package syncer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBuiltInResource_ConfigMaps(t *testing.T) {
	assert.True(t, isBuiltInResource("configmaps"), "configmaps should be built-in")
	assert.True(t, isBuiltInResource("configmap"), "configmap should be built-in")
	assert.True(t, isBuiltInResource("ConfigMaps"), "ConfigMaps (case-insensitive) should be built-in")
	assert.True(t, isBuiltInResource("CONFIGMAPS"), "CONFIGMAPS (uppercase) should be built-in")
}

func TestIsBuiltInResource_Secrets(t *testing.T) {
	assert.True(t, isBuiltInResource("secrets"), "secrets should be built-in")
	assert.True(t, isBuiltInResource("secret"), "secret should be built-in")
	assert.True(t, isBuiltInResource("Secrets"), "Secrets (case-insensitive) should be built-in")
	assert.True(t, isBuiltInResource("SECRET"), "SECRET (uppercase) should be built-in")
}

func TestIsBuiltInResource_Deployments(t *testing.T) {
	assert.True(t, isBuiltInResource("deployments"), "deployments should be built-in")
	assert.True(t, isBuiltInResource("deployment"), "deployment should be built-in")
	assert.True(t, isBuiltInResource("Deployments"), "Deployments (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_Services(t *testing.T) {
	assert.True(t, isBuiltInResource("services"), "services should be built-in")
	assert.True(t, isBuiltInResource("service"), "service should be built-in")
	assert.True(t, isBuiltInResource("Services"), "Services (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_Ingresses(t *testing.T) {
	assert.True(t, isBuiltInResource("ingresses"), "ingresses should be built-in")
	assert.True(t, isBuiltInResource("ingress"), "ingress should be built-in")
	assert.True(t, isBuiltInResource("Ingresses"), "Ingresses (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_Pods(t *testing.T) {
	assert.True(t, isBuiltInResource("pods"), "pods should be built-in")
	assert.True(t, isBuiltInResource("pod"), "pod should be built-in")
	assert.True(t, isBuiltInResource("Pods"), "Pods (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_Events(t *testing.T) {
	assert.True(t, isBuiltInResource("events"), "events should be built-in")
	assert.True(t, isBuiltInResource("event"), "event should be built-in")
	assert.True(t, isBuiltInResource("Events"), "Events (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_Endpoints(t *testing.T) {
	assert.True(t, isBuiltInResource("endpoints"), "endpoints should be built-in")
	assert.True(t, isBuiltInResource("endpoint"), "endpoint should be built-in")
	assert.True(t, isBuiltInResource("Endpoints"), "Endpoints (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_PersistentVolumeClaims(t *testing.T) {
	assert.True(t, isBuiltInResource("persistentvolumeclaims"), "persistentvolumeclaims should be built-in")
	assert.True(t, isBuiltInResource("persistentvolumeclaim"), "persistentvolumeclaim should be built-in")
	assert.True(t, isBuiltInResource("pvc"), "pvc should be built-in")
	assert.True(t, isBuiltInResource("PVC"), "PVC (case-insensitive) should be built-in")
	assert.True(t, isBuiltInResource("PersistentVolumeClaims"), "PersistentVolumeClaims should be built-in")
}

func TestIsBuiltInResource_PersistentVolumes(t *testing.T) {
	assert.True(t, isBuiltInResource("persistentvolumes"), "persistentvolumes should be built-in")
	assert.True(t, isBuiltInResource("persistentvolume"), "persistentvolume should be built-in")
	assert.True(t, isBuiltInResource("pv"), "pv should be built-in")
	assert.True(t, isBuiltInResource("PV"), "PV (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_CustomResourceDefinitions(t *testing.T) {
	assert.True(t, isBuiltInResource("customresourcedefinitions"), "customresourcedefinitions should be built-in")
	assert.True(t, isBuiltInResource("customresourcedefinition"), "customresourcedefinition should be built-in")
	assert.True(t, isBuiltInResource("crd"), "crd should be built-in")
	assert.True(t, isBuiltInResource("crds"), "crds should be built-in")
	assert.True(t, isBuiltInResource("CRD"), "CRD (case-insensitive) should be built-in")
	assert.True(t, isBuiltInResource("CRDs"), "CRDs (case-insensitive) should be built-in")
}

func TestIsBuiltInResource_NotBuiltIn(t *testing.T) {
	// Custom resources should not be built-in
	assert.False(t, isBuiltInResource("certificates"), "certificates should not be built-in")
	assert.False(t, isBuiltInResource("issuers"), "issuers should not be built-in")
	assert.False(t, isBuiltInResource("virtualservices"), "virtualservices should not be built-in")
	assert.False(t, isBuiltInResource("gateways"), "gateways should not be built-in")
	assert.False(t, isBuiltInResource("destinationrules"), "destinationrules should not be built-in")
	assert.False(t, isBuiltInResource("prometheusrules"), "prometheusrules should not be built-in")
	assert.False(t, isBuiltInResource("servicemonitors"), "servicemonitors should not be built-in")
}

func TestIsBuiltInResource_EmptyString(t *testing.T) {
	assert.False(t, isBuiltInResource(""), "empty string should not be built-in")
}

func TestIsBuiltInResource_PartialMatch(t *testing.T) {
	// Partial matches should not be recognized
	assert.False(t, isBuiltInResource("config"), "config should not be built-in")
	assert.False(t, isBuiltInResource("deploy"), "deploy should not be built-in")
	assert.False(t, isBuiltInResource("svc"), "svc should not be built-in (use services)")
	assert.False(t, isBuiltInResource("ing"), "ing should not be built-in (use ingresses)")
}

func TestIsBuiltInResource_MixedCase(t *testing.T) {
	assert.True(t, isBuiltInResource("ConfigMaps"), "ConfigMaps should be built-in")
	assert.True(t, isBuiltInResource("SECRETS"), "SECRETS should be built-in")
	assert.True(t, isBuiltInResource("DeployMents"), "DeployMents should be built-in")
	assert.True(t, isBuiltInResource("sErViCeS"), "sErViCeS should be built-in")
}

func TestIsBuiltInResource_AllBuiltInResources(t *testing.T) {
	// Comprehensive test of all built-in resources
	builtInResources := []string{
		// ConfigMaps
		"configmaps", "configmap",
		// Secrets
		"secrets", "secret",
		// Deployments
		"deployments", "deployment",
		// Services
		"services", "service",
		// Ingresses
		"ingresses", "ingress",
		// Pods
		"pods", "pod",
		// Events
		"events", "event",
		// Endpoints
		"endpoints", "endpoint",
		// PVCs
		"persistentvolumeclaims", "persistentvolumeclaim", "pvc",
		// PVs
		"persistentvolumes", "persistentvolume", "pv",
		// CRDs
		"customresourcedefinitions", "customresourcedefinition", "crd", "crds",
	}

	for _, resource := range builtInResources {
		assert.True(t, isBuiltInResource(resource), "%s should be built-in", resource)
	}
}

func TestIsBuiltInResource_RealisticCRDNames(t *testing.T) {
	// Test with realistic CRD names that should NOT be built-in
	crdNames := []string{
		"clusterissuers",
		"certificates",
		"certificaterequests",
		"virtualservices",
		"destinationrules",
		"gateways",
		"envoyfilters",
		"serviceentries",
		"workloadentries",
		"sidecars",
		"prometheusrules",
		"servicemonitors",
		"podmonitors",
		"thanosrulers",
		"alertmanagers",
		"alertmanagerconfigs",
		"remoteclusters",
		"namespacemappings",
		"clustermappings",
	}

	for _, crd := range crdNames {
		assert.False(t, isBuiltInResource(crd), "%s should NOT be built-in", crd)
	}
}
