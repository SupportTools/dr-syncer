---
sidebar_position: 9
---

# Security Considerations

DR-Syncer is designed with security as a core principle, especially when handling sensitive operations like cross-cluster data replication. This document outlines the security architecture, best practices, and considerations when deploying DR-Syncer in your environment.

## Security Architecture

### Authentication and Authorization

DR-Syncer follows Kubernetes security best practices for authentication and authorization:

#### Cluster Authentication

- **Kubeconfig-based Authentication**: DR-Syncer uses kubeconfig files stored as Kubernetes secrets to authenticate with remote clusters:
  ```yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: dr-cluster-kubeconfig
    namespace: dr-syncer-system
  type: Opaque
  data:
    kubeconfig: <base64-encoded-kubeconfig>
  ```

- **Service Account Usage**: Best practice is to use dedicated service accounts with minimal permissions:
  ```yaml
  # In source cluster
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: dr-syncer-controller
    namespace: dr-syncer-system
  ---
  # In destination cluster
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: dr-syncer-remote
    namespace: dr-syncer-system
  ```

#### RBAC Configuration

DR-Syncer requires specific RBAC permissions to function:

- **Controller Permissions**: Needs read access to source resources and read/write access to the DR-Syncer custom resources:
  ```yaml
  kind: ClusterRole
  apiVersion: rbac.authorization.k8s.io/v1
  metadata:
    name: dr-syncer-controller
  rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["dr-syncer.io"]
    resources: ["remoteclusters", "replications"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  ```

- **Remote Cluster Permissions**: Needs the ability to create and manage resources in destination namespaces:
  ```yaml
  kind: ClusterRole
  apiVersion: rbac.authorization.k8s.io/v1
  metadata:
    name: dr-syncer-remote
  rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list", "watch", "create"]
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  ```

- **Least Privilege Principle**: It's recommended to restrict permissions to only the specific resource types and namespaces needed:
  ```yaml
  # More restrictive example for production use
  kind: Role
  apiVersion: rbac.authorization.k8s.io/v1
  metadata:
    name: dr-syncer-remote-ns
    namespace: production-dr
  rules:
  - apiGroups: [""]
    resources: ["configmaps", "secrets", "services"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  ```

## PVC Data Replication Security

DR-Syncer implements a secure architecture for PVC data replication:

### SSH Key Management

- **Secure Key Generation**: Keys are generated with proper permissions and constraints:
  ```go
  // Simplified key generation logic
  func generateSSHKey() (privateKey, publicKey []byte, err error) {
      // Generate private key
      key, err := rsa.GenerateKey(rand.Reader, 4096)
      if err != nil {
          return nil, nil, err
      }

      // Marshal private key to PEM
      privateKeyPEM := &pem.Block{
          Type:  "RSA PRIVATE KEY",
          Bytes: x509.MarshalPKCS1PrivateKey(key),
      }
      privateKey = pem.EncodeToMemory(privateKeyPEM)

      // Generate public key
      publicRsaKey, err := ssh.NewPublicKey(&key.PublicKey)
      if err != nil {
          return nil, nil, err
      }
      publicKey = ssh.MarshalAuthorizedKey(publicRsaKey)

      return privateKey, publicKey, nil
  }
  ```

- **Key Storage as Kubernetes Secrets**: SSH keys are stored securely in Kubernetes secrets:
  ```yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: dr-cluster-ssh-key
    namespace: dr-syncer-system
  type: Opaque
  data:
    id_rsa: <base64-encoded-private-key>
    id_rsa.pub: <base64-encoded-public-key>
  ```

- **Key Rotation Strategy**: Regular key rotation is supported:
  ```go
  // Simplified key rotation logic
  func rotateSSHKey(ctx context.Context, cluster *drv1alpha1.RemoteCluster) error {
      // Generate new key pair
      privateKey, publicKey, err := generateSSHKey()
      if err != nil {
          return err
      }
      
      // Update secret with new keys
      secret := &corev1.Secret{}
      secret.Name = cluster.Spec.SSHKeySecret
      secret.Namespace = cluster.Namespace
      secret.Data = map[string][]byte{
          "id_rsa":     privateKey,
          "id_rsa.pub": publicKey,
      }
      
      // Apply the updated secret
      if err := c.Update(ctx, secret); err != nil {
          return err
      }
      
      // Update agent with new public key
      return updateAgentAuthorizedKeys(ctx, cluster, publicKey)
  }
  ```

### Command Restriction

DR-Syncer restricts SSH commands to only the necessary operations:

- **Authorized Keys Configuration**: Uses OpenSSH's command restriction feature:
  ```
  # Example authorized_keys entry
  command="rsync --server -vlogDtprze.iLsfxC . /mnt/data",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty ssh-rsa AAAA...
  ```

- **Limited Command Set**: Only specific rsync commands are allowed, preventing arbitrary command execution:
  ```go
  // Simplified authorized_keys generation
  func generateAuthorizedKeysContent(publicKey string, allowedPaths []string) string {
      var entries []string
      for _, path := range allowedPaths {
          entry := fmt.Sprintf(
              `command="rsync --server -vlogDtprze.iLsfxC . %s",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty %s`,
              path, publicKey)
          entries = append(entries, entry)
      }
      return strings.Join(entries, "\n")
  }
  ```

### Agent Security Model

The agent runs with minimal privileges and provides secure data access:

- **Non-Root Execution**: Agent runs as non-root user with minimal permissions:
  ```dockerfile
  # From Dockerfile.agent
  RUN adduser -D -u 1000 drsyncer
  USER drsyncer
  ```

- **Direct PVC Access**: Agent has direct access to mounted PVCs without requiring privileged access:
  ```yaml
  # Agent DaemonSet with volume mounts
  spec:
    template:
      spec:
        volumes:
        - name: host-volumes
          hostPath:
            path: /var/lib/kubelet/pods
        volumeMounts:
        - name: host-volumes
          mountPath: /mnt/data
          readOnly: false
  ```

- **SSH Server Configuration**: Hardened SSH configuration with proper restrictions:
  ```
  # sshd_config
  Protocol 2
  HostKey /etc/ssh/ssh_host_rsa_key
  PermitRootLogin no
  PasswordAuthentication no
  ChallengeResponseAuthentication no
  UsePAM no
  X11Forwarding no
  AllowTcpForwarding no
  PermitTunnel no
  Port 2222
  ```

- **Comprehensive Logging**: Detailed logging of all operations for audit purposes:
  ```go
  log.WithFields(log.Fields{
      "remote_addr": c.RemoteAddr().String(),
      "command":     cmd,
      "user":        c.User(),
  }).Info("SSH command execution")
  ```

## Network Security

DR-Syncer implements network security best practices:

### Secure Communication

- **TLS for API Connections**: All Kubernetes API connections use TLS:
  ```go
  // Simplified client creation with TLS
  config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
  if err != nil {
      return nil, err
  }
  
  // Configure TLS
  config.TLSClientConfig = rest.TLSClientConfig{
      CAData:   caData,
      CertData: certData,
      KeyData:  keyData,
  }
  
  client, err := client.New(config, client.Options{})
  if err != nil {
      return nil, err
  }
  ```

- **SSH Encryption for Data Transfer**: PVC data is transferred using SSH-encrypted rsync:
  ```bash
  rsync -avz --delete -e "ssh -p 2222 -i /path/to/key -o StrictHostKeyChecking=no" /source/path/ user@agent-host:/destination/path/
  ```

### Network Policies

Recommended network policies to secure DR-Syncer communication:

- **Controller Network Policy**: Restrict controller pod communication:
  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: NetworkPolicy
  metadata:
    name: dr-syncer-controller
    namespace: dr-syncer-system
  spec:
    podSelector:
      matchLabels:
        app: dr-syncer-controller
    policyTypes:
    - Ingress
    - Egress
    ingress:
    - from:
      - podSelector:
          matchLabels:
            app: prometheus
      ports:
      - port: 8080  # Metrics port
    egress:
    - to:
      - ipBlock:
          cidr: 0.0.0.0/0
      ports:
      - port: 443  # Kubernetes API
      - port: 2222  # SSH port for agent
  ```

- **Agent Network Policy**: Restrict agent pod communication:
  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: NetworkPolicy
  metadata:
    name: dr-syncer-agent
    namespace: dr-syncer-system
  spec:
    podSelector:
      matchLabels:
        app: dr-syncer-agent
    policyTypes:
    - Ingress
    - Egress
    ingress:
    - from:
      - ipBlock:
          cidr: 10.0.0.0/8  # Controller cluster CIDR
      ports:
      - port: 2222  # SSH port
    egress: []  # No outbound connections needed
  ```

## Secret Management

DR-Syncer handles various secrets securely:

### Secret Handling

- **Resource Synchronization**: When synchronizing Secret resources, DR-Syncer maintains their confidentiality:
  ```go
  // Simplified secret synchronization
  if resource.GetKind() == "Secret" {
      // Apply special handling for Secret resources
      log.Info("Synchronizing Secret resource", "name", resource.GetName())
      
      // Ensure sensitive logs are not generated
      log.Debug("Processing Secret (content not logged)")
      
      // Transfer the Secret directly without logging its contents
  }
  ```

- **Data Encryption**: All synchronized secrets maintain their encryption:
  ```yaml
  # Example of a synchronized Secret with maintained encryption
  apiVersion: v1
  kind: Secret
  metadata:
    name: database-credentials
    namespace: production-dr
  type: Opaque
  data:
    username: dXNlcm5hbWU=  # base64-encoded, synchronized as-is
    password: cGFzc3dvcmQ=  # base64-encoded, synchronized as-is
  ```

- **Secret References**: DR-Syncer properly maintains references to secrets across namespaces:
  ```yaml
  # Original deployment with secret reference
  env:
    - name: DB_PASSWORD
      valueFrom:
        secretKeyRef:
          name: database-credentials
          key: password
  
  # References properly maintained in destination namespace
  ```

## Security Best Practices

### Deployment Recommendations

1. **Dedicated Namespace**: Run DR-Syncer in a dedicated namespace with restricted access:
   ```bash
   kubectl create namespace dr-syncer-system
   ```

2. **Restricted RBAC**: Use the least privilege principle for all service accounts:
   ```bash
   # Create role binding only for necessary namespaces
   kubectl create rolebinding dr-syncer-production \
     --role=dr-syncer-source \
     --serviceaccount=dr-syncer-system:dr-syncer-controller \
     --namespace=production
   ```

3. **Regular Key Rotation**: Implement a policy to regularly rotate SSH keys:
   ```bash
   # Annotate RemoteCluster to trigger key rotation
   kubectl annotate remotecluster dr-cluster dr-syncer.io/rotate-ssh-key="true" --overwrite
   ```

4. **Audit Logging**: Enable audit logging for DR-Syncer operations:
   ```yaml
   # Controller deployment with audit logging
   spec:
     containers:
     - name: dr-syncer-controller
       env:
       - name: LOG_LEVEL
         value: "info"
       - name: ENABLE_AUDIT_LOG
         value: "true"
   ```

5. **Network Segmentation**: Implement network policies to restrict communication:
   ```bash
   kubectl apply -f network-policies.yaml
   ```

### Monitoring Security Events

1. **Watch for Unauthorized Access Attempts**:
   ```bash
   # Check agent logs for unauthorized SSH attempts
   kubectl logs -n dr-syncer-system -l app=dr-syncer-agent | grep "Failed authentication"
   ```

2. **Monitor Status Resources**:
   ```bash
   # Check for synchronization failures that might indicate security issues
   kubectl get replications -o json | jq '.items[] | select(.status.phase == "Failed")'
   ```

3. **Set Up Alerts for Security Events**:
   ```yaml
   # Prometheus alert rule example
   groups:
   - name: dr-syncer-security
     rules:
     - alert: DRSyncerUnauthorizedAccess
       expr: rate(dr_syncer_ssh_unauthorized_attempts_total[5m]) > 0
       for: 5m
       labels:
         severity: warning
       annotations:
         description: "Detected unauthorized SSH access attempts to DR-Syncer agent"
   ```

## Vulnerability Management

DR-Syncer follows these practices for vulnerability management:

1. **Regular Container Image Updates**: Base images are regularly updated to include security patches:
   ```dockerfile
   # Dockerfile starts with pinned, regularly updated base image
   FROM alpine:3.19 AS builder
   ```

2. **Dependency Scanning**: All dependencies are regularly scanned for vulnerabilities:
   ```go
   // go.mod is kept up to date with secure versions
   go 1.23
   
   require (
     k8s.io/api v0.29.0
     k8s.io/apimachinery v0.29.0
     k8s.io/client-go v0.29.0
     sigs.k8s.io/controller-runtime v0.16.0
   )
   ```

3. **Security Testing**: Regular security testing is performed:
   ```bash
   # Example security scan command in CI/CD pipeline
   trivy image supporttools/dr-syncer-controller:latest
   ```

4. **Vulnerability Reporting**: If you discover a security vulnerability in DR-Syncer, please report it responsibly by following the security policy in the repository.

## Conclusion

DR-Syncer's security architecture balances powerful disaster recovery capabilities with robust security measures. By following the recommendations in this document, you can ensure that your DR-Syncer deployment maintains the confidentiality, integrity, and availability of your resources across clusters.

Always apply the principle of least privilege, regularly rotate credentials, monitor for security events, and keep DR-Syncer components updated to maintain a strong security posture.
