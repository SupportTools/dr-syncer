package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Test constants
func TestConstants(t *testing.T) {
	assert.Equal(t, "dr-syncer.io/ignore", IgnoreLabel)
	assert.Equal(t, "dr-syncer.io/scale-override", ScaleOverrideLabel)
}

// Test ParseInt32
func TestParseInt32_Valid(t *testing.T) {
	testCases := []struct {
		input    string
		expected int32
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"100", 100},
		{"-1", -1},
		{"-100", -100},
		{"2147483647", 2147483647},   // max int32
		{"-2147483648", -2147483648}, // min int32
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result, err := ParseInt32(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseInt32_Invalid(t *testing.T) {
	testCases := []string{
		"",
		"abc",
		"12.5",
		"12abc",
		"abc12",
		"2147483648",  // overflow
		"-2147483649", // underflow
		" 12",
		"12 ",
	}

	for _, input := range testCases {
		t.Run(input, func(t *testing.T) {
			_, err := ParseInt32(input)
			assert.Error(t, err)
		})
	}
}

// Test ShouldIgnoreResource
func TestShouldIgnoreResource_True(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Labels: map[string]string{
				IgnoreLabel: "true",
			},
		},
	}

	assert.True(t, ShouldIgnoreResource(pod))
}

func TestShouldIgnoreResource_False(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Labels: map[string]string{
				IgnoreLabel: "false",
			},
		},
	}

	assert.False(t, ShouldIgnoreResource(pod))
}

func TestShouldIgnoreResource_NotPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	assert.False(t, ShouldIgnoreResource(pod))
}

func TestShouldIgnoreResource_NoLabels(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-pod",
			Labels: nil,
		},
	}

	assert.False(t, ShouldIgnoreResource(pod))
}

func TestShouldIgnoreResource_OtherValue(t *testing.T) {
	// Only "true" should trigger ignore
	testCases := []string{"yes", "1", "TRUE", "True", "ignore", ""}

	for _, value := range testCases {
		t.Run(value, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Labels: map[string]string{
						IgnoreLabel: value,
					},
				},
			}
			assert.False(t, ShouldIgnoreResource(pod))
		})
	}
}

func TestShouldIgnoreResource_ConfigMap(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cm",
			Labels: map[string]string{
				IgnoreLabel: "true",
			},
		},
	}

	assert.True(t, ShouldIgnoreResource(cm))
}

func TestShouldIgnoreResource_Secret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
			Labels: map[string]string{
				IgnoreLabel: "true",
			},
		},
	}

	assert.True(t, ShouldIgnoreResource(secret))
}

func TestShouldIgnoreResource_Service(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-svc",
			Labels: map[string]string{
				IgnoreLabel: "true",
			},
		},
	}

	assert.True(t, ShouldIgnoreResource(svc))
}

// Test SanitizeMetadata
func TestSanitizeMetadata_ClearsFields(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod",
			Namespace:       "default",
			UID:             types.UID("12345"),
			ResourceVersion: "1",
			Generation:      5,
			SelfLink:        "/api/v1/pods/test-pod",
			CreationTimestamp: metav1.Time{
				Time: metav1.Now().Time,
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "kubectl"},
			},
			OwnerReferences: []metav1.OwnerReference{
				{Name: "owner"},
			},
			Finalizers: []string{"finalizer.example.com"},
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
				"custom-annotation": "value",
			},
			Labels: map[string]string{
				"app": "test",
			},
		},
	}

	SanitizeMetadata(pod)

	// These should be cleared
	assert.Empty(t, string(pod.UID))
	assert.Empty(t, pod.ResourceVersion)
	assert.Empty(t, pod.SelfLink)
	assert.True(t, pod.CreationTimestamp.IsZero())
	assert.Nil(t, pod.ManagedFields)
	assert.Nil(t, pod.OwnerReferences)
	assert.Zero(t, pod.Generation)
	assert.Nil(t, pod.Finalizers)

	// Last applied configuration should be removed
	assert.NotContains(t, pod.Annotations, "kubectl.kubernetes.io/last-applied-configuration")

	// These should be preserved
	assert.Equal(t, "test-pod", pod.Name)
	assert.Equal(t, "default", pod.Namespace)
	assert.Equal(t, "value", pod.Annotations["custom-annotation"])
	assert.Equal(t, "test", pod.Labels["app"])
}

func TestSanitizeMetadata_NilAnnotations(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Annotations: nil,
		},
	}

	// Should not panic with nil annotations
	SanitizeMetadata(pod)

	assert.Empty(t, string(pod.UID))
	assert.Nil(t, pod.Annotations)
}

func TestSanitizeMetadata_EmptyAnnotations(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Annotations: map[string]string{},
		},
	}

	SanitizeMetadata(pod)

	assert.Empty(t, string(pod.UID))
	assert.Empty(t, pod.Annotations)
}

func TestSanitizeMetadata_PreservesNonKubectlAnnotations(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Annotations: map[string]string{
				"app.example.com/version":                          "v1",
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
				"another.io/key":                                   "value",
			},
		},
	}

	SanitizeMetadata(pod)

	assert.Len(t, pod.Annotations, 2)
	assert.Equal(t, "v1", pod.Annotations["app.example.com/version"])
	assert.Equal(t, "value", pod.Annotations["another.io/key"])
	assert.NotContains(t, pod.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
}

func TestSanitizeMetadata_ConfigMap(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cm",
			UID:             types.UID("uid-123"),
			ResourceVersion: "456",
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	SanitizeMetadata(cm)

	assert.Empty(t, string(cm.UID))
	assert.Empty(t, cm.ResourceVersion)
	// Data should be preserved
	assert.Equal(t, "value", cm.Data["key"])
}

func TestSanitizeMetadata_Secret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-secret",
			UID:             types.UID("uid-123"),
			ResourceVersion: "456",
		},
		Data: map[string][]byte{
			"password": []byte("secret"),
		},
	}

	SanitizeMetadata(secret)

	assert.Empty(t, string(secret.UID))
	assert.Empty(t, secret.ResourceVersion)
	// Data should be preserved
	assert.Equal(t, []byte("secret"), secret.Data["password"])
}

func TestSanitizeMetadata_PreservesLabels(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Labels: map[string]string{
				"app":  "myapp",
				"tier": "frontend",
			},
		},
	}

	SanitizeMetadata(pod)

	assert.Len(t, pod.Labels, 2)
	assert.Equal(t, "myapp", pod.Labels["app"])
	assert.Equal(t, "frontend", pod.Labels["tier"])
}

// Test combination of functions
func TestIgnoredResourceFlow(t *testing.T) {
	// Create a resource that should be ignored
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "ignored-pod",
			UID:             types.UID("12345"),
			ResourceVersion: "1",
			Labels: map[string]string{
				IgnoreLabel: "true",
			},
		},
	}

	// First check if should be ignored
	if !ShouldIgnoreResource(pod) {
		// Would sanitize if not ignored
		SanitizeMetadata(pod)
	}

	// Since it should be ignored, metadata was not sanitized
	assert.True(t, ShouldIgnoreResource(pod))
	assert.NotEmpty(t, string(pod.UID))
	assert.NotEmpty(t, pod.ResourceVersion)
}

func TestNonIgnoredResourceFlow(t *testing.T) {
	// Create a resource that should NOT be ignored
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "synced-pod",
			UID:             types.UID("12345"),
			ResourceVersion: "1",
			Labels: map[string]string{
				"app": "myapp",
			},
		},
	}

	// First check if should be ignored
	if !ShouldIgnoreResource(pod) {
		// Would sanitize if not ignored
		SanitizeMetadata(pod)
	}

	// Since it was not ignored, metadata was sanitized
	assert.False(t, ShouldIgnoreResource(pod))
	assert.Empty(t, string(pod.UID))
	assert.Empty(t, pod.ResourceVersion)
	// Labels should still exist
	assert.Equal(t, "myapp", pod.Labels["app"])
}
