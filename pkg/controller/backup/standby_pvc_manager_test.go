package backup

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

func sourcePVCSpec(storageClass string, accessModes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaimSpec {
	spec := &corev1.PersistentVolumeClaimSpec{
		AccessModes: accessModes,
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
		},
	}
	if storageClass != "" {
		spec.StorageClassName = &storageClass
	}
	return spec
}

func TestEnsureStandbyPVC_CreateNew(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	sourcePVC := drv1alpha1.PVCReference{Namespace: "source-ns", Name: "data-pvc"}
	spec := sourcePVCSpec("fast-storage", corev1.ReadWriteOnce)

	ref, created, err := mgr.EnsureStandbyPVC(ctx, sourcePVC, "dr-ns", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("expected PVC to be newly created")
	}
	if ref.Name != "data-pvc-standby" {
		t.Fatalf("expected name 'data-pvc-standby', got %q", ref.Name)
	}
	if ref.Namespace != "dr-ns" {
		t.Fatalf("expected namespace 'dr-ns', got %q", ref.Namespace)
	}

	// Verify the PVC was actually created with correct labels.
	pvc := &corev1.PersistentVolumeClaim{}
	if err := destClient.Get(ctx, types.NamespacedName{Name: "data-pvc-standby", Namespace: "dr-ns"}, pvc); err != nil {
		t.Fatalf("failed to get created PVC: %v", err)
	}

	if pvc.Labels[labelStandby] != "true" {
		t.Errorf("expected label %s=true, got %q", labelStandby, pvc.Labels[labelStandby])
	}
	if pvc.Labels[labelManagedBy] != labelManagedByValue {
		t.Errorf("expected label %s=%s, got %q", labelManagedBy, labelManagedByValue, pvc.Labels[labelManagedBy])
	}
	if pvc.Labels[labelSourcePVC] != "data-pvc" {
		t.Errorf("expected label %s=data-pvc, got %q", labelSourcePVC, pvc.Labels[labelSourcePVC])
	}
	if pvc.Labels[labelSourceNS] != "source-ns" {
		t.Errorf("expected label %s=source-ns, got %q", labelSourceNS, pvc.Labels[labelSourceNS])
	}
	if pvc.Labels[labelMappingRef] != "test-mapping" {
		t.Errorf("expected label %s=test-mapping, got %q", labelMappingRef, pvc.Labels[labelMappingRef])
	}

	// Verify storage class and access modes.
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast-storage" {
		t.Errorf("expected storage class 'fast-storage', got %v", pvc.Spec.StorageClassName)
	}
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("expected access mode ReadWriteOnce, got %v", pvc.Spec.AccessModes)
	}

	// Verify storage size.
	storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("10Gi")) != 0 {
		t.Errorf("expected storage request 10Gi, got %s", storageReq.String())
	}
}

func TestEnsureStandbyPVC_ExistingReturned(t *testing.T) {
	scheme := testScheme()
	existingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:   "true",
				labelManagedBy: labelManagedByValue,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingPVC).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	sourcePVC := drv1alpha1.PVCReference{Namespace: "source-ns", Name: "data-pvc"}

	ref, created, err := mgr.EnsureStandbyPVC(ctx, sourcePVC, "dr-ns", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("expected PVC to not be newly created")
	}
	if ref.Name != "data-pvc-standby" {
		t.Fatalf("expected name 'data-pvc-standby', got %q", ref.Name)
	}
}

func TestEnsureStandbyPVC_NilSpecError(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	sourcePVC := drv1alpha1.PVCReference{Namespace: "source-ns", Name: "data-pvc"}

	_, _, err := mgr.EnsureStandbyPVC(ctx, sourcePVC, "dr-ns", nil)
	if err == nil {
		t.Fatal("expected error when sourcePVCSpec is nil and PVC doesn't exist")
	}
}

func TestUpdateStandbyPVCStatus(t *testing.T) {
	scheme := testScheme()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc-standby",
			Namespace: "dr-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	pvcRef := drv1alpha1.PVCReference{Namespace: "dr-ns", Name: "data-pvc-standby"}

	if err := mgr.UpdateStandbyPVCStatus(ctx, pvcRef, "snap-abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify annotations were set.
	updated := &corev1.PersistentVolumeClaim{}
	if err := destClient.Get(ctx, types.NamespacedName{Name: "data-pvc-standby", Namespace: "dr-ns"}, updated); err != nil {
		t.Fatalf("failed to get updated PVC: %v", err)
	}

	if updated.Annotations[annotationLastRestoreSnapID] != "snap-abc123" {
		t.Errorf("expected snapshot ID annotation 'snap-abc123', got %q", updated.Annotations[annotationLastRestoreSnapID])
	}
	if updated.Annotations[annotationLastRestoreTime] == "" {
		t.Error("expected last restore time annotation to be set")
	}
}

func TestResolveStorageClass_StandbyConfigOverride(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: &drv1alpha1.StandbyPVCConfig{
			StorageClassName: "override-class",
		},
		PVCConfig: &drv1alpha1.PVCConfig{
			StorageClassMappings: []drv1alpha1.StorageClassMapping{
				{From: "source-class", To: "mapped-class"},
			},
		},
	}

	spec := sourcePVCSpec("source-class", corev1.ReadWriteOnce)
	result := mgr.resolveStorageClass(spec)

	if result != "override-class" {
		t.Errorf("expected 'override-class', got %q", result)
	}
}

func TestResolveStorageClass_MappingFallback(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig: &drv1alpha1.PVCConfig{
			StorageClassMappings: []drv1alpha1.StorageClassMapping{
				{From: "source-class", To: "mapped-class"},
			},
		},
	}

	spec := sourcePVCSpec("source-class", corev1.ReadWriteOnce)
	result := mgr.resolveStorageClass(spec)

	if result != "mapped-class" {
		t.Errorf("expected 'mapped-class', got %q", result)
	}
}

func TestResolveStorageClass_SourceFallback(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig:     nil,
	}

	spec := sourcePVCSpec("source-class", corev1.ReadWriteOnce)
	result := mgr.resolveStorageClass(spec)

	if result != "source-class" {
		t.Errorf("expected 'source-class', got %q", result)
	}
}

func TestResolveAccessModes_StandbyConfigOverride(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: &drv1alpha1.StandbyPVCConfig{
			AccessMode: "ReadWriteMany",
		},
		PVCConfig: &drv1alpha1.PVCConfig{
			AccessModeMappings: []drv1alpha1.AccessModeMapping{
				{From: "ReadWriteOnce", To: "ReadOnlyMany"},
			},
		},
	}

	spec := sourcePVCSpec("fast", corev1.ReadWriteOnce)
	result := mgr.resolveAccessModes(spec)

	if len(result) != 1 || result[0] != corev1.ReadWriteMany {
		t.Errorf("expected [ReadWriteMany], got %v", result)
	}
}

func TestResolveAccessModes_MappingFallback(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig: &drv1alpha1.PVCConfig{
			AccessModeMappings: []drv1alpha1.AccessModeMapping{
				{From: "ReadWriteOnce", To: "ReadOnlyMany"},
			},
		},
	}

	spec := sourcePVCSpec("fast", corev1.ReadWriteOnce)
	result := mgr.resolveAccessModes(spec)

	if len(result) != 1 || result[0] != corev1.ReadOnlyMany {
		t.Errorf("expected [ReadOnlyMany], got %v", result)
	}
}

func TestResolveAccessModes_SourceFallback(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig:     nil,
	}

	spec := sourcePVCSpec("fast", corev1.ReadWriteOnce, corev1.ReadOnlyMany)
	result := mgr.resolveAccessModes(spec)

	if len(result) != 2 || result[0] != corev1.ReadWriteOnce || result[1] != corev1.ReadOnlyMany {
		t.Errorf("expected [ReadWriteOnce, ReadOnlyMany], got %v", result)
	}
}

func TestCleanupStandbyPVCs_DeletesUnusedPVCs(t *testing.T) {
	scheme := testScheme()
	pvc1 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc1).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	if err := mgr.CleanupStandbyPVCs(ctx, "dr-ns"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PVC was deleted.
	pvc := &corev1.PersistentVolumeClaim{}
	err := destClient.Get(ctx, types.NamespacedName{Name: "data-pvc-standby", Namespace: "dr-ns"}, pvc)
	if err == nil {
		t.Fatal("expected PVC to be deleted")
	}
}

func TestCleanupStandbyPVCs_SkipsInUsePVCs(t *testing.T) {
	scheme := testScheme()
	pvc1 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mounted-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	mountingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "dr-ns",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "busybox"},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "mounted-pvc-standby",
						},
					},
				},
			},
		},
	}
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc1, mountingPod).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	if err := mgr.CleanupStandbyPVCs(ctx, "dr-ns"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PVC was NOT deleted because it's in use.
	pvc := &corev1.PersistentVolumeClaim{}
	if err := destClient.Get(ctx, types.NamespacedName{Name: "mounted-pvc-standby", Namespace: "dr-ns"}, pvc); err != nil {
		t.Fatalf("expected PVC to still exist (in use): %v", err)
	}
}

func TestListStandbyPVCs_FiltersCorrectly(t *testing.T) {
	scheme := testScheme()
	matchingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	nonMatchingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-pvc",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelManagedBy: labelManagedByValue,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
		},
	}
	differentMappingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-mapping-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "other-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
		},
	}
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(matchingPVC, nonMatchingPVC, differentMappingPVC).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	pvcs, err := mgr.ListStandbyPVCs(ctx, "dr-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pvcs) != 1 {
		t.Fatalf("expected 1 PVC, got %d", len(pvcs))
	}
	if pvcs[0].Name != "data-pvc-standby" {
		t.Errorf("expected PVC 'data-pvc-standby', got %q", pvcs[0].Name)
	}
}

func TestResolveResources_DefaultWhenMissing(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{}

	spec := &corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources:   corev1.VolumeResourceRequirements{},
	}

	result := mgr.resolveResources(spec)
	storageReq := result.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Errorf("expected default 1Gi, got %s", storageReq.String())
	}
}

func TestIsEnabled(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name   string
		config *drv1alpha1.StandbyPVCConfig
		want   bool
	}{
		{
			name:   "nil config defaults to enabled",
			config: nil,
			want:   true,
		},
		{
			name:   "config with nil Enabled defaults to enabled",
			config: &drv1alpha1.StandbyPVCConfig{},
			want:   true,
		},
		{
			name:   "explicitly enabled",
			config: &drv1alpha1.StandbyPVCConfig{Enabled: boolPtr(true)},
			want:   true,
		},
		{
			name:   "explicitly disabled",
			config: &drv1alpha1.StandbyPVCConfig{Enabled: boolPtr(false)},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &StandbyPVCManagerImpl{StandbyConfig: tt.config}
			if got := mgr.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureStandbyPVC_NamingConvention(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	tests := []struct {
		sourceName  string
		wantStandby string
	}{
		{"my-data", "my-data-standby"},
		{"postgres-db", "postgres-db-standby"},
		{"a", "a-standby"},
	}

	for _, tt := range tests {
		t.Run(tt.sourceName, func(t *testing.T) {
			ctx := context.Background()
			sourcePVC := drv1alpha1.PVCReference{Namespace: "source-ns", Name: tt.sourceName}
			spec := sourcePVCSpec("fast", corev1.ReadWriteOnce)

			ref, _, err := mgr.EnsureStandbyPVC(ctx, sourcePVC, "dr-ns", spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Name != tt.wantStandby {
				t.Errorf("got name %q, want %q", ref.Name, tt.wantStandby)
			}
			if !strings.HasSuffix(ref.Name, "-standby") {
				t.Errorf("standby PVC name %q does not end with '-standby'", ref.Name)
			}
		})
	}
}

func TestResolveStorageClass_NilStorageClassName(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig:     nil,
	}

	spec := sourcePVCSpec("", corev1.ReadWriteOnce) // empty storage class → nil StorageClassName
	result := mgr.resolveStorageClass(spec)

	if result != "" {
		t.Errorf("expected empty storage class, got %q", result)
	}
}

func TestResolveStorageClass_MappingNoMatch(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig: &drv1alpha1.PVCConfig{
			StorageClassMappings: []drv1alpha1.StorageClassMapping{
				{From: "gp2", To: "gp3"},
			},
		},
	}

	spec := sourcePVCSpec("ceph-rbd", corev1.ReadWriteOnce)
	result := mgr.resolveStorageClass(spec)

	if result != "ceph-rbd" {
		t.Errorf("expected fallback to source 'ceph-rbd', got %q", result)
	}
}

func TestResolveAccessModes_MappingNoMatch(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{
		StandbyConfig: nil,
		PVCConfig: &drv1alpha1.PVCConfig{
			AccessModeMappings: []drv1alpha1.AccessModeMapping{
				{From: "ReadWriteOnce", To: "ReadOnlyMany"},
			},
		},
	}

	spec := sourcePVCSpec("fast", corev1.ReadWriteMany)
	result := mgr.resolveAccessModes(spec)

	if len(result) != 1 || result[0] != corev1.ReadWriteMany {
		t.Errorf("expected [ReadWriteMany] (unchanged), got %v", result)
	}
}

func TestCleanupStandbyPVCs_EmptyNamespace(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	if err := mgr.CleanupStandbyPVCs(ctx, "empty-ns"); err != nil {
		t.Fatalf("expected no error for empty namespace, got: %v", err)
	}
}

func TestUpdateStandbyPVCStatus_PVCNotFound(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	pvcRef := drv1alpha1.PVCReference{Namespace: "dr-ns", Name: "nonexistent-standby"}

	err := mgr.UpdateStandbyPVCStatus(ctx, pvcRef, "snap-123")
	if err == nil {
		t.Fatal("expected error when PVC not found")
	}
	if !strings.Contains(err.Error(), "get standby PVC") {
		t.Errorf("expected error about getting standby PVC, got: %v", err)
	}
}

func TestResolveResources_CopiesExistingStorage(t *testing.T) {
	mgr := &StandbyPVCManagerImpl{}

	spec := &corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("50Gi"),
			},
		},
	}

	result := mgr.resolveResources(spec)
	storageReq := result.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("50Gi")) != 0 {
		t.Errorf("expected 50Gi, got %s", storageReq.String())
	}
}

func TestEnsureStandbyPVC_CreatedAtAnnotation(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	sourcePVC := drv1alpha1.PVCReference{Namespace: "source-ns", Name: "ts-pvc"}
	spec := sourcePVCSpec("fast", corev1.ReadWriteOnce)

	ref, _, err := mgr.EnsureStandbyPVC(ctx, sourcePVC, "dr-ns", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := destClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, pvc); err != nil {
		t.Fatalf("failed to get PVC: %v", err)
	}

	if pvc.Annotations[annotationCreatedAt] == "" {
		t.Error("expected created-at annotation to be set")
	}
}

func TestNewStandbyPVCManager(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	if mgr.DestClient != destClient {
		t.Error("DestClient not set correctly")
	}
	if mgr.MappingName != "test-mapping" {
		t.Errorf("expected mapping name 'test-mapping', got %q", mgr.MappingName)
	}
	if mgr.Log == nil {
		t.Error("expected non-nil logger")
	}
	if mgr.StandbyConfig != nil {
		t.Error("expected nil StandbyConfig")
	}
	if mgr.PVCConfig != nil {
		t.Error("expected nil PVCConfig")
	}
}

func TestCleanupStandbyPVCs_IsInUseCheckError(t *testing.T) {
	scheme := testScheme()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	// Intercept List calls: fail only for Pod lists (used by isStandbyPVCInUse),
	// allow PVC lists (used by ListStandbyPVCs) to pass through.
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.PodList); ok {
					return fmt.Errorf("simulated pod list error")
				}
				return c.List(ctx, list, opts...)
			},
		}).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	err := mgr.CleanupStandbyPVCs(ctx, "dr-ns")
	// When isStandbyPVCInUse errors, the PVC is skipped (not deleted), no error returned.
	if err != nil {
		t.Fatalf("expected no error (PVC skipped on in-use check failure), got: %v", err)
	}

	// Verify the PVC was NOT deleted (skipped due to in-use check error).
	existing := &corev1.PersistentVolumeClaim{}
	if getErr := destClient.Get(ctx, types.NamespacedName{Name: "data-pvc-standby", Namespace: "dr-ns"}, existing); getErr != nil {
		t.Fatalf("expected PVC to still exist (skipped), but got: %v", getErr)
	}
}

func TestCleanupStandbyPVCs_DeleteError(t *testing.T) {
	scheme := testScheme()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fail-delete-pvc-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return fmt.Errorf("simulated delete error")
			},
		}).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	err := mgr.CleanupStandbyPVCs(ctx, "dr-ns")
	if err == nil {
		t.Fatal("expected error from failed PVC deletion")
	}
	if !strings.Contains(err.Error(), "failed to delete") {
		t.Errorf("expected 'failed to delete' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "fail-delete-pvc-standby") {
		t.Errorf("expected PVC name in error, got: %v", err)
	}
}

func TestCleanupStandbyPVCs_MultipleDeleteErrors(t *testing.T) {
	scheme := testScheme()
	pvc1 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-a-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
		},
	}
	pvc2 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-b-standby",
			Namespace: "dr-ns",
			Labels: map[string]string{
				labelStandby:    "true",
				labelManagedBy:  labelManagedByValue,
				labelMappingRef: "test-mapping",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
		},
	}

	// Track how many delete attempts are made.
	deleteAttempts := 0
	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc1, pvc2).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deleteAttempts++
				return fmt.Errorf("simulated delete error for %s", obj.GetName())
			},
		}).Build()

	mgr := NewStandbyPVCManager(destClient, nil, nil, "test-mapping")

	ctx := context.Background()
	err := mgr.CleanupStandbyPVCs(ctx, "dr-ns")
	if err == nil {
		t.Fatal("expected error from multiple failed PVC deletions")
	}

	// Both PVCs should have been attempted for deletion.
	if deleteAttempts != 2 {
		t.Errorf("expected 2 delete attempts, got %d", deleteAttempts)
	}

	// Error message should report the count of failures.
	if !strings.Contains(err.Error(), "failed to delete 2 standby PVCs") {
		t.Errorf("expected error about 2 failed PVCs, got: %v", err)
	}
}
