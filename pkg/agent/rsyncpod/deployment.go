package rsyncpod

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/util/rand"
)

// RsyncDeployment represents an rsync deployment
type RsyncDeployment struct {
	// Name is the name of the deployment
	Name string

	// Namespace is the namespace the deployment is in
	Namespace string

	// client is the Kubernetes client
	client kubernetes.Interface

	// PodName is the name of the generated pod
	PodName string

	// PVCName is the name of the PVC being synced
	PVCName string
}

// CreateRsyncDeployment creates a new rsync deployment
func (m *Manager) CreateRsyncDeployment(ctx context.Context, opts RsyncPodOptions) (*RsyncDeployment, error) {
	// Sanitize PVC name for use in deployment name
	safePVCName := sanitizeNameForLabel(opts.PVCName)
	
	// Generate a unique hash for this sync operation
	syncHash := rand.String(8)
	
	// Generate a deployment name
	deploymentName := fmt.Sprintf("dr-syncer-%s-%s", safePVCName, syncHash)
	
	log.WithFields(logrus.Fields{
		"namespace":        opts.Namespace,
		"pvc_name":         opts.PVCName,
		"node_name":        opts.NodeName,
		"deployment_name":  deploymentName,
		"sync_id":          opts.SyncID,
		"replication_name": opts.ReplicationName,
	}).Info("[DR-SYNC-DETAIL] Creating rsync deployment")

	// Create deployment spec
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: opts.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer-rsync",
				"app.kubernetes.io/instance":   opts.SyncID,
				"app.kubernetes.io/component":  string(opts.Type),
				"app.kubernetes.io/managed-by": "dr-syncer",
				"dr-syncer.io/sync-id":         opts.SyncID,
				"dr-syncer.io/replication":     opts.ReplicationName,
				"dr-syncer.io/pvc-name":        safePVCName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":    "dr-syncer-rsync",
					"app.kubernetes.io/instance": opts.SyncID,
					"dr-syncer.io/pvc-name":     safePVCName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "dr-syncer-rsync",
						"app.kubernetes.io/instance":   opts.SyncID,
						"app.kubernetes.io/component":  string(opts.Type),
						"app.kubernetes.io/managed-by": "dr-syncer",
						"dr-syncer.io/sync-id":         opts.SyncID,
						"dr-syncer.io/replication":     opts.ReplicationName,
						"dr-syncer.io/pvc-name":        safePVCName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "rsync",
							Image: "supporttools/dr-syncer-rsync:latest", // This should be configurable
							Command: []string{
								"/bin/sh",
								"-c",
								"sleep infinity", // Initial command is to wait
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: opts.PVCName,
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}
	
	// Set node selector if node name is provided
	if opts.NodeName != "" {
		deployment.Spec.Template.Spec.NodeName = opts.NodeName
	}
	
	// Check if a deployment with this name already exists and delete it if found
	existingDeployment, err := m.client.AppsV1().Deployments(opts.Namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err == nil {
		// Deployment exists, delete it
		log.WithFields(logrus.Fields{
			"deployment": existingDeployment.Name,
			"namespace":  existingDeployment.Namespace,
		}).Info("[DR-SYNC-DETAIL] Found existing deployment, deleting it")
		
		deletePolicy := metav1.DeletePropagationForeground
		deleteOptions := metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		
		if err := m.client.AppsV1().Deployments(opts.Namespace).Delete(ctx, deploymentName, deleteOptions); err != nil {
			if !errors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to delete existing deployment: %v", err)
			}
		}
		
		// Wait for deletion to complete
		if err := waitForDeploymentDeletion(ctx, m.client, opts.Namespace, deploymentName); err != nil {
			return nil, fmt.Errorf("timeout waiting for deployment deletion: %v", err)
		}
	} else if !errors.IsNotFound(err) {
		// Some error other than "not found"
		return nil, fmt.Errorf("failed to check for existing deployment: %v", err)
	}
	
	// Create the deployment
	createdDeployment, err := m.client.AppsV1().Deployments(opts.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync deployment: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"deployment": createdDeployment.Name,
		"namespace":  createdDeployment.Namespace,
	}).Info("[DR-SYNC-DETAIL] Successfully created rsync deployment")
	
	// Create the RsyncDeployment object
	rsyncDeployment := &RsyncDeployment{
		Name:      createdDeployment.Name,
		Namespace: createdDeployment.Namespace,
		client:    m.client,
		PVCName:   opts.PVCName,
	}
	
	return rsyncDeployment, nil
}

// WaitForDeploymentReady waits for the deployment pod to be ready
func (d *RsyncDeployment) WaitForDeploymentReady(ctx context.Context, timeout time.Duration) error {
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"timeout":    timeout,
	}).Info("[DR-SYNC-DETAIL] Waiting for rsync deployment to be ready")
	
	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	// Poll until the deployment is available or timeout
	var podName string
	err := wait.PollUntilContextCancel(timeoutCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		// Get the deployment
		deployment, err := d.client.AppsV1().Deployments(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"deployment": d.Name,
				"namespace":  d.Namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to get deployment while waiting for ready state")
			return false, nil
		}
		
		// Check if deployment is available
		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
				log.WithFields(logrus.Fields{
					"deployment": d.Name,
					"namespace":  d.Namespace,
				}).Info("[DR-SYNC-DETAIL] Deployment is available, checking for running pod")
				
				// Get the pods for this deployment
				labelSelector := fmt.Sprintf("app.kubernetes.io/name=dr-syncer-rsync,dr-syncer.io/pvc-name=%s", sanitizeNameForLabel(d.PVCName))
				pods, err := d.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				
				if err != nil {
					log.WithFields(logrus.Fields{
						"deployment": d.Name,
						"namespace":  d.Namespace,
						"error":      err,
					}).Warn("[DR-SYNC-DETAIL] Failed to list pods for deployment")
					return false, nil
				}
				
				// Find a running pod
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning {
						podName = pod.Name
						log.WithFields(logrus.Fields{
							"deployment": d.Name,
							"namespace":  d.Namespace,
							"pod":        podName,
						}).Info("[DR-SYNC-DETAIL] Found running pod for deployment")
						return true, nil
					}
				}
				
				log.WithFields(logrus.Fields{
					"deployment": d.Name,
					"namespace":  d.Namespace,
				}).Info("[DR-SYNC-DETAIL] Deployment is available but no running pods found yet")
				return false, nil
			}
		}
		
		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
			"status":     deployment.Status,
		}).Debug("[DR-SYNC-DETAIL] Deployment not ready yet, waiting...")
		return false, nil
	})
	
	if err != nil {
		return fmt.Errorf("timeout waiting for rsync deployment %s/%s to be ready: %v", d.Namespace, d.Name, err)
	}
	
	// Store the pod name
	d.PodName = podName
	
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"pod":        d.PodName,
	}).Info("[DR-SYNC-DETAIL] Rsync deployment is ready with running pod")
	
	return nil
}

// GenerateSSHKeys generates SSH keys in the deployment's pod
func (d *RsyncDeployment) GenerateSSHKeys(ctx context.Context) error {
	if d.PodName == "" {
		return fmt.Errorf("no pod found for deployment, ensure WaitForDeploymentReady was called")
	}
	
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"pod":        d.PodName,
	}).Info("[DR-SYNC-DETAIL] Generating SSH keys in rsync pod")
	
	cmd := []string{
		"sh",
		"-c",
		"mkdir -p /root/.ssh && ssh-keygen -t rsa -N '' -f /root/.ssh/id_rsa",
	}
	
	// Execute command in pod to generate SSH keys
	stdout, stderr, err := executeCommandInPod(ctx, d.client, d.Namespace, d.PodName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":    d.PodName,
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"pod":    d.PodName,
		"stdout": stdout,
	}).Debug("[DR-SYNC-DETAIL] Successfully generated SSH keys")
	
	return nil
}

// GetPublicKey gets the public key from the deployment's pod
func (d *RsyncDeployment) GetPublicKey(ctx context.Context) (string, error) {
	if d.PodName == "" {
		return "", fmt.Errorf("no pod found for deployment, ensure WaitForDeploymentReady was called")
	}
	
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"pod":        d.PodName,
	}).Info("[DR-SYNC-DETAIL] Getting public key from rsync pod")
	
	cmd := []string{
		"cat",
		"/root/.ssh/id_rsa.pub",
	}
	
	// Execute command in pod to get public key
	stdout, stderr, err := executeCommandInPod(ctx, d.client, d.Namespace, d.PodName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":    d.PodName,
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to get public key")
		return "", fmt.Errorf("failed to get public key: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"pod": d.PodName,
	}).Debug("[DR-SYNC-DETAIL] Successfully got public key")
	
	return stdout, nil
}

// Cleanup deletes the deployment
func (d *RsyncDeployment) Cleanup(ctx context.Context, gracePeriodSeconds int64) error {
	log.WithFields(logrus.Fields{
		"deployment":           d.Name,
		"namespace":            d.Namespace,
		"grace_period_seconds": gracePeriodSeconds,
	}).Info("[DR-SYNC-DETAIL] Cleaning up rsync deployment")
	
	deleteOptions := metav1.DeleteOptions{}
	if gracePeriodSeconds >= 0 {
		deleteOptions.GracePeriodSeconds = &gracePeriodSeconds
	}
	
	// Set foreground deletion to ensure pods are deleted
	deletionPropagation := metav1.DeletePropagationForeground
	deleteOptions.PropagationPolicy = &deletionPropagation
	
	err := d.client.AppsV1().Deployments(d.Namespace).Delete(ctx, d.Name, deleteOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.WithFields(logrus.Fields{
				"deployment": d.Name,
				"namespace":  d.Namespace,
				"error":      err,
			}).Error("[DR-SYNC-ERROR] Failed to delete rsync deployment")
			return fmt.Errorf("failed to delete rsync deployment: %v", err)
		}
		// Deployment not found, which is fine
		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
		}).Info("[DR-SYNC-DETAIL] Deployment not found, already deleted")
		return nil
	}
	
	// Wait for deletion to complete
	if err := waitForDeploymentDeletion(ctx, d.client, d.Namespace, d.Name); err != nil {
		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
			"error":      err,
		}).Warn("[DR-SYNC-DETAIL] Timeout waiting for deployment deletion, continuing anyway")
		// We'll continue anyway since we've initiated deletion
	}
	
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
	}).Info("[DR-SYNC-DETAIL] Successfully deleted rsync deployment")
	
	return nil
}

// CleanupExistingDeployments cleans up existing rsync deployments for a PVC
func (m *Manager) CleanupExistingDeployments(ctx context.Context, namespace, pvcName string) error {
	safePVCName := sanitizeNameForLabel(pvcName)
	labelSelector := fmt.Sprintf("app.kubernetes.io/name=dr-syncer-rsync,dr-syncer.io/pvc-name=%s", safePVCName)
	
	log.WithFields(logrus.Fields{
		"namespace":      namespace,
		"pvc_name":       pvcName,
		"label_selector": labelSelector,
	}).Info("[DR-SYNC-DETAIL] Cleaning up existing rsync deployments for PVC")
	
	// List deployments with matching labels
	deployments, err := m.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	
	if err != nil {
		return fmt.Errorf("failed to list existing deployments: %v", err)
	}
	
	if len(deployments.Items) == 0 {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Info("[DR-SYNC-DETAIL] No existing deployments found for PVC")
		return nil
	}
	
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"count":     len(deployments.Items),
	}).Info("[DR-SYNC-DETAIL] Found existing deployments to clean up")
	
	// Set deletion options with foreground propagation
	deletionPropagation := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletionPropagation,
	}
	
	// Delete each deployment
	for _, deployment := range deployments.Items {
		log.WithFields(logrus.Fields{
			"deployment": deployment.Name,
			"namespace":  deployment.Namespace,
		}).Info("[DR-SYNC-DETAIL] Deleting existing deployment")
		
		if err := m.client.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, deleteOptions); err != nil {
			if !errors.IsNotFound(err) {
				log.WithFields(logrus.Fields{
					"deployment": deployment.Name,
					"namespace":  deployment.Namespace,
					"error":      err,
				}).Warn("[DR-SYNC-DETAIL] Failed to delete deployment, continuing with others")
				// Continue with other deployments
			}
		}
		
		// Wait for deletion
		if err := waitForDeploymentDeletion(ctx, m.client, namespace, deployment.Name); err != nil {
			log.WithFields(logrus.Fields{
				"deployment": deployment.Name,
				"namespace":  deployment.Namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Timeout waiting for deployment deletion, continuing anyway")
			// Continue with other deployments
		}
	}
	
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("[DR-SYNC-DETAIL] Finished cleaning up existing deployments for PVC")
	
	return nil
}

// waitForDeploymentDeletion waits for a deployment to be deleted
func waitForDeploymentDeletion(ctx context.Context, client kubernetes.Interface, namespace, name string) error {
	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	
	log.WithFields(logrus.Fields{
		"deployment": name,
		"namespace":  namespace,
	}).Debug("[DR-SYNC-DETAIL] Waiting for deployment deletion")
	
	// Poll until the deployment is gone
	return wait.PollUntilContextCancel(timeoutCtx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// Deployment is gone
			return true, nil
		}
		if err != nil {
			// Some other error
			log.WithFields(logrus.Fields{
				"deployment": name,
				"namespace":  namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Error checking deployment existence, will retry")
		}
		// Deployment still exists
		return false, nil
	})
}

// sanitizeNameForLabel ensures a name is valid for use in a Kubernetes label
func sanitizeNameForLabel(name string) string {
	// Replace characters that aren't allowed in labels
	result := name
	invalidChars := []rune{'/', '.', ':', '~'}
	for _, char := range invalidChars {
		for i := 0; i < len(result); i++ {
			if rune(result[i]) == char {
				if i == 0 {
					result = "-" + result[1:]
				} else if i == len(result)-1 {
					result = result[:i] + "-"
				} else {
					result = result[:i] + "-" + result[i+1:]
				}
			}
		}
	}
	
	// Trim to 63 characters max (Kubernetes label value limit)
	if len(result) > 63 {
		result = result[:63]
	}
	
	return result
}
