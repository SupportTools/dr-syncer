package replication

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
)

// performRsync performs the rsync operation between source and destination pods
func (p *PVCSyncer) performRsync(ctx context.Context, destDeployment *rsyncpod.RsyncDeployment, agentPod, mountPath string) error {
	log.WithFields(logrus.Fields{
		"deployment": destDeployment.Name,
		"pod_name":   destDeployment.PodName,
		"agent_pod":  agentPod,
		"mount_path": mountPath,
	}).Info("[DR-SYNC-DETAIL] Starting rsync operation")

	// Default rsync options
	rsyncOptions := []string{
		"--archive",
		"--verbose",
		"--delete",
		"--human-readable",
	}

	// Test SSH connectivity first
	log.Info("[DR-SYNC-DETAIL] Running pre-rsync SSH connectivity check")
	if err := p.TestSSHConnectivity(ctx, destDeployment, agentPod, 2222); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Pre-rsync SSH connectivity check failed")
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}
	log.Info("[DR-SYNC-DETAIL] Pre-rsync SSH connectivity check passed")

	// Combine rsync options
	rsyncOptsStr := strings.Join(rsyncOptions, " ")

	// Build the rsync command with tee to log the output
	// Use -a to preserve permissions, -v for verbose output, and --delete to remove files that don't exist in source
	rsyncCmd := fmt.Sprintf("rsync %s -e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p 2222' root@%s:%s/ /data/ | tee /var/log/console.log",
		rsyncOptsStr, agentPod, mountPath)

	log.WithFields(logrus.Fields{
		"rsync_cmd": rsyncCmd,
		"dest_pod":  destDeployment.PodName,
		"source":    fmt.Sprintf("root@%s:%s/", agentPod, mountPath),
		"dest":      "/data/",
	}).Info("[DR-SYNC-DETAIL] Executing rsync command with detailed info")

	log.WithFields(logrus.Fields{
		"rsync_cmd": rsyncCmd,
	}).Info("[DR-SYNC-DETAIL] Executing rsync command")

	// Execute command in rsync pod
	cmd := []string{"sh", "-c", rsyncCmd}
	stdout, stderr, err := executeCommandInPod(ctx, nil, destDeployment.Namespace, destDeployment.PodName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Rsync command failed")
		return fmt.Errorf("rsync command failed: %v", err)
	}

	// Check if rsync was successful
	if strings.Contains(stderr, "rsync error") {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
		}).Error("[DR-SYNC-ERROR] Rsync error detected in output")
		return fmt.Errorf("rsync error: %s", stderr)
	}

	log.WithFields(logrus.Fields{
		"deployment": destDeployment.Name,
		"pod_name":   destDeployment.PodName,
		"agent_pod":  agentPod,
		"mount_path": mountPath,
	}).Info("[DR-SYNC-DETAIL] Rsync command executed successfully")

	// Output first 100 characters of stdout to help with debugging
	if len(stdout) > 100 {
		log.WithFields(logrus.Fields{
			"stdout_preview": stdout[:100] + "...",
		}).Info("[DR-SYNC-DETAIL] Rsync output preview")

		// Log the full output with multiple log entries for better visibility in logs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				}).Info("[DR-SYNC-OUTPUT] Rsync output line")
			}
		}
	} else if len(stdout) > 0 {
		log.WithFields(logrus.Fields{
			"stdout": stdout,
		}).Info("[DR-SYNC-DETAIL] Rsync output")

		// Log each line separately for better visibility even for shorter outputs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				}).Info("[DR-SYNC-OUTPUT] Rsync output line")
			}
		}
	}

	return nil
}

// TestSSHConnectivity tests SSH connectivity from the rsync pod to the agent pod
func (p *PVCSyncer) TestSSHConnectivity(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment, agentIP string, port int) error {
	log.WithFields(logrus.Fields{
		"deployment": rsyncDeployment.Name,
		"pod_name":   rsyncDeployment.PodName,
		"agent_ip":   agentIP,
		"port":       port,
	}).Info("[DR-SYNC-DETAIL] Testing SSH connectivity")

	// Construct SSH command
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d root@%s 'echo SSH connectivity test'", port, agentIP)

	log.WithFields(logrus.Fields{
		"ssh_command": sshCommand,
	}).Info("[DR-SYNC-DETAIL] Executing SSH command")

	cmd := []string{"sh", "-c", sshCommand}

	// Execute command in pod to generate SSH keys
	stdout, stderr, err := executeCommandInPod(ctx, nil, rsyncDeployment.Namespace, rsyncDeployment.PodName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to execute SSH command")
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}

	log.WithFields(logrus.Fields{
		"stdout": stdout,
	}).Info("[DR-SYNC-DETAIL] SSH connectivity test successful")

	return nil
}
