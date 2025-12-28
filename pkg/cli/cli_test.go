package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Test constants
func TestConstants(t *testing.T) {
	assert.Equal(t, "dr-syncer.io/original-replicas", OriginalReplicasAnnotation)
}

// Test DefaultResourceTypes
func TestDefaultResourceTypes(t *testing.T) {
	assert.NotEmpty(t, DefaultResourceTypes)
	assert.Contains(t, DefaultResourceTypes, "configmaps")
	assert.Contains(t, DefaultResourceTypes, "secrets")
	assert.Contains(t, DefaultResourceTypes, "deployments")
	assert.Contains(t, DefaultResourceTypes, "statefulsets")
	assert.Contains(t, DefaultResourceTypes, "daemonsets")
	assert.Contains(t, DefaultResourceTypes, "services")
	assert.Contains(t, DefaultResourceTypes, "ingresses")
	assert.Contains(t, DefaultResourceTypes, "serviceaccounts")
	assert.Contains(t, DefaultResourceTypes, "persistentvolumeclaims")
}

// Test Config struct
func TestConfig_Struct(t *testing.T) {
	config := &Config{
		SourceKubeconfig:       "/path/to/source",
		DestKubeconfig:         "/path/to/dest",
		SourceNamespace:        "source-ns",
		DestNamespace:          "dest-ns",
		Mode:                   "Stage",
		IncludeCustomResources: true,
		MigratePVCData:         true,
		ResourceTypes:          []string{"deployments", "services"},
		ExcludeResourceTypes:   []string{"secrets"},
		PVMigrateFlags:         "--some-flag",
	}

	assert.Equal(t, "/path/to/source", config.SourceKubeconfig)
	assert.Equal(t, "/path/to/dest", config.DestKubeconfig)
	assert.Equal(t, "source-ns", config.SourceNamespace)
	assert.Equal(t, "dest-ns", config.DestNamespace)
	assert.Equal(t, "Stage", config.Mode)
	assert.True(t, config.IncludeCustomResources)
	assert.True(t, config.MigratePVCData)
	assert.Len(t, config.ResourceTypes, 2)
	assert.Len(t, config.ExcludeResourceTypes, 1)
}

func TestConfig_Empty(t *testing.T) {
	config := &Config{}

	assert.Empty(t, config.SourceKubeconfig)
	assert.Empty(t, config.DestKubeconfig)
	assert.Empty(t, config.Mode)
	assert.False(t, config.IncludeCustomResources)
	assert.False(t, config.MigratePVCData)
	assert.Nil(t, config.ResourceTypes)
	assert.Nil(t, config.ExcludeResourceTypes)
}

// Test ShouldSyncResourceType method
func TestShouldSyncResourceType_DefaultResourceType(t *testing.T) {
	config := &Config{}

	assert.True(t, config.ShouldSyncResourceType("configmaps", false))
	assert.True(t, config.ShouldSyncResourceType("secrets", false))
	assert.True(t, config.ShouldSyncResourceType("deployments", false))
}

func TestShouldSyncResourceType_NotInDefaultList(t *testing.T) {
	config := &Config{}

	// Resource types not in default list should not sync
	assert.False(t, config.ShouldSyncResourceType("pods", false))
	assert.False(t, config.ShouldSyncResourceType("replicasets", false))
}

func TestShouldSyncResourceType_CustomResourceExcluded(t *testing.T) {
	config := &Config{
		IncludeCustomResources: false,
	}

	// Custom resources should be excluded when flag is false
	assert.False(t, config.ShouldSyncResourceType("mycrd", true))
}

func TestShouldSyncResourceType_CustomResourceIncluded(t *testing.T) {
	config := &Config{
		IncludeCustomResources: true,
	}

	// Custom resources should be included when flag is true
	assert.True(t, config.ShouldSyncResourceType("mycrd", true))
}

func TestShouldSyncResourceType_ExplicitResourceTypes(t *testing.T) {
	config := &Config{
		ResourceTypes: []string{"deployments", "services"},
	}

	// Only explicitly listed types should sync
	assert.True(t, config.ShouldSyncResourceType("deployments", false))
	assert.True(t, config.ShouldSyncResourceType("services", false))
	assert.False(t, config.ShouldSyncResourceType("configmaps", false))
}

func TestShouldSyncResourceType_ExcludeList(t *testing.T) {
	config := &Config{
		ExcludeResourceTypes: []string{"secrets"},
	}

	// Excluded types should not sync
	assert.False(t, config.ShouldSyncResourceType("secrets", false))
	// Non-excluded types in default list should sync
	assert.True(t, config.ShouldSyncResourceType("configmaps", false))
}

func TestShouldSyncResourceType_ExcludeTakesPrecedence(t *testing.T) {
	config := &Config{
		ResourceTypes:        []string{"secrets", "configmaps"},
		ExcludeResourceTypes: []string{"secrets"},
	}

	// Exclude should take precedence over include
	assert.False(t, config.ShouldSyncResourceType("secrets", false))
	assert.True(t, config.ShouldSyncResourceType("configmaps", false))
}

func TestShouldSyncResourceType_CustomWithExplicitTypes(t *testing.T) {
	config := &Config{
		IncludeCustomResources: true,
		ResourceTypes:          []string{"mycrd"},
	}

	// Only explicitly listed custom resource should sync
	assert.True(t, config.ShouldSyncResourceType("mycrd", true))
	assert.False(t, config.ShouldSyncResourceType("othercrd", true))
}

// Test parseCommandLineArgs function
func TestParseCommandLineArgs_Simple(t *testing.T) {
	args, err := parseCommandLineArgs("--flag1 value1 --flag2 value2")

	assert.NoError(t, err)
	assert.Len(t, args, 4)
	assert.Equal(t, "--flag1", args[0])
	assert.Equal(t, "value1", args[1])
	assert.Equal(t, "--flag2", args[2])
	assert.Equal(t, "value2", args[3])
}

func TestParseCommandLineArgs_Empty(t *testing.T) {
	args, err := parseCommandLineArgs("")

	assert.NoError(t, err)
	assert.Len(t, args, 0)
}

func TestParseCommandLineArgs_SingleArg(t *testing.T) {
	args, err := parseCommandLineArgs("--verbose")

	assert.NoError(t, err)
	assert.Len(t, args, 1)
	assert.Equal(t, "--verbose", args[0])
}

func TestParseCommandLineArgs_DoubleQuotes(t *testing.T) {
	args, err := parseCommandLineArgs(`--message "hello world" --name test`)

	assert.NoError(t, err)
	assert.Len(t, args, 4)
	assert.Equal(t, "--message", args[0])
	assert.Equal(t, "hello world", args[1])
	assert.Equal(t, "--name", args[2])
	assert.Equal(t, "test", args[3])
}

func TestParseCommandLineArgs_SingleQuotes(t *testing.T) {
	args, err := parseCommandLineArgs("--message 'hello world' --name test")

	assert.NoError(t, err)
	assert.Len(t, args, 4)
	assert.Equal(t, "--message", args[0])
	assert.Equal(t, "hello world", args[1])
}

func TestParseCommandLineArgs_MixedQuotes(t *testing.T) {
	args, err := parseCommandLineArgs(`--msg1 "hello" --msg2 'world'`)

	assert.NoError(t, err)
	assert.Len(t, args, 4)
	assert.Equal(t, "hello", args[1])
	assert.Equal(t, "world", args[3])
}

func TestParseCommandLineArgs_QuotesInQuotes(t *testing.T) {
	args, err := parseCommandLineArgs(`--message "it's okay" --other test`)

	assert.NoError(t, err)
	assert.Len(t, args, 4)
	assert.Equal(t, "it's okay", args[1])
}

func TestParseCommandLineArgs_UnterminatedQuote(t *testing.T) {
	_, err := parseCommandLineArgs(`--message "hello world`)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated quote")
}

func TestParseCommandLineArgs_MultipleSpaces(t *testing.T) {
	args, err := parseCommandLineArgs("--flag1   value1    --flag2   value2")

	assert.NoError(t, err)
	assert.Len(t, args, 4)
}

func TestParseCommandLineArgs_EmptyQuotedString(t *testing.T) {
	// Note: The implementation skips empty arguments, so "" is not preserved
	args, err := parseCommandLineArgs(`--message "" --other value`)

	assert.NoError(t, err)
	// Empty quoted string is not preserved, so we get 3 args
	assert.Len(t, args, 3)
	assert.Equal(t, "--message", args[0])
	assert.Equal(t, "--other", args[1])
	assert.Equal(t, "value", args[2])
}

// Test transformResource function
func TestTransformResource_Basic(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":            "test-cm",
				"namespace":       "source-ns",
				"resourceVersion": "12345",
				"uid":             "abc123",
			},
			"data": map[string]interface{}{
				"key": "value",
			},
		},
	}

	transformed, err := transformResource(resource, "dest-ns")

	assert.NoError(t, err)
	assert.Equal(t, "dest-ns", transformed.GetNamespace())
	assert.Empty(t, transformed.GetResourceVersion())
	assert.Empty(t, string(transformed.GetUID()))
}

func TestTransformResource_ClearsStatus(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-cm",
				"namespace": "source-ns",
			},
			"status": map[string]interface{}{
				"phase": "Active",
			},
		},
	}

	transformed, err := transformResource(resource, "dest-ns")

	assert.NoError(t, err)
	assert.Nil(t, transformed.Object["status"])
}

func TestTransformResource_ClearsManagedFields(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-cm",
				"namespace": "source-ns",
				"managedFields": []interface{}{
					map[string]interface{}{
						"manager": "kubectl",
					},
				},
			},
		},
	}

	transformed, err := transformResource(resource, "dest-ns")

	assert.NoError(t, err)
	assert.Nil(t, transformed.GetManagedFields())
}

// Test handleServiceTransform function
func TestHandleServiceTransform(t *testing.T) {
	service := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "test-svc",
				"namespace": "source-ns",
			},
			"spec": map[string]interface{}{
				"clusterIP":   "10.0.0.1",
				"clusterIPs":  []interface{}{"10.0.0.1"},
				"type":        "ClusterIP",
				"ipFamilies":  []interface{}{"IPv4"},
				"selector":    map[string]interface{}{"app": "test"},
				"ports":       []interface{}{},
				"loadBalancerIP": "1.2.3.4",
			},
		},
	}

	handleServiceTransform(service)

	spec := service.Object["spec"].(map[string]interface{})

	// These should be removed
	assert.Nil(t, spec["clusterIP"])
	assert.Nil(t, spec["clusterIPs"])
	assert.Nil(t, spec["ipFamilies"])
	assert.Nil(t, spec["loadBalancerIP"])

	// These should remain
	assert.Equal(t, "ClusterIP", spec["type"])
	assert.NotNil(t, spec["selector"])
}

// Test handleDeploymentTransform function
func TestHandleDeploymentTransform(t *testing.T) {
	replicas := int64(3)
	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-deploy",
				"namespace": "source-ns",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"kind": "SomeOwner",
						"name": "owner",
					},
				},
			},
			"spec": map[string]interface{}{
				"replicas": replicas,
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "test",
					},
				},
			},
		},
	}

	handleDeploymentTransform(deployment)

	// Should add annotation with original replicas
	annotations := deployment.GetAnnotations()
	assert.NotNil(t, annotations)
	assert.Equal(t, "3", annotations[OriginalReplicasAnnotation])

	// Should remove owner references
	assert.Nil(t, deployment.GetOwnerReferences())
}

func TestHandleDeploymentTransform_NoReplicas(t *testing.T) {
	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-deploy",
				"namespace": "source-ns",
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{},
			},
		},
	}

	handleDeploymentTransform(deployment)

	// Should not fail with no replicas field
	// Annotations might be nil or not contain original replicas
	annotations := deployment.GetAnnotations()
	if annotations != nil {
		_, exists := annotations[OriginalReplicasAnnotation]
		assert.False(t, exists)
	}
}

// Test handleStatefulSetTransform function
func TestHandleStatefulSetTransform(t *testing.T) {
	replicas := int64(5)
	statefulset := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      "test-sts",
				"namespace": "source-ns",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"kind": "SomeOwner",
						"name": "owner",
					},
				},
			},
			"spec": map[string]interface{}{
				"replicas":    replicas,
				"serviceName": "test-svc",
				"volumeClaimTemplates": []interface{}{
					map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "data",
						},
						"spec": map[string]interface{}{
							"accessModes": []interface{}{"ReadWriteOnce"},
						},
						"status": map[string]interface{}{
							"phase": "Bound",
						},
					},
				},
			},
		},
	}

	handleStatefulSetTransform(statefulset)

	// Should add annotation with original replicas
	annotations := statefulset.GetAnnotations()
	assert.NotNil(t, annotations)
	assert.Equal(t, "5", annotations[OriginalReplicasAnnotation])

	// Should remove owner references
	assert.Nil(t, statefulset.GetOwnerReferences())

	// Should remove status from volumeClaimTemplates
	templates, _, _ := unstructured.NestedSlice(statefulset.Object, "spec", "volumeClaimTemplates")
	assert.Len(t, templates, 1)
	template := templates[0].(map[string]interface{})
	assert.Nil(t, template["status"])
}

// Test handleIngressTransform function
func TestHandleIngressTransform(t *testing.T) {
	ingress := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "test-ingress",
				"namespace": "source-ns",
				"annotations": map[string]interface{}{
					"kubernetes.io/ingress.class": "nginx",
					"other-annotation":            "value",
				},
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"kind": "SomeOwner",
						"name": "owner",
					},
				},
			},
			"spec": map[string]interface{}{
				"rules": []interface{}{},
			},
		},
	}

	handleIngressTransform(ingress)

	annotations := ingress.GetAnnotations()

	// Should remove kubernetes.io/ingress.class
	_, exists := annotations["kubernetes.io/ingress.class"]
	assert.False(t, exists)

	// Should keep other annotations
	assert.Equal(t, "value", annotations["other-annotation"])

	// Should remove owner references
	assert.Nil(t, ingress.GetOwnerReferences())
}

func TestHandleIngressTransform_NoIngressClassAnnotation(t *testing.T) {
	ingress := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name":      "test-ingress",
				"namespace": "source-ns",
				"annotations": map[string]interface{}{
					"other-annotation": "value",
				},
			},
		},
	}

	handleIngressTransform(ingress)

	annotations := ingress.GetAnnotations()
	assert.Equal(t, "value", annotations["other-annotation"])
}

// Test realistic scenarios
func TestConfig_StageMode(t *testing.T) {
	config := &Config{
		SourceKubeconfig: "/kubeconfig/prod",
		DestKubeconfig:   "/kubeconfig/dr",
		SourceNamespace:  "production",
		DestNamespace:    "production-dr",
		Mode:             "Stage",
		MigratePVCData:   true,
	}

	assert.Equal(t, "Stage", config.Mode)
	assert.True(t, config.MigratePVCData)
}

func TestConfig_CutoverMode(t *testing.T) {
	config := &Config{
		SourceKubeconfig: "/kubeconfig/prod",
		DestKubeconfig:   "/kubeconfig/dr",
		SourceNamespace:  "production",
		DestNamespace:    "production-dr",
		Mode:             "Cutover",
	}

	assert.Equal(t, "Cutover", config.Mode)
}

func TestConfig_FailbackMode(t *testing.T) {
	config := &Config{
		SourceKubeconfig:      "/kubeconfig/prod",
		DestKubeconfig:        "/kubeconfig/dr",
		SourceNamespace:       "production",
		DestNamespace:         "production-dr",
		Mode:                  "Failback",
		ReverseMigratePVCData: true,
	}

	assert.Equal(t, "Failback", config.Mode)
	assert.True(t, config.ReverseMigratePVCData)
}

func TestTransformResource_Deployment(t *testing.T) {
	replicas := int64(3)
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":            "nginx",
				"namespace":       "source",
				"resourceVersion": "12345",
				"uid":             "abc123",
				"creationTimestamp": "2023-01-01T00:00:00Z",
				"managedFields": []interface{}{
					map[string]interface{}{"manager": "kubectl"},
				},
			},
			"spec": map[string]interface{}{
				"replicas": replicas,
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "nginx",
					},
				},
			},
			"status": map[string]interface{}{
				"readyReplicas": int64(3),
			},
		},
	}

	transformed, err := transformResource(resource, "dest")

	assert.NoError(t, err)
	assert.Equal(t, "dest", transformed.GetNamespace())
	assert.Empty(t, transformed.GetResourceVersion())
	assert.Empty(t, string(transformed.GetUID()))
	assert.True(t, transformed.GetCreationTimestamp().Time.IsZero())
	assert.Nil(t, transformed.GetManagedFields())
	assert.Nil(t, transformed.Object["status"])

	// Deployment-specific transformations
	annotations := transformed.GetAnnotations()
	assert.Equal(t, "3", annotations[OriginalReplicasAnnotation])
}

func TestTransformResource_Service(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "my-svc",
				"namespace": "source",
			},
			"spec": map[string]interface{}{
				"clusterIP":   "10.0.0.100",
				"clusterIPs":  []interface{}{"10.0.0.100"},
				"type":        "LoadBalancer",
				"loadBalancerIP": "1.2.3.4",
				"ports": []interface{}{
					map[string]interface{}{
						"port":     int64(80),
						"protocol": "TCP",
					},
				},
			},
		},
	}

	transformed, err := transformResource(resource, "dest")

	assert.NoError(t, err)
	spec := transformed.Object["spec"].(map[string]interface{})
	assert.Nil(t, spec["clusterIP"])
	assert.Nil(t, spec["clusterIPs"])
	assert.Nil(t, spec["loadBalancerIP"])
	assert.Equal(t, "LoadBalancer", spec["type"])
	assert.NotNil(t, spec["ports"])
}

// Test ShouldSyncResourceType with complex scenarios
func TestShouldSyncResourceType_CompleteScenario(t *testing.T) {
	config := &Config{
		IncludeCustomResources: true,
		ResourceTypes:          []string{"deployments", "mycrd"},
		ExcludeResourceTypes:   []string{"secrets"},
	}

	// Explicitly listed deployment should sync
	assert.True(t, config.ShouldSyncResourceType("deployments", false))

	// Explicitly listed custom resource should sync
	assert.True(t, config.ShouldSyncResourceType("mycrd", true))

	// Not in explicit list
	assert.False(t, config.ShouldSyncResourceType("configmaps", false))

	// Excluded takes precedence
	assert.False(t, config.ShouldSyncResourceType("secrets", false))
}

// Test empty creation timestamp is zero
func TestTransformResource_CreationTimestampCleared(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":              "test",
				"namespace":         "source",
				"creationTimestamp": "2023-12-01T10:00:00Z",
			},
		},
	}

	transformed, err := transformResource(resource, "dest")

	assert.NoError(t, err)
	assert.Equal(t, metav1.Time{}, transformed.GetCreationTimestamp())
}

// Test preserve data while clearing metadata
func TestTransformResource_PreservesData(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test",
				"namespace": "source",
			},
			"data": map[string]interface{}{
				"config.yaml": "key: value",
				"app.conf":    "setting=true",
			},
		},
	}

	transformed, err := transformResource(resource, "dest")

	assert.NoError(t, err)
	data := transformed.Object["data"].(map[string]interface{})
	assert.Equal(t, "key: value", data["config.yaml"])
	assert.Equal(t, "setting=true", data["app.conf"])
}
