package deploy

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	agentNamespace = "dr-syncer"
	agentName      = "dr-syncer-agent"
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
	log.Infof("Deploying agent components for remote cluster %s", rc.Name)

	// Create or update namespace
	if err := d.createOrUpdateNamespace(ctx); err != nil {
		return fmt.Errorf("failed to create/update namespace: %v", err)
	}

	// Create or update service account
	if err := d.createOrUpdateServiceAccount(ctx); err != nil {
		return fmt.Errorf("failed to create/update service account: %v", err)
	}

	// Create or update RBAC
	if err := d.createOrUpdateRBAC(ctx); err != nil {
		return fmt.Errorf("failed to create/update RBAC: %v", err)
	}

	// Create or update DaemonSet
	if err := d.createOrUpdateDaemonSet(ctx, rc); err != nil {
		return fmt.Errorf("failed to create/update daemonset: %v", err)
	}

	// Update status with agent information
	if err := d.updateAgentStatus(ctx, rc); err != nil {
		return fmt.Errorf("failed to update agent status: %v", err)
	}

	return nil
}

// Cleanup removes all agent components from the remote cluster
func (d *Deployer) Cleanup(ctx context.Context) error {
	log.Info("Cleaning up agent components from remote cluster")

	// Delete DaemonSet
	if err := d.deleteDaemonSet(ctx); err != nil {
		return fmt.Errorf("failed to delete daemonset: %v", err)
	}

	// Delete RBAC
	if err := d.deleteRBAC(ctx); err != nil {
		return fmt.Errorf("failed to delete RBAC: %v", err)
	}

	// Delete ServiceAccount
	if err := d.deleteServiceAccount(ctx); err != nil {
		return fmt.Errorf("failed to delete service account: %v", err)
	}

	// We don't delete the namespace as it might contain other resources
	// The namespace will be cleaned up by the controller if needed

	return nil
}

// createOrUpdateNamespace creates or updates the agent namespace
func (d *Deployer) createOrUpdateNamespace(ctx context.Context) error {
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

	// Try to get existing namespace
	existing := &corev1.Namespace{}
	err := d.client.Get(ctx, client.ObjectKey{Name: agentNamespace}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		return d.client.Create(ctx, ns)
	}

	// Update existing namespace
	existing.Labels = ns.Labels
	return d.client.Update(ctx, existing)
}

// createOrUpdateServiceAccount creates or updates the agent service account
func (d *Deployer) createOrUpdateServiceAccount(ctx context.Context) error {
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

	// Try to get existing service account
	existing := &corev1.ServiceAccount{}
	err := d.client.Get(ctx, client.ObjectKey{Name: agentName, Namespace: agentNamespace}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		return d.client.Create(ctx, sa)
	}

	// Update existing service account
	existing.Labels = sa.Labels
	return d.client.Update(ctx, existing)
}

// createOrUpdateRBAC creates or updates the agent RBAC resources
func (d *Deployer) createOrUpdateRBAC(ctx context.Context) error {
	// Create or update ClusterRole
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
				Resources: []string{"persistentvolumes", "persistentvolumeclaims", "pods", "nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	// Try to get existing cluster role
	existingCR := &rbacv1.ClusterRole{}
	err := d.client.Get(ctx, client.ObjectKey{Name: agentName}, existingCR)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		if err := d.client.Create(ctx, cr); err != nil {
			return err
		}
	} else {
		// Update existing cluster role
		existingCR.Rules = cr.Rules
		existingCR.Labels = cr.Labels
		if err := d.client.Update(ctx, existingCR); err != nil {
			return err
		}
	}

	// Create or update ClusterRoleBinding
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

	// Try to get existing cluster role binding
	existingCRB := &rbacv1.ClusterRoleBinding{}
	err = d.client.Get(ctx, client.ObjectKey{Name: agentName}, existingCRB)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		return d.client.Create(ctx, crb)
	}

	// Update existing cluster role binding
	existingCRB.Subjects = crb.Subjects
	existingCRB.RoleRef = crb.RoleRef
	existingCRB.Labels = crb.Labels
	return d.client.Update(ctx, existingCRB)
}

// createOrUpdateDaemonSet creates or updates the agent DaemonSet
func (d *Deployer) createOrUpdateDaemonSet(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.Image == nil {
		return fmt.Errorf("PVCSync or Image configuration not found")
	}

	// Set default SSH port if not specified
	sshPort := int32(2222)
	if rc.Spec.PVCSync.SSH != nil && rc.Spec.PVCSync.SSH.Port > 0 {
		sshPort = rc.Spec.PVCSync.SSH.Port
	}

	// Get image repository from environment variable or CRD
	repository := rc.Spec.PVCSync.Image.Repository
	if envRepo := os.Getenv("AGENT_IMAGE_REPOSITORY"); envRepo != "" {
		repository = envRepo
	}

	// Get image tag from environment variable or CRD
	tag := rc.Spec.PVCSync.Image.Tag
	if envTag := os.Getenv("AGENT_IMAGE_TAG"); envTag != "" {
		tag = envTag
	}

	// Set default image pull policy if not specified
	imagePullPolicy := corev1.PullIfNotPresent
	if rc.Spec.PVCSync.Image.PullPolicy != "" {
		imagePullPolicy = corev1.PullPolicy(rc.Spec.PVCSync.Image.PullPolicy)
	}

	// Determine secret name for SSH keys
	secretName := "pvc-syncer-agent-keys"
	if rc.Spec.PVCSync.SSH != nil && rc.Spec.PVCSync.SSH.KeySecretRef != nil {
		secretName = rc.Spec.PVCSync.SSH.KeySecretRef.Name
	}

	// Create base labels and annotations
	labels := map[string]string{
		"app":                          agentName,
		"dr-syncer.io/remote-cluster":  rc.Name,
		"app.kubernetes.io/name":       agentName,
		"app.kubernetes.io/part-of":    "dr-syncer",
		"app.kubernetes.io/managed-by": "dr-syncer-controller",
	}

	// Use a fixed annotation value unless we're explicitly rotating keys
	annotations := map[string]string{
		"dr-syncer.io/ssh-key-version": "1", // Fixed value, only change during key rotation
	}

	// Add custom labels and annotations if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.Labels != nil {
		for k, v := range rc.Spec.PVCSync.Deployment.Labels {
			labels[k] = v
		}
	}

	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.Annotations != nil {
		for k, v := range rc.Spec.PVCSync.Deployment.Annotations {
			annotations[k] = v
		}
	}

	// Determine if host network should be used
	hostNetwork := true
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.HostNetwork != nil {
		hostNetwork = *rc.Spec.PVCSync.Deployment.HostNetwork
	}

	// Create base volumes and volume mounts
	defaultMode := int32(420)                // 0644 in octal
	hostPathType := corev1.HostPathDirectory // Use explicit type instead of nil
	volumes := []corev1.Volume{
		{
			Name: "kubelet",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/lib/kubelet",
					Type: &hostPathType, // Use pointer to explicit type
				},
			},
		},
		{
			Name: "ssh-keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  secretName,
					Optional:    &[]bool{false}[0],
					DefaultMode: &defaultMode, // Explicitly set default mode
				},
			},
		},
	}

	// Add authorized-keys secret volume if it exists
	authorizedKeysSecretName := "dr-syncer-authorized-keys"
	authorizedKeysSecret := &corev1.Secret{}
	authKeysErr := d.client.Get(ctx, client.ObjectKey{Name: authorizedKeysSecretName, Namespace: agentNamespace}, authorizedKeysSecret)
	if authKeysErr == nil {
		// Secret exists, add it as a volume
		volumes = append(volumes, corev1.Volume{
			Name: "authorized-keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  authorizedKeysSecretName,
					Optional:    &[]bool{true}[0], // Make it optional to maintain backward compatibility
					DefaultMode: &defaultMode,
				},
			},
		})
		log.Infof("Found authorized-keys secret, mounting it to the agent pods")
	} else {
		log.Infof("Authorized-keys secret not found, using direct file updates for backward compatibility")
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "kubelet",
			MountPath: "/var/lib/kubelet",
		},
		{
			Name:      "ssh-keys",
			MountPath: "/etc/ssh/keys",
			ReadOnly:  true,
		},
	}

	// Add authorized-keys volume mount if the secret exists
	if authKeysErr == nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "authorized-keys",
			MountPath: "/etc/ssh/keys/authorized_keys",
			SubPath:   "authorized_keys",
			ReadOnly:  true,
		})
	}

	// Create base environment variables
	env := []corev1.EnvVar{
		{
			Name:  "SSH_PORT",
			Value: fmt.Sprintf("%d", sshPort),
		},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	}

	// Add extra environment variables if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.ExtraEnv != nil {
		for _, extraEnv := range rc.Spec.PVCSync.Deployment.ExtraEnv {
			newEnv := corev1.EnvVar{
				Name:  extraEnv.Name,
				Value: extraEnv.Value,
			}

			// Handle ValueFrom if specified
			if extraEnv.ValueFrom != nil && extraEnv.ValueFrom.FieldRef != nil {
				newEnv.ValueFrom = &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: extraEnv.ValueFrom.FieldRef.FieldPath,
					},
				}
			}

			env = append(env, newEnv)
		}
	}

	// Create container security context
	securityContext := &corev1.SecurityContext{
		Privileged: &[]bool{true}[0],
	}

	// Override privileged mode if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.Privileged != nil {
		securityContext.Privileged = rc.Spec.PVCSync.Deployment.Privileged
	}

	// Create liveness and readiness probes
	livenessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(int(sshPort)),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       60,
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(int(sshPort)),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}

	// Create pod spec
	podSpec := corev1.PodSpec{
		ServiceAccountName: agentName,
		HostNetwork:        hostNetwork,
		Containers: []corev1.Container{
			{
				Name:            "agent",
				Image:           fmt.Sprintf("%s:%s", repository, tag),
				ImagePullPolicy: imagePullPolicy,
				SecurityContext: securityContext,
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: sshPort,
						Protocol:      corev1.ProtocolTCP,
					},
				},
				VolumeMounts:   volumeMounts,
				Env:            env,
				LivenessProbe:  livenessProbe,
				ReadinessProbe: readinessProbe,
			},
		},
		Volumes: volumes,
	}

	// Add node selector if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.NodeSelector != nil {
		podSpec.NodeSelector = rc.Spec.PVCSync.Deployment.NodeSelector
	}

	// Add tolerations if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.Tolerations != nil {
		// Convert from our simplified map format to Kubernetes Toleration objects
		var tolerations []corev1.Toleration
		for _, tolMap := range rc.Spec.PVCSync.Deployment.Tolerations {
			tol := corev1.Toleration{}

			if key, ok := tolMap["key"]; ok {
				tol.Key = key
			}

			if operator, ok := tolMap["operator"]; ok {
				tol.Operator = corev1.TolerationOperator(operator)
			}

			if value, ok := tolMap["value"]; ok {
				tol.Value = value
			}

			if effect, ok := tolMap["effect"]; ok {
				tol.Effect = corev1.TaintEffect(effect)
			}

			if seconds, ok := tolMap["tolerationSeconds"]; ok {
				if s, err := strconv.ParseInt(seconds, 10, 64); err == nil {
					tol.TolerationSeconds = &s
				}
			}

			tolerations = append(tolerations, tol)
		}
		podSpec.Tolerations = tolerations
	}

	// Add priority class name if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.PriorityClassName != "" {
		podSpec.PriorityClassName = rc.Spec.PVCSync.Deployment.PriorityClassName
	}

	// Add resource requirements if specified
	if rc.Spec.PVCSync.Deployment != nil && rc.Spec.PVCSync.Deployment.Resources != nil {
		resources := corev1.ResourceRequirements{
			Limits:   make(corev1.ResourceList),
			Requests: make(corev1.ResourceList),
		}

		// Convert limits
		if rc.Spec.PVCSync.Deployment.Resources.Limits != nil {
			for name, quantity := range rc.Spec.PVCSync.Deployment.Resources.Limits {
				resources.Limits[corev1.ResourceName(name)] = resource.MustParse(quantity)
			}
		}

		// Convert requests
		if rc.Spec.PVCSync.Deployment.Resources.Requests != nil {
			for name, quantity := range rc.Spec.PVCSync.Deployment.Resources.Requests {
				resources.Requests[corev1.ResourceName(name)] = resource.MustParse(quantity)
			}
		}

		podSpec.Containers[0].Resources = resources
	}

	// Create DaemonSet object
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentName,
			Namespace: agentNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": agentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: podSpec,
			},
		},
	}

	// Try to get existing DaemonSet
	existing := &appsv1.DaemonSet{}
	err := d.client.Get(ctx, client.ObjectKey{Name: agentName, Namespace: agentNamespace}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Not found, create it
		log.Infof("Creating new DaemonSet %s in namespace %s", agentName, agentNamespace)
		return d.client.Create(ctx, ds)
	}

	// Check if update is needed by comparing relevant fields
	needsUpdate := false

	// Compare container image
	if len(existing.Spec.Template.Spec.Containers) != len(ds.Spec.Template.Spec.Containers) {
		needsUpdate = true
		log.Infof("Container count changed, updating DaemonSet")
	} else if len(existing.Spec.Template.Spec.Containers) > 0 && len(ds.Spec.Template.Spec.Containers) > 0 {
		existingImage := existing.Spec.Template.Spec.Containers[0].Image
		newImage := ds.Spec.Template.Spec.Containers[0].Image
		if existingImage != newImage {
			needsUpdate = true
			log.Infof("Container image changed from %s to %s, updating DaemonSet", existingImage, newImage)
		}
	}

	// Compare annotations (especially for SSH key version)
	if !reflect.DeepEqual(existing.Spec.Template.Annotations, ds.Spec.Template.Annotations) {
		needsUpdate = true
		log.Infof("Annotations changed, updating DaemonSet")

		// Debug output for annotations differences
		log.Infof("Existing annotations: %+v", existing.Spec.Template.Annotations)
		log.Infof("New annotations: %+v", ds.Spec.Template.Annotations)

		// Show specific differences
		for k, v := range ds.Spec.Template.Annotations {
			if existingVal, ok := existing.Spec.Template.Annotations[k]; !ok {
				log.Infof("Added annotation: %s=%s", k, v)
			} else if existingVal != v {
				log.Infof("Changed annotation: %s from %s to %s", k, existingVal, v)
			}
		}

		for k, v := range existing.Spec.Template.Annotations {
			if _, ok := ds.Spec.Template.Annotations[k]; !ok {
				log.Infof("Removed annotation: %s=%s", k, v)
			}
		}
	}

	// Compare volumes
	if !reflect.DeepEqual(existing.Spec.Template.Spec.Volumes, ds.Spec.Template.Spec.Volumes) {
		needsUpdate = true
		log.Infof("Volumes changed, updating DaemonSet")

		// Print volume details for debugging
		log.Infof("Number of volumes: existing=%d, new=%d",
			len(existing.Spec.Template.Spec.Volumes), len(ds.Spec.Template.Spec.Volumes))

		// Compare volumes by name
		existingVolumes := make(map[string]corev1.Volume)
		for _, vol := range existing.Spec.Template.Spec.Volumes {
			existingVolumes[vol.Name] = vol
		}

		newVolumes := make(map[string]corev1.Volume)
		for _, vol := range ds.Spec.Template.Spec.Volumes {
			newVolumes[vol.Name] = vol
		}

		// Check for added or changed volumes
		for name, vol := range newVolumes {
			if existingVol, ok := existingVolumes[name]; !ok {
				log.Infof("Added volume: %s", name)
			} else if !reflect.DeepEqual(existingVol, vol) {
				log.Infof("Changed volume: %s", name)
				log.Infof("  Existing: %+v", existingVol)
				log.Infof("  New: %+v", vol)
			}
		}

		// Check for removed volumes
		for name := range existingVolumes {
			if _, ok := newVolumes[name]; !ok {
				log.Infof("Removed volume: %s", name)
			}
		}
	}

	// Compare environment variables
	if len(existing.Spec.Template.Spec.Containers) > 0 && len(ds.Spec.Template.Spec.Containers) > 0 {
		existingEnv := existing.Spec.Template.Spec.Containers[0].Env
		newEnv := ds.Spec.Template.Spec.Containers[0].Env

		// Convert environment variables to comparable format
		existingEnvMap := convertEnvToMap(existingEnv)
		newEnvMap := convertEnvToMap(newEnv)

		// Compare the maps instead of using reflect.DeepEqual
		if !areEnvMapsEqual(existingEnvMap, newEnvMap) {
			needsUpdate = true
			log.Infof("Environment variables changed, updating DaemonSet")

			// Check for added or changed env vars
			for name, value := range newEnvMap {
				if existingValue, ok := existingEnvMap[name]; !ok {
					log.Infof("Added env var: %s=%s", name, value)
				} else if existingValue != value {
					log.Infof("Changed env var: %s from %s to %s", name, existingValue, value)
				}
			}

			// Check for removed env vars
			for name := range existingEnvMap {
				if _, ok := newEnvMap[name]; !ok {
					log.Infof("Removed env var: %s", name)
				}
			}

			// Also check for ValueFrom differences
			log.Infof("Number of env vars: existing=%d, new=%d", len(existingEnv), len(newEnv))
			if len(existingEnv) != len(newEnv) {
				log.Infof("Different number of environment variables")
			}
		} else {
			log.Infof("Environment variables content is identical, no update needed")
		}
	}

	// Only update if needed
	if needsUpdate {
		log.Infof("Updating DaemonSet %s in namespace %s", agentName, agentNamespace)
		existing.Spec = ds.Spec
		existing.Labels = ds.Labels
		return d.client.Update(ctx, existing)
	} else {
		log.Infof("DaemonSet %s in namespace %s is already up to date, skipping update", agentName, agentNamespace)
		return nil
	}
}

// updateAgentStatus updates the agent status in the RemoteCluster status
func (d *Deployer) updateAgentStatus(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Get DaemonSet to check status
	ds := &appsv1.DaemonSet{}
	err := d.client.Get(ctx, client.ObjectKey{Name: agentName, Namespace: agentNamespace}, ds)
	if err != nil {
		return client.IgnoreNotFound(err)
	}

	// Initialize PVC sync status if needed
	if rc.Status.PVCSync == nil {
		rc.Status.PVCSync = &drv1alpha1.PVCSyncStatus{
			Phase: "Deploying",
		}
	}

	// Initialize agent status if needed
	if rc.Status.PVCSync.AgentStatus == nil {
		rc.Status.PVCSync.AgentStatus = &drv1alpha1.PVCSyncAgentStatus{
			NodeStatuses: make(map[string]drv1alpha1.PVCSyncNodeStatus),
		}
	}

	// Update agent status
	rc.Status.PVCSync.AgentStatus.TotalNodes = ds.Status.DesiredNumberScheduled
	rc.Status.PVCSync.AgentStatus.ReadyNodes = ds.Status.NumberReady

	// Update phase based on agent status
	if ds.Status.NumberReady == 0 {
		rc.Status.PVCSync.Phase = "Deploying"
	} else if ds.Status.NumberReady < ds.Status.DesiredNumberScheduled {
		rc.Status.PVCSync.Phase = "PartiallyReady"
	} else {
		rc.Status.PVCSync.Phase = "Ready"
	}

	return nil
}

// deleteDaemonSet deletes the agent DaemonSet
func (d *Deployer) deleteDaemonSet(ctx context.Context) error {
	log.Infof("Deleting DaemonSet %s in namespace %s", agentName, agentNamespace)
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentName,
			Namespace: agentNamespace,
		},
	}
	return client.IgnoreNotFound(d.client.Delete(ctx, ds))
}

// deleteRBAC deletes the agent RBAC resources
func (d *Deployer) deleteRBAC(ctx context.Context) error {
	log.Info("Deleting RBAC resources")
	// Delete ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentName,
		},
	}
	if err := client.IgnoreNotFound(d.client.Delete(ctx, crb)); err != nil {
		return err
	}

	// Delete ClusterRole
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentName,
		},
	}
	return client.IgnoreNotFound(d.client.Delete(ctx, cr))
}

// deleteServiceAccount deletes the agent service account
func (d *Deployer) deleteServiceAccount(ctx context.Context) error {
	log.Infof("Deleting ServiceAccount %s in namespace %s", agentName, agentNamespace)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentName,
			Namespace: agentNamespace,
		},
	}
	return client.IgnoreNotFound(d.client.Delete(ctx, sa))
}

// convertEnvToMap converts a slice of environment variables to a map for easier comparison
// It handles both simple env vars with direct values and those with ValueFrom references
func convertEnvToMap(envVars []corev1.EnvVar) map[string]string {
	result := make(map[string]string)

	for _, env := range envVars {
		if env.Value != "" {
			// For simple env vars with direct values
			result[env.Name] = env.Value
		} else if env.ValueFrom != nil {
			// For env vars with ValueFrom, create a string representation
			if env.ValueFrom.FieldRef != nil {
				result[env.Name] = fmt.Sprintf("fieldRef:%s", env.ValueFrom.FieldRef.FieldPath)
			} else if env.ValueFrom.ResourceFieldRef != nil {
				result[env.Name] = fmt.Sprintf("resourceFieldRef:%s", env.ValueFrom.ResourceFieldRef.Resource)
			} else if env.ValueFrom.ConfigMapKeyRef != nil {
				result[env.Name] = fmt.Sprintf("configMapKeyRef:%s:%s",
					env.ValueFrom.ConfigMapKeyRef.Name, env.ValueFrom.ConfigMapKeyRef.Key)
			} else if env.ValueFrom.SecretKeyRef != nil {
				result[env.Name] = fmt.Sprintf("secretKeyRef:%s:%s",
					env.ValueFrom.SecretKeyRef.Name, env.ValueFrom.SecretKeyRef.Key)
			}
		}
	}

	return result
}

// areEnvMapsEqual compares two environment variable maps for equality
func areEnvMapsEqual(map1, map2 map[string]string) bool {
	if len(map1) != len(map2) {
		return false
	}

	for key, value1 := range map1 {
		if value2, ok := map2[key]; !ok || value1 != value2 {
			return false
		}
	}

	return true
}
