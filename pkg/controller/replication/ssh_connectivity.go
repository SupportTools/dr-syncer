package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
	"github.com/supporttools/dr-syncer/pkg/logging"
)

// TestSSHConnectivity tests SSH connectivity to the agent pod
func (p *PVCSyncer) TestSSHConnectivity(ctx context.Context, rsyncPod *rsyncpod.RsyncDeployment, targetIP string, targetPort int) error {
	log.WithFields(logrus.Fields{
		"target_ip":   targetIP,
		"target_port": targetPort,
		"pod_name":    rsyncPod.PodName,
	}).Info(logging.LogTagDetail + " Testing SSH connectivity to agent")

	// Context with PVCSyncer for pod exec
	syncerCtx := context.WithValue(ctx, "pvcsync", p)

	// SSH test command
	sshTestCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -p %d root@%s echo SSH_CONNECTION_SUCCESSFUL", 
		targetPort, targetIP)
	
	// Execute command with timeout
	execTimeout := 30 * time.Second
	execCtx, cancel := context.WithTimeout(syncerCtx, execTimeout)
	defer cancel()

	// Execute the command in the pod using the ExecuteCommandInPod function
	stdout, stderr, err := rsyncpod.ExecuteCommandInPod(
		execCtx, 
		p.DestinationK8sClient, 
		rsyncPod.Namespace, 
		rsyncPod.PodName, 
		[]string{"/bin/sh", "-c", sshTestCmd},
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
