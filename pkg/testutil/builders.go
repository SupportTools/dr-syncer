package testutil

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// RemoteClusterBuilder provides a fluent API for building RemoteCluster test objects.
type RemoteClusterBuilder struct {
	rc *drv1alpha1.RemoteCluster
}

// NewRemoteCluster creates a new RemoteClusterBuilder with the given name.
func NewRemoteCluster(name string) *RemoteClusterBuilder {
	return &RemoteClusterBuilder{
		rc: &drv1alpha1.RemoteCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "dr-syncer.io/v1alpha1",
				Kind:       "RemoteCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: drv1alpha1.RemoteClusterSpec{
				KubeconfigSecretRef: drv1alpha1.KubeconfigSecretRef{
					Name:      name + "-kubeconfig",
					Namespace: "default",
				},
			},
		},
	}
}

// WithNamespace sets the namespace.
func (b *RemoteClusterBuilder) WithNamespace(ns string) *RemoteClusterBuilder {
	b.rc.Namespace = ns
	return b
}

// WithKubeconfig sets the kubeconfig secret reference.
func (b *RemoteClusterBuilder) WithKubeconfig(name, namespace string) *RemoteClusterBuilder {
	b.rc.Spec.KubeconfigSecretRef = drv1alpha1.KubeconfigSecretRef{
		Name:      name,
		Namespace: namespace,
	}
	return b
}

// WithKubeconfigKey sets the kubeconfig secret key.
func (b *RemoteClusterBuilder) WithKubeconfigKey(key string) *RemoteClusterBuilder {
	b.rc.Spec.KubeconfigSecretRef.Key = key
	return b
}

// WithSchedule sets the default schedule.
func (b *RemoteClusterBuilder) WithSchedule(schedule string) *RemoteClusterBuilder {
	b.rc.Spec.DefaultSchedule = schedule
	return b
}

// WithResourceTypes sets the default resource types.
func (b *RemoteClusterBuilder) WithResourceTypes(types ...string) *RemoteClusterBuilder {
	b.rc.Spec.DefaultResourceTypes = types
	return b
}

// WithPVCSync enables PVC sync with default settings.
func (b *RemoteClusterBuilder) WithPVCSync(enabled bool) *RemoteClusterBuilder {
	b.rc.Spec.PVCSync = &drv1alpha1.PVCSyncSpec{
		Enabled: enabled,
	}
	return b
}

// WithPVCSyncSSH configures PVC sync SSH settings.
func (b *RemoteClusterBuilder) WithPVCSyncSSH(port int32, secretName, secretNamespace string) *RemoteClusterBuilder {
	if b.rc.Spec.PVCSync == nil {
		b.rc.Spec.PVCSync = &drv1alpha1.PVCSyncSpec{Enabled: true}
	}
	b.rc.Spec.PVCSync.SSH = &drv1alpha1.PVCSyncSSH{
		Port: port,
		KeySecretRef: &drv1alpha1.SSHKeySecretRef{
			Name:      secretName,
			Namespace: secretNamespace,
		},
	}
	return b
}

// WithLabel adds a label.
func (b *RemoteClusterBuilder) WithLabel(key, value string) *RemoteClusterBuilder {
	if b.rc.Labels == nil {
		b.rc.Labels = make(map[string]string)
	}
	b.rc.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *RemoteClusterBuilder) WithAnnotation(key, value string) *RemoteClusterBuilder {
	if b.rc.Annotations == nil {
		b.rc.Annotations = make(map[string]string)
	}
	b.rc.Annotations[key] = value
	return b
}

// WithStatusHealth sets the health status.
func (b *RemoteClusterBuilder) WithStatusHealth(health string) *RemoteClusterBuilder {
	b.rc.Status.Health = health
	return b
}

// Build returns the constructed RemoteCluster.
func (b *RemoteClusterBuilder) Build() *drv1alpha1.RemoteCluster {
	return b.rc
}

// NamespaceMappingBuilder provides a fluent API for building NamespaceMapping test objects.
type NamespaceMappingBuilder struct {
	nm *drv1alpha1.NamespaceMapping
}

// NewNamespaceMapping creates a new NamespaceMappingBuilder with the given name.
func NewNamespaceMapping(name string) *NamespaceMappingBuilder {
	return &NamespaceMappingBuilder{
		nm: &drv1alpha1.NamespaceMapping{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "dr-syncer.io/v1alpha1",
				Kind:       "NamespaceMapping",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: drv1alpha1.NamespaceMappingSpec{
				ReplicationMode: drv1alpha1.ScheduledMode,
			},
		},
	}
}

// WithNamespace sets the resource namespace.
func (b *NamespaceMappingBuilder) WithNamespace(ns string) *NamespaceMappingBuilder {
	b.nm.Namespace = ns
	return b
}

// WithSourceNamespace sets the source namespace to replicate from.
func (b *NamespaceMappingBuilder) WithSourceNamespace(ns string) *NamespaceMappingBuilder {
	b.nm.Spec.SourceNamespace = ns
	return b
}

// WithDestinationNamespace sets the destination namespace to replicate to.
func (b *NamespaceMappingBuilder) WithDestinationNamespace(ns string) *NamespaceMappingBuilder {
	b.nm.Spec.DestinationNamespace = ns
	return b
}

// WithSourceCluster sets the source cluster name.
func (b *NamespaceMappingBuilder) WithSourceCluster(name string) *NamespaceMappingBuilder {
	b.nm.Spec.SourceCluster = name
	return b
}

// WithDestinationCluster sets the destination cluster name.
func (b *NamespaceMappingBuilder) WithDestinationCluster(name string) *NamespaceMappingBuilder {
	b.nm.Spec.DestinationCluster = name
	return b
}

// WithClusterMappingRef sets the cluster mapping reference.
func (b *NamespaceMappingBuilder) WithClusterMappingRef(name string) *NamespaceMappingBuilder {
	b.nm.Spec.ClusterMappingRef = &drv1alpha1.ClusterMappingReference{
		Name: name,
	}
	return b
}

// WithReplicationMode sets the replication mode.
func (b *NamespaceMappingBuilder) WithReplicationMode(mode drv1alpha1.ReplicationMode) *NamespaceMappingBuilder {
	b.nm.Spec.ReplicationMode = mode
	return b
}

// WithSchedule sets the sync schedule.
func (b *NamespaceMappingBuilder) WithSchedule(schedule string) *NamespaceMappingBuilder {
	b.nm.Spec.Schedule = schedule
	return b
}

// WithResourceTypes sets the resource types to replicate.
func (b *NamespaceMappingBuilder) WithResourceTypes(types ...string) *NamespaceMappingBuilder {
	b.nm.Spec.ResourceTypes = types
	return b
}

// WithScaleToZero sets the scale to zero option.
func (b *NamespaceMappingBuilder) WithScaleToZero(scaleToZero bool) *NamespaceMappingBuilder {
	b.nm.Spec.ScaleToZero = &scaleToZero
	return b
}

// WithPaused sets the paused state.
func (b *NamespaceMappingBuilder) WithPaused(paused bool) *NamespaceMappingBuilder {
	b.nm.Spec.Paused = &paused
	return b
}

// WithPVCSync enables PVC sync configuration.
func (b *NamespaceMappingBuilder) WithPVCSync(syncData bool) *NamespaceMappingBuilder {
	b.nm.Spec.PVCConfig = &drv1alpha1.PVCConfig{
		SyncData: syncData,
	}
	return b
}

// WithLabel adds a label.
func (b *NamespaceMappingBuilder) WithLabel(key, value string) *NamespaceMappingBuilder {
	if b.nm.Labels == nil {
		b.nm.Labels = make(map[string]string)
	}
	b.nm.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *NamespaceMappingBuilder) WithAnnotation(key, value string) *NamespaceMappingBuilder {
	if b.nm.Annotations == nil {
		b.nm.Annotations = make(map[string]string)
	}
	b.nm.Annotations[key] = value
	return b
}

// WithStatusPhase sets the status phase.
func (b *NamespaceMappingBuilder) WithStatusPhase(phase drv1alpha1.SyncPhase) *NamespaceMappingBuilder {
	b.nm.Status.Phase = phase
	return b
}

// Build returns the constructed NamespaceMapping.
func (b *NamespaceMappingBuilder) Build() *drv1alpha1.NamespaceMapping {
	return b.nm
}

// ClusterMappingBuilder provides a fluent API for building ClusterMapping test objects.
type ClusterMappingBuilder struct {
	cm *drv1alpha1.ClusterMapping
}

// NewClusterMapping creates a new ClusterMappingBuilder with the given name.
func NewClusterMapping(name string) *ClusterMappingBuilder {
	return &ClusterMappingBuilder{
		cm: &drv1alpha1.ClusterMapping{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "dr-syncer.io/v1alpha1",
				Kind:       "ClusterMapping",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: drv1alpha1.ClusterMappingSpec{},
		},
	}
}

// WithNamespace sets the namespace.
func (b *ClusterMappingBuilder) WithNamespace(ns string) *ClusterMappingBuilder {
	b.cm.Namespace = ns
	return b
}

// WithSourceCluster sets the source cluster name.
func (b *ClusterMappingBuilder) WithSourceCluster(name string) *ClusterMappingBuilder {
	b.cm.Spec.SourceCluster = name
	return b
}

// WithTargetCluster sets the target cluster name.
func (b *ClusterMappingBuilder) WithTargetCluster(name string) *ClusterMappingBuilder {
	b.cm.Spec.TargetCluster = name
	return b
}

// WithClusters sets both source and target clusters.
func (b *ClusterMappingBuilder) WithClusters(source, target string) *ClusterMappingBuilder {
	b.cm.Spec.SourceCluster = source
	b.cm.Spec.TargetCluster = target
	return b
}

// WithSSHKeySecret sets the SSH key secret reference.
func (b *ClusterMappingBuilder) WithSSHKeySecret(name, namespace string) *ClusterMappingBuilder {
	b.cm.Spec.SSHKeySecretRef = &drv1alpha1.ClusterMappingSSHKeySecretRef{
		Name:      name,
		Namespace: namespace,
	}
	return b
}

// WithVerifyConnectivity sets whether to verify connectivity.
func (b *ClusterMappingBuilder) WithVerifyConnectivity(verify bool) *ClusterMappingBuilder {
	b.cm.Spec.VerifyConnectivity = &verify
	return b
}

// WithConnectivityTimeout sets the connectivity timeout.
func (b *ClusterMappingBuilder) WithConnectivityTimeout(seconds int32) *ClusterMappingBuilder {
	b.cm.Spec.ConnectivityTimeoutSeconds = &seconds
	return b
}

// WithPaused sets the paused state.
func (b *ClusterMappingBuilder) WithPaused(paused bool) *ClusterMappingBuilder {
	b.cm.Spec.Paused = &paused
	return b
}

// WithLabel adds a label.
func (b *ClusterMappingBuilder) WithLabel(key, value string) *ClusterMappingBuilder {
	if b.cm.Labels == nil {
		b.cm.Labels = make(map[string]string)
	}
	b.cm.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *ClusterMappingBuilder) WithAnnotation(key, value string) *ClusterMappingBuilder {
	if b.cm.Annotations == nil {
		b.cm.Annotations = make(map[string]string)
	}
	b.cm.Annotations[key] = value
	return b
}

// WithStatusPhase sets the status phase.
func (b *ClusterMappingBuilder) WithStatusPhase(phase drv1alpha1.ClusterMappingPhase) *ClusterMappingBuilder {
	b.cm.Status.Phase = phase
	return b
}

// WithStatusMessage sets the status message.
func (b *ClusterMappingBuilder) WithStatusMessage(message string) *ClusterMappingBuilder {
	b.cm.Status.Message = message
	return b
}

// Build returns the constructed ClusterMapping.
func (b *ClusterMappingBuilder) Build() *drv1alpha1.ClusterMapping {
	return b.cm
}
