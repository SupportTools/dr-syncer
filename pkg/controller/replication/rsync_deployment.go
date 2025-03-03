package replication

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

	"github.com/supporttools/dr-syncer/pkg/logging"
)

// CreateRsyncDeployment creates a new rsync deployment that starts in a waiting state
func (p *PVCSyncer) CreateRsyncDeployment(ctx context.Context, namespace, pvcName string) (*appsv1.Deployment, *corev1.Pod, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info(logging.LogTagDetail + " Creating rsync deployment for PVC replication")

	// Generate a unique name for the deployment
	deploymentName := fmt.Sprintf("dr-syncer-rsync-%s", pvcName)

	// Check if deployment already exists
	existingDeployment, err := p.DestinationK8sClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err == nil {
		log.WithFields(logrus.Fields{
			"namespace":  namespace,
			"pvc_name":   pvcName,
			"deployment": deploymentName,
		}).Info(logging.LogTagDetail + " Rsync deployment already exists, reusing")
		
		// Get the pod associated with the deployment
		pods, err := p.DestinationK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/name=dr-syncer-rsync,dr-syncer.io/pvc-name=%s", pvcName),
		})
		if err != nil || len(pods.Items) == 0 {
			return existingDeployment, nil, fmt.Errorf("failed to find pod for existing deployment: %v", err)
		}
		
		return existingDeployment, &pods.Items[0], nil
	}

	if !errors.IsNotFound(err) {
		return nil, nil, fmt.Errorf("failed to check for existing deployment: %v", err)
	}

	// Define deployment spec
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name": "dr-syncer-rsync",
				"app.kubernetes.io/part-of": "dr-syncer",
				"dr-syncer.io/pvc-name": pvcName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": "dr-syncer-rsync",
					"dr-syncer.io/pvc-name": pvcName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name": "dr-syncer-rsync",
						"app.kubernetes.io/part-of": "dr-syncer",
						"dr-syncer.io/pvc-name": pvcName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "rsync",
							Image: "supporttools/dr-syncer-rsync:latest",
							Command: []string{
								"sleep", // Start in waiting state
								"infinity",
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
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}

	// Create the deployment
	createdDeployment, err := p.DestinationK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create rsync deployment: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"pvc_name":   pvcName,
		"deployment": deploymentName,
	}).Info(logging.LogTagDetail + " Rsync deployment created, waiting for pod to be ready")

	// Wait for the pod to be created and running
	var rsyncPod *corev1.Pod
	err = wait.PollImmediate(2*time.Second, 5*time.Minute, func() (bool, error) {
		// List pods for this deployment
		pods, err := p.DestinationK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/name=dr-syncer-rsync,dr-syncer.io/pvc-name=%s", pvcName),
		})
		if err != nil || len(pods.Items) == 0 {
			return false, nil // Keep polling
		}

		// Check if pod is running
		pod := pods.Items[0]
		if pod.Status.Phase == corev1.PodRunning {
			rsyncPod = &pod
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("timed out waiting for rsync pod to be ready: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"pvc_name":   pvcName,
		"deployment": deploymentName,
		"pod_name":   rsyncPod.Name,
	}).Info(logging.LogTagDetail + " Rsync pod is ready")

	return createdDeployment, rsyncPod, nil
}

// CleanupRsyncDeployment deletes the rsync deployment
func (p *PVCSyncer) CleanupRsyncDeployment(ctx context.Context, namespace, pvcName string) error {
	deploymentName := fmt.Sprintf("dr-syncer-rsync-%s", pvcName)

	log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"pvc_name":   pvcName,
		"deployment": deploymentName,
	}).Info(logging.LogTagDetail + " Cleaning up rsync deployment")

	// Use foreground deletion so pods are cleaned up first
	deletionPropagation := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletionPropagation,
	}

	err := p.DestinationK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deploymentName, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete rsync deployment: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"pvc_name":   pvcName,
		"deployment": deploymentName,
	}).Info(logging.LogTagDetail + " Rsync deployment cleanup completed")

	return nil
}

// WaitForPodReady waits for a pod to be ready
func (p *PVCSyncer) WaitForPodReady(ctx context.Context, namespace, podName string) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod_name":  podName,
	}).Info(logging.LogTagDetail + " Waiting for pod to be ready")

	return wait.PollImmediate(2*time.Second, 3*time.Minute, func() (bool, error) {
		pod, err := p.DestinationK8sClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil // Keep polling
			}
			return false, err
		}

		// Check if pod is running and ready
		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}

		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})
}
