package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"github.com/supporttools/dr-syncer/pkg/logging"
	"k8s.io/client-go/rest"
)

// The custom context key types are defined in pvc_sync.go

// TestSSHConnectivity tests SSH connectivity to the agent pod
func (p *PVCSyncer) TestSSHConnectivity(ctx context.Context, rsyncPod *rsyncpod.RsyncDeployment, targetIP string, targetPort int, explicitConfig ...*rest.Config) error {
	log.WithFields(logrus.Fields{
		"target_ip":   targetIP,
		"target_port": targetPort,
		"pod_name":    rsyncPod.PodName,
	}).Info(logging.LogTagDetail + " Testing SSH connectivity to agent")

	// Context with PVCSyncer for pod exec
	syncerCtx := context.WithValue(ctx, syncerKey, p)

	// SSH test command
	sshTestCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -p %d root@%s echo SSH_CONNECTION_SUCCESSFUL",
		targetPort, targetIP)

	// Execute command with timeout
	execTimeout := 30 * time.Second
	execCtx, cancel := context.WithTimeout(syncerCtx, execTimeout)
	defer cancel()

	// Check if explicit config is provided
	configToUse := p.DestinationConfig
	if len(explicitConfig) > 0 && explicitConfig[0] != nil {
		configToUse = explicitConfig[0]
		log.WithFields(logrus.Fields{
			"target_ip":   targetIP,
			"target_port": targetPort,
			"pod_name":    rsyncPod.PodName,
			"host":        configToUse.Host,
		}).Info(logging.LogTagDetail + " Using explicit config for SSH connectivity test")
	}

	// Execute the command in the pod using the ExecuteCommandInPod function
	stdout, stderr, err := rsyncpod.ExecuteCommandInPod(
		execCtx,
		p.DestinationK8sClient,
		rsyncPod.Namespace,
		rsyncPod.PodName,
		[]string{"/bin/sh", "-c", sshTestCmd},
		configToUse,
	)
	if err != nil {
		log.WithFields(logrus.Fields{
			"target_ip":   targetIP,
			"target_port": targetPort,
			"pod_name":    rsyncPod.PodName,
			"error":       err,
			"stderr":      stderr,
		}).Error(logging.LogTagError + " SSH connectivity test failed")
		return fmt.Errorf("SSH connectivity test failed: %v, stderr: %s", err, stderr)
	}

	if stdout != "SSH_CONNECTION_SUCCESSFUL\n" {
		log.WithFields(logrus.Fields{
			"target_ip":   targetIP,
			"target_port": targetPort,
			"pod_name":    rsyncPod.PodName,
			"stdout":      stdout,
			"stderr":      stderr,
		}).Error(logging.LogTagError + " SSH connectivity test response incorrect")
		return fmt.Errorf("SSH connectivity test response incorrect: stdout: %s, stderr: %s", stdout, stderr)
	}

	log.WithFields(logrus.Fields{
		"target_ip":   targetIP,
		"target_port": targetPort,
		"pod_name":    rsyncPod.PodName,
	}).Info(logging.LogTagDetail + " SSH connectivity test successful")

	return nil
}
