package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test constants for PVC sync
func TestPVCConstants(t *testing.T) {
	assert.Equal(t, "dr-syncer.io/storage-class", StorageClassLabel)
	assert.Equal(t, "dr-syncer.io/access-mode", AccessModeLabel)
	assert.Equal(t, "dr-syncer.io/sync-pv", SyncPVLabel)
}

// Test immutable handling constants
func TestImmutableConstants(t *testing.T) {
	assert.Equal(t, "dr-syncer.io/immutable-handling", ImmutableHandlingLabel)
}

// Test getStorageClassMapping
func TestGetStorageClassMapping_NoMappings(t *testing.T) {
	result := getStorageClassMapping("fast-storage", nil)
	assert.Equal(t, "fast-storage", result)
}

func TestGetStorageClassMapping_EmptyMappings(t *testing.T) {
	result := getStorageClassMapping("fast-storage", []drv1alpha1.StorageClassMapping{})
	assert.Equal(t, "fast-storage", result)
}

func TestGetStorageClassMapping_MatchFound(t *testing.T) {
	mappings := []drv1alpha1.StorageClassMapping{
		{From: "fast-storage", To: "dr-fast-storage"},
		{From: "standard", To: "dr-standard"},
	}

	result := getStorageClassMapping("fast-storage", mappings)
	assert.Equal(t, "dr-fast-storage", result)
}

func TestGetStorageClassMapping_NoMatch(t *testing.T) {
	mappings := []drv1alpha1.StorageClassMapping{
		{From: "fast-storage", To: "dr-fast-storage"},
		{From: "standard", To: "dr-standard"},
	}

	result := getStorageClassMapping("slow-storage", mappings)
	assert.Equal(t, "slow-storage", result) // Original returned when no match
}

func TestGetStorageClassMapping_FirstMatchWins(t *testing.T) {
	mappings := []drv1alpha1.StorageClassMapping{
		{From: "fast-storage", To: "dr-fast-1"},
		{From: "fast-storage", To: "dr-fast-2"},
	}

	result := getStorageClassMapping("fast-storage", mappings)
	assert.Equal(t, "dr-fast-1", result) // First match wins
}

func TestGetStorageClassMapping_EmptySourceClass(t *testing.T) {
	mappings := []drv1alpha1.StorageClassMapping{
		{From: "", To: "default-class"},
	}

	result := getStorageClassMapping("", mappings)
	assert.Equal(t, "default-class", result)
}

// Test getAccessModeMapping
func TestGetAccessModeMapping_NoMappings(t *testing.T) {
	result := getAccessModeMapping(corev1.ReadWriteOnce, nil)
	assert.Equal(t, corev1.ReadWriteOnce, result)
}

func TestGetAccessModeMapping_EmptyMappings(t *testing.T) {
	result := getAccessModeMapping(corev1.ReadWriteOnce, []drv1alpha1.AccessModeMapping{})
	assert.Equal(t, corev1.ReadWriteOnce, result)
}

func TestGetAccessModeMapping_MatchFound(t *testing.T) {
	mappings := []drv1alpha1.AccessModeMapping{
		{From: "ReadWriteOnce", To: "ReadWriteMany"},
		{From: "ReadOnlyMany", To: "ReadWriteMany"},
	}

	result := getAccessModeMapping(corev1.ReadWriteOnce, mappings)
	assert.Equal(t, corev1.ReadWriteMany, result)
}

func TestGetAccessModeMapping_NoMatch(t *testing.T) {
	mappings := []drv1alpha1.AccessModeMapping{
		{From: "ReadWriteOnce", To: "ReadWriteMany"},
	}

	result := getAccessModeMapping(corev1.ReadOnlyMany, mappings)
	assert.Equal(t, corev1.ReadOnlyMany, result) // Original returned when no match
}

func TestGetAccessModeMapping_AllModes(t *testing.T) {
	testCases := []struct {
		name     string
		srcMode  corev1.PersistentVolumeAccessMode
		mappings []drv1alpha1.AccessModeMapping
		expected corev1.PersistentVolumeAccessMode
	}{
		{
			name:    "ReadWriteOnce to ReadWriteMany",
			srcMode: corev1.ReadWriteOnce,
			mappings: []drv1alpha1.AccessModeMapping{
				{From: "ReadWriteOnce", To: "ReadWriteMany"},
			},
			expected: corev1.ReadWriteMany,
		},
		{
			name:    "ReadOnlyMany to ReadWriteMany",
			srcMode: corev1.ReadOnlyMany,
			mappings: []drv1alpha1.AccessModeMapping{
				{From: "ReadOnlyMany", To: "ReadWriteMany"},
			},
			expected: corev1.ReadWriteMany,
		},
		{
			name:    "ReadWriteMany to ReadWriteOnce",
			srcMode: corev1.ReadWriteMany,
			mappings: []drv1alpha1.AccessModeMapping{
				{From: "ReadWriteMany", To: "ReadWriteOnce"},
			},
			expected: corev1.ReadWriteOnce,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getAccessModeMapping(tc.srcMode, tc.mappings)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Test NewImmutableResourceHandler
func TestNewImmutableResourceHandler(t *testing.T) {
	handler := NewImmutableResourceHandler(nil, nil, nil)

	assert.NotNil(t, handler)
	assert.Nil(t, handler.sourceClient)
	assert.Nil(t, handler.destClient)
	assert.Nil(t, handler.ctrlClient)
}

// Test DetermineHandling
func TestDetermineHandling_LabelOverride_NoChange(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ImmutableHandlingLabel: "no-change",
			},
		},
	}

	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.NoChange, result)
}

func TestDetermineHandling_LabelOverride_Recreate(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ImmutableHandlingLabel: "recreate",
			},
		},
	}

	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.Recreate, result)
}

func TestDetermineHandling_LabelOverride_RecreateWithDrain(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ImmutableHandlingLabel: "recreate-with-drain",
			},
		},
	}

	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.RecreateWithPodDrain, result)
}

func TestDetermineHandling_LabelOverride_PartialUpdate(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ImmutableHandlingLabel: "partial-update",
			},
		},
	}

	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.PartialUpdate, result)
}

func TestDetermineHandling_LabelOverride_ForceUpdate(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ImmutableHandlingLabel: "force-update",
			},
		},
	}

	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.ForceUpdate, result)
}

func TestDetermineHandling_LabelOverride_Invalid(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ImmutableHandlingLabel: "invalid-value",
			},
		},
	}

	// Invalid label value falls through to config/default
	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.NoChange, result) // System default
}

func TestDetermineHandling_ConfigDefault(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	config := &drv1alpha1.ImmutableResourceConfig{
		DefaultHandling: drv1alpha1.Recreate,
	}

	result := handler.DetermineHandling(obj, config)
	assert.Equal(t, drv1alpha1.Recreate, result)
}

func TestDetermineHandling_NoConfigNoLabel(t *testing.T) {
	handler := &ImmutableResourceHandler{}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	result := handler.DetermineHandling(obj, nil)
	assert.Equal(t, drv1alpha1.NoChange, result) // System default
}

// Test updateConfigMapFields
func TestUpdateConfigMapFields_Basic(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
			Labels: map[string]string{
				"old-label": "old-value",
			},
		},
		Data: map[string]string{
			"old-key": "old-value",
		},
	}

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
			Labels: map[string]string{
				"new-label": "new-value",
			},
			Annotations: map[string]string{
				"annotation": "value",
			},
		},
		Data: map[string]string{
			"new-key": "new-value",
		},
		BinaryData: map[string][]byte{
			"binary": []byte("data"),
		},
	}

	result, err := handler.updateConfigMapFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.ConfigMap)
	assert.Equal(t, "test-cm", updated.Name)
	assert.Equal(t, "new-value", updated.Labels["new-label"])
	assert.Equal(t, "value", updated.Annotations["annotation"])
	assert.Equal(t, "new-value", updated.Data["new-key"])
	assert.Equal(t, []byte("data"), updated.BinaryData["binary"])
}

// Test updateSecretFields
func TestUpdateSecretFields_Basic(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"old-key": []byte("old-value"),
		},
	}

	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
			Labels: map[string]string{
				"new-label": "new-value",
			},
		},
		Type: corev1.SecretTypeOpaque, // Type is immutable
		Data: map[string][]byte{
			"new-key": []byte("new-value"),
		},
		StringData: map[string]string{
			"string-key": "string-value",
		},
	}

	result, err := handler.updateSecretFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.Secret)
	assert.Equal(t, "test-secret", updated.Name)
	assert.Equal(t, []byte("new-value"), updated.Data["new-key"])
	assert.Equal(t, "string-value", updated.StringData["string-key"])
	assert.Equal(t, "new-value", updated.Labels["new-label"])
}

// Test updateServiceFields
func TestUpdateServiceFields_PreservesClusterIP(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:  "10.0.0.100",
			ClusterIPs: []string{"10.0.0.100"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80},
			},
		},
	}

	source := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Labels: map[string]string{
				"updated": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:  "10.0.0.200", // This should be ignored
			ClusterIPs: []string{"10.0.0.200"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080}, // Updated port
			},
			Selector: map[string]string{
				"app": "updated",
			},
		},
	}

	result, err := handler.updateServiceFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.Service)

	// Immutable fields preserved
	assert.Equal(t, "10.0.0.100", updated.Spec.ClusterIP)
	assert.Equal(t, []string{"10.0.0.100"}, updated.Spec.ClusterIPs)

	// Mutable fields updated
	assert.Equal(t, int32(8080), updated.Spec.Ports[0].Port)
	assert.Equal(t, "updated", updated.Spec.Selector["app"])
	assert.Equal(t, "true", updated.Labels["updated"])
}

func TestUpdateServiceFields_UpdatesMutableFields(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.100",
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	externalPolicy := corev1.ServiceExternalTrafficPolicyLocal
	source := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ExternalIPs:           []string{"1.2.3.4"},
			LoadBalancerIP:        "5.6.7.8",
			ExternalName:          "external.example.com",
			ExternalTrafficPolicy: externalPolicy,
			SessionAffinity:       corev1.ServiceAffinityClientIP,
		},
	}

	result, err := handler.updateServiceFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.Service)

	assert.Equal(t, []string{"1.2.3.4"}, updated.Spec.ExternalIPs)
	assert.Equal(t, "5.6.7.8", updated.Spec.LoadBalancerIP)
	assert.Equal(t, "external.example.com", updated.Spec.ExternalName)
	assert.Equal(t, externalPolicy, updated.Spec.ExternalTrafficPolicy)
	assert.Equal(t, corev1.ServiceAffinityClientIP, updated.Spec.SessionAffinity)
}

// Test updatePVCFields
func TestUpdatePVCFields_ExpandsStorage(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
			Labels: map[string]string{
				"updated": "true",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("20Gi"), // Expansion
				},
			},
		},
	}

	result, err := handler.updatePVCFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.PersistentVolumeClaim)

	// Storage should be expanded
	storage := updated.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "20Gi", storage.String())
	assert.Equal(t, "true", updated.Labels["updated"])
}

func TestUpdatePVCFields_DoesNotContract(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("20Gi"),
				},
			},
		},
	}

	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"), // Contraction attempt
				},
			},
		},
	}

	result, err := handler.updatePVCFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.PersistentVolumeClaim)

	// Storage should NOT be contracted
	storage := updated.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "20Gi", storage.String())
}

func TestUpdatePVCFields_NoStorageChange(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"), // Same size
				},
			},
		},
	}

	result, err := handler.updatePVCFields(current, source)

	require.NoError(t, err)
	updated := result.(*corev1.PersistentVolumeClaim)

	// Storage should remain the same
	storage := updated.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", storage.String())
}

// Test updateDeploymentFields
func TestUpdateDeploymentFields_PreservesSelector(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	replicas := int32(3)
	current := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "original",
				},
			},
		},
	}

	newReplicas := int32(5)
	source := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			Labels: map[string]string{
				"updated": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &newReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "updated", // This should be ignored
				},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			},
		},
	}

	result, err := handler.updateDeploymentFields(current, source)

	require.NoError(t, err)
	updated := result.(*appsv1.Deployment)

	// Selector should be preserved (immutable)
	assert.Equal(t, "original", updated.Spec.Selector.MatchLabels["app"])

	// Mutable fields should be updated
	assert.Equal(t, int32(5), *updated.Spec.Replicas)
	assert.Equal(t, "true", updated.Labels["updated"])
	assert.Len(t, updated.Spec.Template.Spec.Containers, 1)
}

func TestUpdateDeploymentFields_UpdatesMutableFields(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	replicas := int32(3)
	minReady := int32(5)
	revisionLimit := int32(5)
	progressDeadline := int32(300)

	current := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}

	newReplicas := int32(5)
	source := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:                &newReplicas,
			MinReadySeconds:         minReady,
			RevisionHistoryLimit:    &revisionLimit,
			Paused:                  true,
			ProgressDeadlineSeconds: &progressDeadline,
		},
	}

	result, err := handler.updateDeploymentFields(current, source)

	require.NoError(t, err)
	updated := result.(*appsv1.Deployment)

	assert.Equal(t, int32(5), *updated.Spec.Replicas)
	assert.Equal(t, int32(5), updated.Spec.MinReadySeconds)
	assert.Equal(t, int32(5), *updated.Spec.RevisionHistoryLimit)
	assert.True(t, updated.Spec.Paused)
	assert.Equal(t, int32(300), *updated.Spec.ProgressDeadlineSeconds)
}

// Test updateIngressFields
func TestUpdateIngressFields_Basic(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	className := "nginx"
	current := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			Rules: []networkingv1.IngressRule{
				{Host: "old.example.com"},
			},
		},
	}

	newClassName := "traefik"
	source := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: "default",
			Labels: map[string]string{
				"updated": "true",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &newClassName,
			Rules: []networkingv1.IngressRule{
				{Host: "new.example.com"},
			},
			TLS: []networkingv1.IngressTLS{
				{Hosts: []string{"new.example.com"}, SecretName: "tls-secret"},
			},
		},
	}

	result, err := handler.updateIngressFields(current, source)

	require.NoError(t, err)
	updated := result.(*networkingv1.Ingress)

	assert.Equal(t, "traefik", *updated.Spec.IngressClassName)
	assert.Equal(t, "new.example.com", updated.Spec.Rules[0].Host)
	assert.Len(t, updated.Spec.TLS, 1)
	assert.Equal(t, "tls-secret", updated.Spec.TLS[0].SecretName)
	assert.Equal(t, "true", updated.Labels["updated"])
}

// Test updateMetadataOnly
func TestUpdateMetadataOnly_Basic(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
			Labels: map[string]string{
				"old-label": "old-value",
			},
		},
		Data: map[string]string{
			"old-data": "should-not-change",
		},
	}

	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
			Labels: map[string]string{
				"new-label": "new-value",
			},
			Annotations: map[string]string{
				"annotation": "value",
			},
		},
		Data: map[string]string{
			"new-data": "should-be-ignored",
		},
	}

	result, err := handler.updateMetadataOnly(current, source)

	require.NoError(t, err)

	// Labels and annotations should be updated
	assert.Equal(t, "new-value", result.GetLabels()["new-label"])
	assert.Equal(t, "value", result.GetAnnotations()["annotation"])

	// Data should NOT be updated (only metadata is updated)
	updated := result.(*corev1.ConfigMap)
	assert.Equal(t, "should-not-change", updated.Data["old-data"])
}

// Test applyMutableFieldUpdates type switching
func TestApplyMutableFieldUpdates_ConfigMap(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Data:       map[string]string{"key": "old"},
	}
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Data:       map[string]string{"key": "new"},
	}

	result, err := handler.applyMutableFieldUpdates(current, source)
	require.NoError(t, err)

	cm := result.(*corev1.ConfigMap)
	assert.Equal(t, "new", cm.Data["key"])
}

func TestApplyMutableFieldUpdates_Secret(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Data:       map[string][]byte{"key": []byte("old")},
	}
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Data:       map[string][]byte{"key": []byte("new")},
	}

	result, err := handler.applyMutableFieldUpdates(current, source)
	require.NoError(t, err)

	secret := result.(*corev1.Secret)
	assert.Equal(t, []byte("new"), secret.Data["key"])
}

func TestApplyMutableFieldUpdates_Service(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.100",
			Ports:     []corev1.ServicePort{{Port: 80}},
		},
	}
	source := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.200", // Should be ignored
			Ports:     []corev1.ServicePort{{Port: 8080}},
		},
	}

	result, err := handler.applyMutableFieldUpdates(current, source)
	require.NoError(t, err)

	svc := result.(*corev1.Service)
	assert.Equal(t, "10.0.0.100", svc.Spec.ClusterIP) // Preserved
	assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)
}

func TestApplyMutableFieldUpdates_PVC(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("20Gi"),
				},
			},
		},
	}

	result, err := handler.applyMutableFieldUpdates(current, source)
	require.NoError(t, err)

	pvc := result.(*corev1.PersistentVolumeClaim)
	assert.Equal(t, "20Gi", pvc.Spec.Resources.Requests.Storage().String())
}

func TestApplyMutableFieldUpdates_Deployment(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	replicas := int32(3)
	current := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "original"},
			},
		},
	}

	newReplicas := int32(5)
	source := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &newReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "changed"},
			},
		},
	}

	result, err := handler.applyMutableFieldUpdates(current, source)
	require.NoError(t, err)

	deploy := result.(*appsv1.Deployment)
	assert.Equal(t, int32(5), *deploy.Spec.Replicas)
	assert.Equal(t, "original", deploy.Spec.Selector.MatchLabels["app"]) // Preserved
}

func TestApplyMutableFieldUpdates_Ingress(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	className := "nginx"
	current := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
		},
	}

	newClassName := "traefik"
	source := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &newClassName,
		},
	}

	result, err := handler.applyMutableFieldUpdates(current, source)
	require.NoError(t, err)

	ingress := result.(*networkingv1.Ingress)
	assert.Equal(t, "traefik", *ingress.Spec.IngressClassName)
}

func TestApplyMutableFieldUpdates_TypeMismatch(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	source := &corev1.Secret{ // Wrong type
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	_, err := handler.applyMutableFieldUpdates(current, source)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "current object is not a Secret")
}

// Test ImmutableResourceHandling type values
func TestImmutableResourceHandling_Values(t *testing.T) {
	assert.Equal(t, drv1alpha1.ImmutableResourceHandling("NoChange"), drv1alpha1.NoChange)
	assert.Equal(t, drv1alpha1.ImmutableResourceHandling("Recreate"), drv1alpha1.Recreate)
	assert.Equal(t, drv1alpha1.ImmutableResourceHandling("RecreateWithPodDrain"), drv1alpha1.RecreateWithPodDrain)
	assert.Equal(t, drv1alpha1.ImmutableResourceHandling("PartialUpdate"), drv1alpha1.PartialUpdate)
	assert.Equal(t, drv1alpha1.ImmutableResourceHandling("ForceUpdate"), drv1alpha1.ForceUpdate)
}

// Test edge cases
func TestUpdatePVCFields_NilSourceRequests(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       corev1.PersistentVolumeClaimSpec{},
	}

	result, err := handler.updatePVCFields(current, source)
	require.NoError(t, err)

	pvc := result.(*corev1.PersistentVolumeClaim)
	// Storage should be unchanged
	assert.Equal(t, "10Gi", pvc.Spec.Resources.Requests.Storage().String())
}

func TestUpdatePVCFields_NilCurrentRequests(t *testing.T) {
	handler := &ImmutableResourceHandler{}

	current := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       corev1.PersistentVolumeClaimSpec{},
	}

	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	result, err := handler.updatePVCFields(current, source)
	require.NoError(t, err)

	// Should not panic with nil current requests
	assert.NotNil(t, result)
}
