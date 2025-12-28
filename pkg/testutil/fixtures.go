package testutil

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeploymentBuilder provides a fluent API for building Deployment test objects.
type DeploymentBuilder struct {
	deploy *appsv1.Deployment
}

// NewDeployment creates a new DeploymentBuilder with the given name.
func NewDeployment(name, namespace string) *DeploymentBuilder {
	replicas := int32(1)
	return &DeploymentBuilder{
		deploy: &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": name,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  name,
								Image: "nginx:latest",
							},
						},
					},
				},
			},
		},
	}
}

// WithReplicas sets the replica count.
func (b *DeploymentBuilder) WithReplicas(replicas int32) *DeploymentBuilder {
	b.deploy.Spec.Replicas = &replicas
	return b
}

// WithImage sets the container image.
func (b *DeploymentBuilder) WithImage(image string) *DeploymentBuilder {
	if len(b.deploy.Spec.Template.Spec.Containers) > 0 {
		b.deploy.Spec.Template.Spec.Containers[0].Image = image
	}
	return b
}

// WithLabel adds a label to the deployment.
func (b *DeploymentBuilder) WithLabel(key, value string) *DeploymentBuilder {
	if b.deploy.Labels == nil {
		b.deploy.Labels = make(map[string]string)
	}
	b.deploy.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *DeploymentBuilder) WithAnnotation(key, value string) *DeploymentBuilder {
	if b.deploy.Annotations == nil {
		b.deploy.Annotations = make(map[string]string)
	}
	b.deploy.Annotations[key] = value
	return b
}

// Build returns the constructed Deployment.
func (b *DeploymentBuilder) Build() *appsv1.Deployment {
	return b.deploy
}

// ConfigMapBuilder provides a fluent API for building ConfigMap test objects.
type ConfigMapBuilder struct {
	cm *corev1.ConfigMap
}

// NewConfigMap creates a new ConfigMapBuilder with the given name.
func NewConfigMap(name, namespace string) *ConfigMapBuilder {
	return &ConfigMapBuilder{
		cm: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Data: make(map[string]string),
		},
	}
}

// WithData adds a data entry.
func (b *ConfigMapBuilder) WithData(key, value string) *ConfigMapBuilder {
	b.cm.Data[key] = value
	return b
}

// WithDataMap sets all data entries.
func (b *ConfigMapBuilder) WithDataMap(data map[string]string) *ConfigMapBuilder {
	b.cm.Data = data
	return b
}

// WithLabel adds a label.
func (b *ConfigMapBuilder) WithLabel(key, value string) *ConfigMapBuilder {
	if b.cm.Labels == nil {
		b.cm.Labels = make(map[string]string)
	}
	b.cm.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *ConfigMapBuilder) WithAnnotation(key, value string) *ConfigMapBuilder {
	if b.cm.Annotations == nil {
		b.cm.Annotations = make(map[string]string)
	}
	b.cm.Annotations[key] = value
	return b
}

// Build returns the constructed ConfigMap.
func (b *ConfigMapBuilder) Build() *corev1.ConfigMap {
	return b.cm
}

// SecretBuilder provides a fluent API for building Secret test objects.
type SecretBuilder struct {
	secret *corev1.Secret
}

// NewSecret creates a new SecretBuilder with the given name.
func NewSecret(name, namespace string) *SecretBuilder {
	return &SecretBuilder{
		secret: &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: make(map[string][]byte),
		},
	}
}

// WithType sets the secret type.
func (b *SecretBuilder) WithType(secretType corev1.SecretType) *SecretBuilder {
	b.secret.Type = secretType
	return b
}

// WithData adds a data entry.
func (b *SecretBuilder) WithData(key string, value []byte) *SecretBuilder {
	b.secret.Data[key] = value
	return b
}

// WithStringData adds a string data entry.
func (b *SecretBuilder) WithStringData(key, value string) *SecretBuilder {
	if b.secret.StringData == nil {
		b.secret.StringData = make(map[string]string)
	}
	b.secret.StringData[key] = value
	return b
}

// WithLabel adds a label.
func (b *SecretBuilder) WithLabel(key, value string) *SecretBuilder {
	if b.secret.Labels == nil {
		b.secret.Labels = make(map[string]string)
	}
	b.secret.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *SecretBuilder) WithAnnotation(key, value string) *SecretBuilder {
	if b.secret.Annotations == nil {
		b.secret.Annotations = make(map[string]string)
	}
	b.secret.Annotations[key] = value
	return b
}

// Build returns the constructed Secret.
func (b *SecretBuilder) Build() *corev1.Secret {
	return b.secret
}

// PVCBuilder provides a fluent API for building PersistentVolumeClaim test objects.
type PVCBuilder struct {
	pvc *corev1.PersistentVolumeClaim
}

// NewPVC creates a new PVCBuilder with the given name.
func NewPVC(name, namespace string) *PVCBuilder {
	return &PVCBuilder{
		pvc: &corev1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "PersistentVolumeClaim",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		},
	}
}

// WithStorageClass sets the storage class.
func (b *PVCBuilder) WithStorageClass(name string) *PVCBuilder {
	b.pvc.Spec.StorageClassName = &name
	return b
}

// WithAccessModes sets the access modes.
func (b *PVCBuilder) WithAccessModes(modes ...corev1.PersistentVolumeAccessMode) *PVCBuilder {
	b.pvc.Spec.AccessModes = modes
	return b
}

// WithStorage sets the storage size.
func (b *PVCBuilder) WithStorage(size string) *PVCBuilder {
	b.pvc.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse(size)
	return b
}

// WithLabel adds a label.
func (b *PVCBuilder) WithLabel(key, value string) *PVCBuilder {
	if b.pvc.Labels == nil {
		b.pvc.Labels = make(map[string]string)
	}
	b.pvc.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *PVCBuilder) WithAnnotation(key, value string) *PVCBuilder {
	if b.pvc.Annotations == nil {
		b.pvc.Annotations = make(map[string]string)
	}
	b.pvc.Annotations[key] = value
	return b
}

// WithVolumeName sets the volume name (binds to a specific PV).
func (b *PVCBuilder) WithVolumeName(name string) *PVCBuilder {
	b.pvc.Spec.VolumeName = name
	return b
}

// Build returns the constructed PersistentVolumeClaim.
func (b *PVCBuilder) Build() *corev1.PersistentVolumeClaim {
	return b.pvc
}

// ServiceBuilder provides a fluent API for building Service test objects.
type ServiceBuilder struct {
	svc *corev1.Service
}

// NewService creates a new ServiceBuilder with the given name.
func NewService(name, namespace string) *ServiceBuilder {
	return &ServiceBuilder{
		svc: &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": name,
				},
				Ports: []corev1.ServicePort{
					{
						Port:     80,
						Protocol: corev1.ProtocolTCP,
					},
				},
			},
		},
	}
}

// WithType sets the service type.
func (b *ServiceBuilder) WithType(svcType corev1.ServiceType) *ServiceBuilder {
	b.svc.Spec.Type = svcType
	return b
}

// WithPort adds a port.
func (b *ServiceBuilder) WithPort(port int32, protocol corev1.Protocol) *ServiceBuilder {
	b.svc.Spec.Ports = append(b.svc.Spec.Ports, corev1.ServicePort{
		Port:     port,
		Protocol: protocol,
	})
	return b
}

// WithSelector sets the selector.
func (b *ServiceBuilder) WithSelector(selector map[string]string) *ServiceBuilder {
	b.svc.Spec.Selector = selector
	return b
}

// WithLabel adds a label.
func (b *ServiceBuilder) WithLabel(key, value string) *ServiceBuilder {
	if b.svc.Labels == nil {
		b.svc.Labels = make(map[string]string)
	}
	b.svc.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *ServiceBuilder) WithAnnotation(key, value string) *ServiceBuilder {
	if b.svc.Annotations == nil {
		b.svc.Annotations = make(map[string]string)
	}
	b.svc.Annotations[key] = value
	return b
}

// Build returns the constructed Service.
func (b *ServiceBuilder) Build() *corev1.Service {
	return b.svc
}

// NamespaceBuilder provides a fluent API for building Namespace test objects.
type NamespaceBuilder struct {
	ns *corev1.Namespace
}

// NewNamespace creates a new NamespaceBuilder with the given name.
func NewNamespace(name string) *NamespaceBuilder {
	return &NamespaceBuilder{
		ns: &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Namespace",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}
}

// WithLabel adds a label.
func (b *NamespaceBuilder) WithLabel(key, value string) *NamespaceBuilder {
	if b.ns.Labels == nil {
		b.ns.Labels = make(map[string]string)
	}
	b.ns.Labels[key] = value
	return b
}

// WithAnnotation adds an annotation.
func (b *NamespaceBuilder) WithAnnotation(key, value string) *NamespaceBuilder {
	if b.ns.Annotations == nil {
		b.ns.Annotations = make(map[string]string)
	}
	b.ns.Annotations[key] = value
	return b
}

// Build returns the constructed Namespace.
func (b *NamespaceBuilder) Build() *corev1.Namespace {
	return b.ns
}
