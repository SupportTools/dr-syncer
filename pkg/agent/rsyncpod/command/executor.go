package command

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/logging"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/apimachinery/pkg/util/rand"
	corev1 "k8s.io/api/core/v1"
)

var log = logrus.WithField("component", "command-executor")

// RetryableError represents an error that can be retried
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

// OutputCapture captures command output and logs it
type OutputCapture struct {
	// The original buffer
	buffer *bytes.Buffer
	// The kind of output (stdout/stderr)
	kind string
	// Command info for logging
	podName   string
	namespace string
	command   string
}

// Write implements the io.Writer interface
func (o *OutputCapture) Write(p []byte) (n int, err error) {
	// First, write to the buffer
	n, err = o.buffer.Write(p)
	if err != nil {
		return n, err
	}
	
	// Then log to the logger with a prefix
	output := string(p)
	
	// Use different log levels for stdout vs stderr
	fields := logrus.Fields{
		"pod":       o.podName,
		"namespace": o.namespace,
		"command":   o.command,
		"output":    output,
		"type":      o.kind,
	}
	
	if o.kind == "stdout" {
		log.WithFields(fields).Info(fmt.Sprintf("[REMOTE-EXEC-OUT] %s", strings.TrimSpace(output)))
	} else {
		log.WithFields(fields).Warn(fmt.Sprintf("[REMOTE-EXEC-ERR] %s", strings.TrimSpace(output)))
	}
	
	return n, nil
}

// Executor handles executing commands in pods
type Executor struct {
	client kubernetes.Interface
}

// NewExecutor creates a new command executor
func NewExecutor(client kubernetes.Interface) *Executor {
	return &Executor{
		client: client,
	}
}

// ExecuteCommandOptions contains options for executing a command
type ExecuteCommandOptions struct {
	// The namespace of the pod
	Namespace string
	// The name of the pod
	PodName string
	// The command to execute
	Command []string
	// Maximum number of retries for retryable errors
	MaxRetries int
	// Initial backoff duration for retries
	RetryBackoff time.Duration
}

// ExecuteResult represents the result of a command execution
type ExecuteResult struct {
	// Command that was executed
	Command string
	// The stdout output
	Stdout string
	// The stderr output
	Stderr string
	// Whether the command succeeded
	Succeeded bool
	// Any error that occurred
	Error error
	// Execution time
	ExecutionTime time.Duration
}

// ExecuteCommand executes a command in a pod
func (e *Executor) ExecuteCommand(ctx context.Context, opts ExecuteCommandOptions) (*ExecuteResult, error) {
	commandStr := strings.Join(opts.Command, " ")
	commandId := fmt.Sprintf("cmd-%s", rand.String(6))
	startTime := time.Now()
	
	log.WithFields(logrus.Fields{
		"pod":         opts.PodName,
		"namespace":   opts.Namespace,
		"command":     commandStr,
		"command_id":  commandId, 
		"timestamp":   startTime.Format(time.RFC3339),
	}).Info(logging.LogTagInfo + " Executing command in pod")

	// Configure retry parameters
	maxRetries := 3
	if opts.MaxRetries > 0 {
		maxRetries = opts.MaxRetries
	}
	
	retryBackoff := 10 * time.Second
	if opts.RetryBackoff > 0 {
		retryBackoff = opts.RetryBackoff
	}
	
	// Execute with retry logic
	var stdout, stderr string
	var execErr error
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		stdout, stderr, execErr = e.executeCommandOnce(ctx, opts.Namespace, opts.PodName, opts.Command, commandId)
		
		if execErr == nil {
			// Command succeeded
			break
		}
		
		// Check if error is retryable
		_, isRetryable := execErr.(*RetryableError)
		if !isRetryable {
			// Non-retryable error
			break
		}
		
		if attempt < maxRetries-1 {
			// Log retry attempt
			log.WithFields(logrus.Fields{
				"attempt":     attempt + 1,
				"max_retries": maxRetries,
				"error":       execErr,
				"command_id":  commandId,
			}).Info(logging.LogTagWarn + " Operation failed, retrying...")
			
			// Wait before retrying with exponential backoff
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryBackoff * time.Duration(1<<attempt)):
				// Continue to next attempt
			}
		}
	}
	
	// Prepare result
	executionTime := time.Since(startTime)
	result := &ExecuteResult{
		Command:       commandStr,
		Stdout:        stdout,
		Stderr:        stderr,
		Error:         execErr,
		Succeeded:     execErr == nil,
		ExecutionTime: executionTime,
	}
	
	// Generate execution summary
	summary := fmt.Sprintf("Command execution summary (ID: %s):\n"+
		"Pod: %s/%s\n"+
		"Command: %s\n"+
		"Exit Code: %v\n"+
		"Stdout Size: %d bytes\n"+
		"Stderr Size: %d bytes\n"+
		"Execution Time: %s",
		commandId, opts.Namespace, opts.PodName, commandStr, 
		execErr != nil, len(stdout), len(stderr),
		executionTime)
	
	log.Info(logging.LogTagInfo + " " + summary)
	
	return result, execErr
}

// executeCommandOnce executes a command once without retries
func (e *Executor) executeCommandOnce(ctx context.Context, namespace, podName string, command []string, commandId string) (string, string, error) {
	commandStr := strings.Join(command, " ")
	
	// Set up the ExecOptions for the command
	execOpts := &corev1.PodExecOptions{
		Command: command,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	// Create the URL for the exec request
	req := e.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(execOpts, scheme.ParameterCodec)

	// Get REST config from context
	config := e.getConfigFromContext(ctx, commandId)
	if config == nil {
		return "", "", fmt.Errorf("no REST config found in context")
	}

	// Log the URL
	log.WithFields(logrus.Fields{
		"url":        req.URL().String(),
		"command_id": commandId,
	}).Debug(logging.LogTagDetail + " Preparing execution URL")

	// Create a SPDY executor
	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error":      err,
			"command_id": commandId,
		}).Error(logging.LogTagError + " Failed to create SPDY executor")
		return "", "", &RetryableError{Err: fmt.Errorf("failed to create SPDY executor: %v", err)}
	}

	// Create buffers for stdout and stderr with enhanced logging capability
	var stdoutBuffer, stderrBuffer bytes.Buffer
	stdout := &OutputCapture{
		buffer:    &stdoutBuffer,
		kind:      "stdout",
		podName:   podName,
		namespace: namespace,
		command:   commandStr,
	}
	stderr := &OutputCapture{
		buffer:    &stderrBuffer,
		kind:      "stderr",
		podName:   podName,
		namespace: namespace,
		command:   commandStr,
	}

	// Execute the command
	log.WithFields(logrus.Fields{
		"command_id": commandId,
		"timestamp":  time.Now().Format(time.RFC3339),
	}).Info(logging.LogTagInfo + " Starting command execution...")
	
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	// Check for errors
	if err != nil {
		// Determine if the error is retryable
		if strings.Contains(err.Error(), "connection refused") || 
		   strings.Contains(err.Error(), "connection reset") || 
		   strings.Contains(err.Error(), "broken pipe") {
			return stdoutBuffer.String(), stderrBuffer.String(), &RetryableError{Err: fmt.Errorf("transient error: %v", err)}
		}
		
		log.WithFields(logrus.Fields{
			"error":      err,
			"stderr":     stderrBuffer.String(),
			"command_id": commandId,
			"timestamp":  time.Now().Format(time.RFC3339),
		}).Error(logging.LogTagError + " Failed to execute command")
		return stdoutBuffer.String(), stderrBuffer.String(), fmt.Errorf("failed to execute command: %v, stderr: %s", err, stderrBuffer.String())
	}

	// Log completion
	log.WithFields(logrus.Fields{
		"pod":        podName,
		"namespace":  namespace,
		"command":    commandStr,
		"stdout_len": stdoutBuffer.Len(),
		"stderr_len": stderrBuffer.Len(),
		"command_id": commandId,
		"timestamp":  time.Now().Format(time.RFC3339),
	}).Info(logging.LogTagInfo + " Command execution completed successfully")

	// If there's content in stderr but no error was returned, log it as a warning
	if stderrBuffer.Len() > 0 && err == nil {
		log.WithFields(logrus.Fields{
			"pod":        podName,
			"namespace":  namespace,
			"command":    commandStr,
			"stderr":     stderrBuffer.String(),
			"command_id": commandId,
		}).Warn(logging.LogTagWarn + " Command produced stderr output but no error")
	}

	return stdoutBuffer.String(), stderrBuffer.String(), nil
}

// getConfigFromContext extracts the REST config from the context
func (e *Executor) getConfigFromContext(ctx context.Context, commandId string) *rest.Config {
	// First priority: explicit config in context
	if configFromCtx := ctx.Value("k8s-config"); configFromCtx != nil {
		config := configFromCtx.(*rest.Config)
		log.WithFields(logrus.Fields{
			"host":       config.Host,
			"command_id": commandId,
		}).Info(logging.LogTagInfo + " Using explicit config from context")
		return config
	} 
	
	// No explicit config provided - check for PVCSyncer in context
	if ctx.Value("pvcsync") != nil {
		// Get config from PVCSyncer context
		type ConfigProvider interface {
			GetSourceConfig() *rest.Config
			GetDestinationConfig() *rest.Config
			GetSourceClient() kubernetes.Interface
			GetDestinationClient() kubernetes.Interface
		}
		
		if provider, ok := ctx.Value("pvcsync").(ConfigProvider); ok {
			// Compare the client with source/destination clients to determine which to use
			srcClient := provider.GetSourceClient()
			destClient := provider.GetDestinationClient()
			
			// Compare client URLs to determine if we're using source or destination client
			clientHost := e.client.CoreV1().RESTClient().Get().URL().Host
			srcHost := srcClient.CoreV1().RESTClient().Get().URL().Host
			destHost := destClient.CoreV1().RESTClient().Get().URL().Host
			
			if clientHost == srcHost {
				config := provider.GetSourceConfig()
				log.WithFields(logrus.Fields{
					"host":        config.Host,
					"client_host": clientHost,
					"command_id":  commandId,
				}).Info(logging.LogTagInfo + " Using source config from PVCSyncer (matched client)")
				return config
			} else if clientHost == destHost {
				config := provider.GetDestinationConfig()
				log.WithFields(logrus.Fields{
					"host":        config.Host,
					"client_host": clientHost,
					"command_id":  commandId,
				}).Info(logging.LogTagInfo + " Using destination config from PVCSyncer (matched client)")
				return config
			} else {
				// If no direct match, use simple heuristic - dest for rsync operations
				if provider.GetDestinationConfig() != nil {
					config := provider.GetDestinationConfig()
					log.WithFields(logrus.Fields{
						"host":        config.Host,
						"client_host": clientHost,
						"src_host":    srcHost,
						"dest_host":   destHost,
						"command_id":  commandId,
					}).Info(logging.LogTagInfo + " Using destination config from PVCSyncer (no direct match)")
					return config
				} else if provider.GetSourceConfig() != nil {
					config := provider.GetSourceConfig()
					log.WithFields(logrus.Fields{
						"host":        config.Host,
						"client_host": clientHost,
						"src_host":    srcHost,
						"dest_host":   destHost,
						"command_id":  commandId,
					}).Info(logging.LogTagInfo + " Using source config from PVCSyncer (no direct match)")
					return config
				}
			}
		}
	}
	
	// No config found
	return nil
}

// ExecuteCommandInPod executes a command in a pod - compatibility wrapper for existing code
func ExecuteCommandInPod(ctx context.Context, client kubernetes.Interface, namespace, podName string, command []string) (string, string, error) {
	executor := NewExecutor(client)
	result, err := executor.ExecuteCommand(ctx, ExecuteCommandOptions{
		Namespace:    namespace,
		PodName:      podName,
		Command:      command,
		MaxRetries:   3,
		RetryBackoff: 10 * time.Second,
	})
	
	if result != nil {
		return result.Stdout, result.Stderr, err
	}
	
	return "", "", err
}
