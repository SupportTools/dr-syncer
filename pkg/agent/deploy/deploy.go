package deploy

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	agentNamespace = "dr-syncer-agent"
	agentName      = "pvc-syncer-agent"
)

// Deployer handles agent deployment in remote clusters
type Deployer struct {
	client client.Client
}

// NewDeployer creates a new agent deployer
func NewDeployer(client client.Client) *Deployer {
	return &Deployer{
		client: client,
	}
}

// Deploy deploys the agent components to the remote cluster
func (d *Deployer) Deploy(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if err := d.createNamespace(ctx); err != nil {
		return fmt.Errorf("failed to create namespace: %v", err)
	}

	if err := d.createServiceAccount(ctx); err != nil {
		return fmt.Errorf("failed to create service account: %v", err)
	}

	if err := d.createRBAC(ctx); err != nil {
		return fmt.Errorf("failed to create RBAC: %v", err)
	}

	if err := d.createDaemonSet(ctx, rc); err != nil {
		return fmt.Errorf("failed to create daemonset: %v", err)
	}

	return nil
}

// createNamespace creates the agent namespace
func (d *Deployer) createNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       agentName,
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
	}

	return d.client.Create(ctx, ns)
}

// createServiceAccount creates the agent service account
func (d *Deployer) createServiceAccount(ctx context.Context) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentName,
			Namespace: agentNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       agentName,
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
	}

	return d.client.Create(ctx, sa)
}

// createRBAC creates the agent RBAC resources
func (d *Deployer) createRBAC(ctx context.Context) error {
	// Create ClusterRole
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       agentName,
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes", "persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	if err := d.client.Create(ctx, cr); err != nil {
		return err
	}

	// Create ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       agentName,
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      agentName,
				Namespace: agentNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     agentName,
		},
	}

	return d.client.Create(ctx, crb)
}

// createDaemonSet creates the agent DaemonSet
func (d *Deployer) createDaemonSet(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.Image == nil {
		return fmt.Errorf("PVCSync or Image configuration not found")
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentName,
			Namespace: agentNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       agentName,
				"app.kubernetes.io/part-of":    "dr-syncer",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": agentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": agentName,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: agentName,
					HostNetwork:        true,
					Containers: []corev1.Container{
						{
							Name:            "agent",
							Image:           fmt.Sprintf("%s:%s", rc.Spec.PVCSync.Image.Repository, rc.Spec.PVCSync.Image.Tag),
							ImagePullPolicy: corev1.PullPolicy(rc.Spec.PVCSync.Image.PullPolicy),
							SecurityContext: &corev1.SecurityContext{
								Privileged: &[]bool{true}[0],
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: int32(rc.Spec.PVCSync.SSH.Port),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "kubelet",
									MountPath: "/var/lib/kubelet",
								},
								{
									Name:      "ssh-keys",
									MountPath: "/etc/ssh/keys",
									ReadOnly:  true,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(int(rc.Spec.PVCSync.SSH.Port)),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       60,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(int(rc.Spec.PVCSync.SSH.Port)),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "kubelet",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet",
								},
							},
						},
						{
							Name: "ssh-keys",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: fmt.Sprintf("%s-keys", agentName),
									Optional:   &[]bool{false}[0],
								},
							},
						},
					},
				},
			},
		},
	}

	return d.client.Create(ctx, ds)
}
