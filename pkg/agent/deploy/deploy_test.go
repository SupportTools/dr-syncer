package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestConstants(t *testing.T) {
	// Test that constants are defined with expected values
	assert.Equal(t, "dr-syncer", agentNamespace)
	assert.Equal(t, "dr-syncer-agent", agentName)
}

func TestNewDeployer(t *testing.T) {
	// Test creating a deployer with nil client
	deployer := NewDeployer(nil)
	assert.NotNil(t, deployer)
	assert.Nil(t, deployer.client)
}

func TestDeployer_Struct(t *testing.T) {
	// Test the Deployer struct
	deployer := &Deployer{}
	assert.NotNil(t, deployer)
	assert.Nil(t, deployer.client)
}

// Tests for convertEnvToMap function

func TestConvertEnvToMap_Empty(t *testing.T) {
	result := convertEnvToMap([]corev1.EnvVar{})
	assert.NotNil(t, result)
	assert.Len(t, result, 0)
}

func TestConvertEnvToMap_Nil(t *testing.T) {
	result := convertEnvToMap(nil)
	assert.NotNil(t, result)
	assert.Len(t, result, 0)
}

func TestConvertEnvToMap_DirectValue(t *testing.T) {
	envVars := []corev1.EnvVar{
		{Name: "MY_VAR", Value: "my-value"},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 1)
	assert.Equal(t, "my-value", result["MY_VAR"])
}

func TestConvertEnvToMap_MultipleDirectValues(t *testing.T) {
	envVars := []corev1.EnvVar{
		{Name: "VAR1", Value: "value1"},
		{Name: "VAR2", Value: "value2"},
		{Name: "VAR3", Value: "value3"},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 3)
	assert.Equal(t, "value1", result["VAR1"])
	assert.Equal(t, "value2", result["VAR2"])
	assert.Equal(t, "value3", result["VAR3"])
}

func TestConvertEnvToMap_FieldRef(t *testing.T) {
	envVars := []corev1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 1)
	assert.Equal(t, "fieldRef:spec.nodeName", result["NODE_NAME"])
}

func TestConvertEnvToMap_ResourceFieldRef(t *testing.T) {
	envVars := []corev1.EnvVar{
		{
			Name: "CPU_LIMIT",
			ValueFrom: &corev1.EnvVarSource{
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource: "limits.cpu",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 1)
	assert.Equal(t, "resourceFieldRef:limits.cpu", result["CPU_LIMIT"])
}

func TestConvertEnvToMap_ConfigMapKeyRef(t *testing.T) {
	envVars := []corev1.EnvVar{
		{
			Name: "CONFIG_VALUE",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "my-config",
					},
					Key: "config-key",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 1)
	assert.Equal(t, "configMapKeyRef:my-config:config-key", result["CONFIG_VALUE"])
}

func TestConvertEnvToMap_SecretKeyRef(t *testing.T) {
	envVars := []corev1.EnvVar{
		{
			Name: "SECRET_VALUE",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "my-secret",
					},
					Key: "secret-key",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 1)
	assert.Equal(t, "secretKeyRef:my-secret:secret-key", result["SECRET_VALUE"])
}

func TestConvertEnvToMap_MixedTypes(t *testing.T) {
	envVars := []corev1.EnvVar{
		{Name: "DIRECT_VAR", Value: "direct-value"},
		{
			Name: "FIELD_REF_VAR",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "CONFIG_VAR",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "app-config",
					},
					Key: "app-setting",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 3)
	assert.Equal(t, "direct-value", result["DIRECT_VAR"])
	assert.Equal(t, "fieldRef:metadata.name", result["FIELD_REF_VAR"])
	assert.Equal(t, "configMapKeyRef:app-config:app-setting", result["CONFIG_VAR"])
}

func TestConvertEnvToMap_EmptyValue(t *testing.T) {
	// Empty string value should not be added to the map
	envVars := []corev1.EnvVar{
		{Name: "EMPTY_VAR", Value: ""},
	}

	result := convertEnvToMap(envVars)

	// Empty string value is not added because if env.Value != "" check fails
	assert.Len(t, result, 0)
}

func TestConvertEnvToMap_EmptyValueFrom(t *testing.T) {
	// EnvVar with ValueFrom but no specific source
	envVars := []corev1.EnvVar{
		{
			Name:      "NO_SOURCE",
			ValueFrom: &corev1.EnvVarSource{},
		},
	}

	result := convertEnvToMap(envVars)

	// No specific source means nothing is added to the map
	assert.Len(t, result, 0)
}

func TestConvertEnvToMap_RealisticAgentEnv(t *testing.T) {
	// Simulate realistic agent environment variables
	envVars := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2222"},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
		{Name: "LOG_LEVEL", Value: "info"},
		{Name: "AGENT_MODE", Value: "daemon"},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 4)
	assert.Equal(t, "2222", result["SSH_PORT"])
	assert.Equal(t, "fieldRef:spec.nodeName", result["NODE_NAME"])
	assert.Equal(t, "info", result["LOG_LEVEL"])
	assert.Equal(t, "daemon", result["AGENT_MODE"])
}

// Tests for areEnvMapsEqual function

func TestAreEnvMapsEqual_BothEmpty(t *testing.T) {
	map1 := map[string]string{}
	map2 := map[string]string{}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_BothNil(t *testing.T) {
	// Both nil maps should be equal (both have length 0)
	var map1 map[string]string
	var map2 map[string]string

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_OneNil(t *testing.T) {
	var nilMap map[string]string
	emptyMap := map[string]string{}

	// Nil and empty map should be equal (both have length 0)
	assert.True(t, areEnvMapsEqual(nilMap, emptyMap))
	assert.True(t, areEnvMapsEqual(emptyMap, nilMap))
}

func TestAreEnvMapsEqual_Equal(t *testing.T) {
	map1 := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	map2 := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_DifferentLength(t *testing.T) {
	map1 := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	map2 := map[string]string{
		"KEY1": "value1",
	}

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_DifferentValues(t *testing.T) {
	map1 := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	map2 := map[string]string{
		"KEY1": "value1",
		"KEY2": "different-value",
	}

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_DifferentKeys(t *testing.T) {
	map1 := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	map2 := map[string]string{
		"KEY1": "value1",
		"KEY3": "value2", // Different key
	}

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_SameKeysDifferentOrder(t *testing.T) {
	// Maps don't have order, so this should be equal
	map1 := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
		"KEY3": "value3",
	}
	map2 := map[string]string{
		"KEY3": "value3",
		"KEY1": "value1",
		"KEY2": "value2",
	}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_SingleEntry(t *testing.T) {
	map1 := map[string]string{"KEY": "value"}
	map2 := map[string]string{"KEY": "value"}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_SingleEntryDifferent(t *testing.T) {
	map1 := map[string]string{"KEY": "value1"}
	map2 := map[string]string{"KEY": "value2"}

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_LargeMap(t *testing.T) {
	map1 := make(map[string]string)
	map2 := make(map[string]string)

	for i := 0; i < 100; i++ {
		key := "KEY" + string(rune(i))
		value := "value" + string(rune(i))
		map1[key] = value
		map2[key] = value
	}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_LargeMapOneDifferent(t *testing.T) {
	map1 := make(map[string]string)
	map2 := make(map[string]string)

	for i := 0; i < 100; i++ {
		key := "KEY" + string(rune(i))
		value := "value" + string(rune(i))
		map1[key] = value
		map2[key] = value
	}

	// Change one value
	map2["KEY50"] = "different"

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_EmptyStrings(t *testing.T) {
	map1 := map[string]string{"KEY": ""}
	map2 := map[string]string{"KEY": ""}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestAreEnvMapsEqual_SpecialCharacters(t *testing.T) {
	map1 := map[string]string{
		"KEY_WITH_UNDERSCORE": "value",
		"KEY-WITH-DASH":       "value",
		"KEY.WITH.DOT":        "value",
	}
	map2 := map[string]string{
		"KEY_WITH_UNDERSCORE": "value",
		"KEY-WITH-DASH":       "value",
		"KEY.WITH.DOT":        "value",
	}

	assert.True(t, areEnvMapsEqual(map1, map2))
}

// Integration tests combining both functions

func TestConvertEnvToMap_And_AreEnvMapsEqual_Integration(t *testing.T) {
	env1 := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2222"},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	}

	env2 := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2222"},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	}

	map1 := convertEnvToMap(env1)
	map2 := convertEnvToMap(env2)

	assert.True(t, areEnvMapsEqual(map1, map2))
}

func TestConvertEnvToMap_And_AreEnvMapsEqual_Different(t *testing.T) {
	env1 := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2222"},
	}

	env2 := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2223"}, // Different value
	}

	map1 := convertEnvToMap(env1)
	map2 := convertEnvToMap(env2)

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestConvertEnvToMap_And_AreEnvMapsEqual_DifferentFieldRef(t *testing.T) {
	env1 := []corev1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	}

	env2 := []corev1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name", // Different field path
				},
			},
		},
	}

	map1 := convertEnvToMap(env1)
	map2 := convertEnvToMap(env2)

	assert.False(t, areEnvMapsEqual(map1, map2))
}

func TestConvertEnvToMap_And_AreEnvMapsEqual_ExtraEnvVar(t *testing.T) {
	env1 := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2222"},
	}

	env2 := []corev1.EnvVar{
		{Name: "SSH_PORT", Value: "2222"},
		{Name: "EXTRA_VAR", Value: "extra-value"}, // Extra env var
	}

	map1 := convertEnvToMap(env1)
	map2 := convertEnvToMap(env2)

	assert.False(t, areEnvMapsEqual(map1, map2))
}

// Test edge cases with realistic k8s env var names

func TestConvertEnvToMap_KubernetesEnvVars(t *testing.T) {
	// These are typical env vars that Kubernetes sets
	envVars := []corev1.EnvVar{
		{
			Name: "KUBERNETES_SERVICE_HOST",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
		{Name: "KUBERNETES_SERVICE_PORT", Value: "443"},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 4)
	assert.Equal(t, "fieldRef:status.hostIP", result["KUBERNETES_SERVICE_HOST"])
	assert.Equal(t, "443", result["KUBERNETES_SERVICE_PORT"])
	assert.Equal(t, "fieldRef:metadata.name", result["POD_NAME"])
	assert.Equal(t, "fieldRef:metadata.namespace", result["POD_NAMESPACE"])
}

func TestConvertEnvToMap_ResourceLimits(t *testing.T) {
	envVars := []corev1.EnvVar{
		{
			Name: "CPU_LIMIT",
			ValueFrom: &corev1.EnvVarSource{
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource: "limits.cpu",
				},
			},
		},
		{
			Name: "MEMORY_LIMIT",
			ValueFrom: &corev1.EnvVarSource{
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource: "limits.memory",
				},
			},
		},
		{
			Name: "CPU_REQUEST",
			ValueFrom: &corev1.EnvVarSource{
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource: "requests.cpu",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 3)
	assert.Equal(t, "resourceFieldRef:limits.cpu", result["CPU_LIMIT"])
	assert.Equal(t, "resourceFieldRef:limits.memory", result["MEMORY_LIMIT"])
	assert.Equal(t, "resourceFieldRef:requests.cpu", result["CPU_REQUEST"])
}

// Test Deployer pointer operations

func TestNewDeployer_ReturnsNonNilPointer(t *testing.T) {
	deployer := NewDeployer(nil)
	assert.NotNil(t, deployer)

	// Verify it's a valid pointer that can be used
	var _ *Deployer = deployer
}

func TestDeployer_ClientField(t *testing.T) {
	deployer := &Deployer{
		client: nil,
	}
	assert.Nil(t, deployer.client)
}

// Test combined env var scenarios that could occur in real deployments

func TestConvertEnvToMap_FullAgentConfig(t *testing.T) {
	// Simulate a full agent configuration with all types of env vars
	envVars := []corev1.EnvVar{
		// Direct values
		{Name: "SSH_PORT", Value: "2222"},
		{Name: "LOG_LEVEL", Value: "debug"},
		{Name: "SYNC_INTERVAL", Value: "60"},
		// Field references
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		// ConfigMap reference
		{
			Name: "APP_CONFIG",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "agent-config",
					},
					Key: "settings",
				},
			},
		},
		// Secret reference
		{
			Name: "SSH_PRIVATE_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "ssh-keys",
					},
					Key: "id_rsa",
				},
			},
		},
	}

	result := convertEnvToMap(envVars)

	assert.Len(t, result, 7)
	assert.Equal(t, "2222", result["SSH_PORT"])
	assert.Equal(t, "debug", result["LOG_LEVEL"])
	assert.Equal(t, "60", result["SYNC_INTERVAL"])
	assert.Equal(t, "fieldRef:spec.nodeName", result["NODE_NAME"])
	assert.Equal(t, "fieldRef:status.podIP", result["POD_IP"])
	assert.Equal(t, "configMapKeyRef:agent-config:settings", result["APP_CONFIG"])
	assert.Equal(t, "secretKeyRef:ssh-keys:id_rsa", result["SSH_PRIVATE_KEY"])
}
