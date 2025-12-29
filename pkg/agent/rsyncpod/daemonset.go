package rsyncpod

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/agent/tempod"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	// RsyncDaemonSetName is the name of the rsync DaemonSet
	RsyncDaemonSetName = "dr-syncer-rsync"

	// RsyncDaemonSetLabelKey is the label key for rsync DaemonSet pods
	RsyncDaemonSetLabelKey = "app.kubernetes.io/name"

	// RsyncDaemonSetLabelValue is the label value for rsync DaemonSet pods
	RsyncDaemonSetLabelValue = "dr-syncer-rsync"

	// DefaultRsyncImage is the default rsync container image
	DefaultRsyncImage = "supporttools/dr-syncer-rsync:latest"

	// PlaceholderPodTimeout is the timeout for waiting for placeholder pod to be ready
	PlaceholderPodTimeout = 2 * time.Minute
)

var dsLog = logrus.WithField("component", "rsync-daemonset")

// RsyncDaemonSet manages the rsync DaemonSet on the destination cluster
type RsyncDaemonSet struct {
	// Client is the Kubernetes client for the destination cluster
	Client kubernetes.Interface

	// Namespace is the namespace where the DaemonSet runs
	Namespace string

	// Name is the name of the DaemonSet
	Name string

	// SSHSecretName is the name of the secret containing SSH keys
	SSHSecretName string

	// Image is the rsync container image
	Image string

	// log is the logger for this instance
	log *logrus.Entry
}

// NewRsyncDaemonSet creates a new RsyncDaemonSet instance
func NewRsyncDaemonSet(client kubernetes.Interface, namespace string) *RsyncDaemonSet {
	return &RsyncDaemonSet{
		Client:        client,
		Namespace:     namespace,
		Name:          RsyncDaemonSetName,
		SSHSecretName: "dr-syncer-rsync-ssh-keys",
		Image:         DefaultRsyncImage,
		log: dsLog.WithFields(logrus.Fields{
			"namespace": namespace,
			"daemonset": RsyncDaemonSetName,
		}),
	}
}

// WithSSHSecret sets the SSH secret name
func (d *RsyncDaemonSet) WithSSHSecret(secretName string) *RsyncDaemonSet {
	d.SSHSecretName = secretName
	return d
}

// WithImage sets the rsync container image
func (d *RsyncDaemonSet) WithImage(image string) *RsyncDaemonSet {
	if image != "" {
		d.Image = image
	}
	return d
}

// Deploy creates or updates the rsync DaemonSet
func (d *RsyncDaemonSet) Deploy(ctx context.Context) error {
	d.log.Info("Deploying rsync DaemonSet")

	// Build the DaemonSet spec
	ds := d.buildDaemonSet()

	// Check if DaemonSet already exists
	existing, err := d.Client.AppsV1().DaemonSets(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new DaemonSet
			d.log.Info("Creating new rsync DaemonSet")
			_, err = d.Client.AppsV1().DaemonSets(d.Namespace).Create(ctx, ds, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create rsync DaemonSet: %w", err)
			}
			d.log.Info("Successfully created rsync DaemonSet")
			return nil
		}
		return fmt.Errorf("failed to get rsync DaemonSet: %w", err)
	}

	// Update existing DaemonSet
	d.log.Info("Updating existing rsync DaemonSet")
	ds.ResourceVersion = existing.ResourceVersion
	_, err = d.Client.AppsV1().DaemonSets(d.Namespace).Update(ctx, ds, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update rsync DaemonSet: %w", err)
	}
	d.log.Info("Successfully updated rsync DaemonSet")
	return nil
}

// buildDaemonSet constructs the DaemonSet spec
func (d *RsyncDaemonSet) buildDaemonSet() *appsv1.DaemonSet {
	hostPathType := corev1.HostPathDirectory
	defaultMode := int32(0600)

	labels := map[string]string{
		RsyncDaemonSetLabelKey:        RsyncDaemonSetLabelValue,
		"app.kubernetes.io/component": "rsync-pool",
		"app.kubernetes.io/part-of":   "dr-syncer",
	}

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.Name,
			Namespace: d.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					RsyncDaemonSetLabelKey: RsyncDaemonSetLabelValue,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
					Containers: []corev1.Container{
						{
							Name:    "rsync",
							Image:   d.Image,
							Command: []string{"sleep", "infinity"},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "kubelet",
									MountPath: "/var/lib/kubelet",
									ReadOnly:  false,
								},
								{
									Name:      "ssh-keys",
									MountPath: "/root/.ssh",
									ReadOnly:  true,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: ptr.To(true),
								RunAsUser:  ptr.To(int64(0)),
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("100m"),
									corev1.ResourceMemory: mustParseQuantity("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("2"),
									corev1.ResourceMemory: mustParseQuantity("2Gi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "kubelet",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet",
									Type: &hostPathType,
								},
							},
						},
						{
							Name: "ssh-keys",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  d.SSHSecretName,
									DefaultMode: &defaultMode,
									Optional:    ptr.To(true), // Optional for backward compatibility
								},
							},
						},
					},
					// Allow running on all nodes including masters
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/master",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
		},
	}
}

// FindPodOnNode returns the DaemonSet pod running on a specific node
func (d *RsyncDaemonSet) FindPodOnNode(ctx context.Context, nodeName string) (*corev1.Pod, error) {
	d.log.WithField("node", nodeName).Debug("Finding rsync DaemonSet pod on node")

	// List pods with DaemonSet labels
	pods, err := d.Client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", RsyncDaemonSetLabelKey, RsyncDaemonSetLabelValue),
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list rsync pods: %w", err)
	}

	// Find a running pod on the node
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			d.log.WithFields(logrus.Fields{
				"node": nodeName,
				"pod":  pod.Name,
			}).Debug("Found running rsync DaemonSet pod")
			return pod, nil
		}
	}

	return nil, fmt.Errorf("no running rsync DaemonSet pod found on node %s", nodeName)
}

// ResolveDestinationPath resolves the path to write data for a destination PVC
// It uses a hybrid approach: kubelet path if PVC is mounted, TempPod fallback if not
// Returns: path, cleanup function (may be nil), error
func (d *RsyncDaemonSet) ResolveDestinationPath(ctx context.Context, nodeName, namespace, pvcName string) (string, func(), error) {
	d.log.WithFields(logrus.Fields{
		"node":      nodeName,
		"namespace": namespace,
		"pvc":       pvcName,
	}).Info("Resolving destination path for PVC")

	// Try kubelet path first (fast path - no overhead)
	csiPath, err := tempod.FindCSIPath(ctx, d.Client, namespace, pvcName, nodeName)
	if err == nil {
		d.log.WithFields(logrus.Fields{
			"pvc":      pvcName,
			"csi_path": csiPath,
		}).Info("Found existing CSI path for PVC (fast path)")
		return csiPath, nil, nil // No cleanup needed
	}

	d.log.WithFields(logrus.Fields{
		"pvc":   pvcName,
		"error": err,
	}).Info("CSI path not found, falling back to placeholder pod (slow path)")

	// Fall back to TempPod (slow path - creates placeholder pod)
	placeholderPod, err := tempod.CreatePlaceholderPod(ctx, d.Client, namespace, pvcName, nodeName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create placeholder pod: %w", err)
	}

	// Wait for placeholder pod to be running
	err = tempod.WaitForPlaceholderPod(ctx, d.Client, namespace, placeholderPod.Name, PlaceholderPodTimeout)
	if err != nil {
		// Clean up on failure
		_ = tempod.DeletePlaceholderPod(ctx, d.Client, namespace, placeholderPod.Name)
		return "", nil, fmt.Errorf("placeholder pod failed to start: %w", err)
	}

	// Get the updated pod with UID
	placeholderPod, err = d.Client.CoreV1().Pods(namespace).Get(ctx, placeholderPod.Name, metav1.GetOptions{})
	if err != nil {
		_ = tempod.DeletePlaceholderPod(ctx, d.Client, namespace, placeholderPod.Name)
		return "", nil, fmt.Errorf("failed to get placeholder pod: %w", err)
	}

	// Now find the CSI path for the placeholder pod
	csiPath, err = tempod.FindCSIPath(ctx, d.Client, namespace, pvcName, nodeName)
	if err != nil {
		_ = tempod.DeletePlaceholderPod(ctx, d.Client, namespace, placeholderPod.Name)
		return "", nil, fmt.Errorf("failed to find CSI path after creating placeholder pod: %w", err)
	}

	// Create cleanup function
	cleanup := func() {
		d.log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pod":       placeholderPod.Name,
		}).Info("Cleaning up placeholder pod")
		if err := tempod.DeletePlaceholderPod(context.Background(), d.Client, namespace, placeholderPod.Name); err != nil {
			d.log.WithError(err).Warn("Failed to delete placeholder pod")
		}
	}

	d.log.WithFields(logrus.Fields{
		"pvc":             pvcName,
		"csi_path":        csiPath,
		"placeholder_pod": placeholderPod.Name,
	}).Info("Resolved destination path using placeholder pod")

	return csiPath, cleanup, nil
}

// Delete removes the rsync DaemonSet
func (d *RsyncDaemonSet) Delete(ctx context.Context) error {
	d.log.Info("Deleting rsync DaemonSet")

	err := d.Client.AppsV1().DaemonSets(d.Namespace).Delete(ctx, d.Name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete rsync DaemonSet: %w", err)
	}

	d.log.Info("Successfully deleted rsync DaemonSet")
	return nil
}

// IsReady checks if the DaemonSet has at least one ready pod
func (d *RsyncDaemonSet) IsReady(ctx context.Context) (bool, error) {
	ds, err := d.Client.AppsV1().DaemonSets(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get rsync DaemonSet: %w", err)
	}

	return ds.Status.NumberReady > 0, nil
}

// GetStatus returns the current status of the DaemonSet
func (d *RsyncDaemonSet) GetStatus(ctx context.Context) (*appsv1.DaemonSetStatus, error) {
	ds, err := d.Client.AppsV1().DaemonSets(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get rsync DaemonSet: %w", err)
	}
	return &ds.Status, nil
}

// mustParseQuantity parses a quantity string and panics on error
func mustParseQuantity(s string) resource.Quantity {
	return resource.MustParse(s)
}

// RsyncDaemonSetPod represents an rsync pod from the DaemonSet pool.
// It provides a similar interface to RsyncDeployment but uses existing DaemonSet pods.
type RsyncDaemonSetPod struct {
	// Name is the name of the DaemonSet
	Name string

	// Namespace is the namespace of the DaemonSet
	Namespace string

	// PodName is the name of the DaemonSet pod on the target node
	PodName string

	// PVCName is the name of the PVC being synced
	PVCName string

	// DestNamespace is the namespace of the destination PVC
	DestNamespace string

	// NodeName is the node where this pod runs
	NodeName string

	// DestinationPath is the resolved path for the destination PVC
	DestinationPath string

	// CleanupFunc is called after sync to cleanup any temporary resources (e.g., placeholder pods)
	CleanupFunc func()

	// HasCachedKeys is always true for DaemonSet pods since SSH keys are pre-mounted
	HasCachedKeys bool

	// Client is the Kubernetes client
	Client kubernetes.Interface

	// log is the logger for this instance
	log *logrus.Entry
}

// NewRsyncDaemonSetPod creates a new RsyncDaemonSetPod from an existing DaemonSet pod
func NewRsyncDaemonSetPod(client kubernetes.Interface, pod *corev1.Pod, pvcName, destNamespace, destPath string, cleanup func()) *RsyncDaemonSetPod {
	return &RsyncDaemonSetPod{
		Name:            RsyncDaemonSetName,
		Namespace:       pod.Namespace,
		PodName:         pod.Name,
		PVCName:         pvcName,
		DestNamespace:   destNamespace,
		NodeName:        pod.Spec.NodeName,
		DestinationPath: destPath,
		CleanupFunc:     cleanup,
		HasCachedKeys:   true, // DaemonSet pods always have SSH keys pre-mounted
		Client:          client,
		log: dsLog.WithFields(logrus.Fields{
			"pod":       pod.Name,
			"node":      pod.Spec.NodeName,
			"pvc":       pvcName,
			"namespace": destNamespace,
		}),
	}
}

// Cleanup cleans up any temporary resources created for this sync.
// For DaemonSet pods, this does NOT delete the pod itself (unlike RsyncDeployment).
func (p *RsyncDaemonSetPod) Cleanup(ctx context.Context) error {
	p.log.Info("Cleaning up temporary resources for DaemonSet pod sync")

	if p.CleanupFunc != nil {
		p.CleanupFunc()
	}

	p.log.Info("Cleanup completed")
	return nil
}

// GetDestinationPath returns the resolved destination path for rsync
func (p *RsyncDaemonSetPod) GetDestinationPath() string {
	return p.DestinationPath
}
