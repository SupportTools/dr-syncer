package logging

import (
	"os"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
)

// Log tags for different log categories
const (
	LogTagDetail = "[DR-SYNC-DETAIL]" // Detailed operational logs (debug level)
	LogTagWarn   = "[DR-SYNC-WARN]"   // Warning messages (warning level)
	LogTagError  = "[DR-SYNC-ERROR]"  // Error messages (error level)
	LogTagOutput = "[DR-SYNC-OUTPUT]" // Command output logs (debug level)
	LogTagInfo   = "[DR-SYNC-INFO]"   // Important operation logs (info level)
	
	// Step tags for workflow steps
	LogTagStep0     = "[DR-SYNC-STEP-0]"     // Acquiring lock
	LogTagStep1     = "[DR-SYNC-STEP-1]"     // Deploying rsync pod
	LogTagStep2     = "[DR-SYNC-STEP-2]"     // Generating SSH keys
	LogTagStep3     = "[DR-SYNC-STEP-3]"     // Getting public key
	LogTagStep4     = "[DR-SYNC-STEP-4]"     // Checking PVC mount
	LogTagStep5     = "[DR-SYNC-STEP-5]"     // Finding node where PVC is mounted
	LogTagStep6     = "[DR-SYNC-STEP-6]"     // Finding agent on node
	LogTagStep7     = "[DR-SYNC-STEP-7]"     // Finding mount path
	LogTagStep8     = "[DR-SYNC-STEP-8]"     // Pushing public key
	LogTagStep9     = "[DR-SYNC-STEP-9]"     // Testing SSH connectivity
	LogTagStep10    = "[DR-SYNC-STEP-10]"    // Running rsync
	LogTagStep11    = "[DR-SYNC-STEP-11]"    // Updating annotations
	LogTagStep12    = "[DR-SYNC-STEP-12]"    // Cleaning up resources
	LogTagStep13    = "[DR-SYNC-STEP-13]"    // Releasing lock
	
	// Step completion tags
	LogTagStep0Complete  = "[DR-SYNC-STEP-0-COMPLETE]"  // Lock acquired
	LogTagStep1Complete  = "[DR-SYNC-STEP-1-COMPLETE]"  // Pod deployed
	LogTagStep2Complete  = "[DR-SYNC-STEP-2-COMPLETE]"  // SSH keys generated
	LogTagStep3Complete  = "[DR-SYNC-STEP-3-COMPLETE]"  // Public key retrieved
	LogTagStep4Complete  = "[DR-SYNC-STEP-4-COMPLETE]"  // PVC mount checked
	LogTagStep5Complete  = "[DR-SYNC-STEP-5-COMPLETE]"  // Node found
	LogTagStep6Complete  = "[DR-SYNC-STEP-6-COMPLETE]"  // Agent found
	LogTagStep7Complete  = "[DR-SYNC-STEP-7-COMPLETE]"  // Mount path found
	LogTagStep8Complete  = "[DR-SYNC-STEP-8-COMPLETE]"  // Public key pushed
	LogTagStep9Complete  = "[DR-SYNC-STEP-9-COMPLETE]"  // SSH connectivity tested
	LogTagStep10Complete = "[DR-SYNC-STEP-10-COMPLETE]" // Rsync complete
	LogTagStep11Complete = "[DR-SYNC-STEP-11-COMPLETE]" // Annotations updated
	LogTagStep12Complete = "[DR-SYNC-STEP-12-COMPLETE]" // Resources cleaned up
	LogTagStep13Complete = "[DR-SYNC-STEP-13-COMPLETE]" // Lock released
	
	// Skip and complete tags
	LogTagSkip     = "[DR-SYNC-SKIP]"     // Skipping step
	LogTagComplete = "[DR-SYNC-COMPLETE]" // Workflow completed
)

// Export the logger instance for use across packages
var log *logrus.Logger

// SetupLogging initializes the logger with the appropriate settings.
func SetupLogging() *logrus.Logger {
	// Get the debug from environment variable
	debug := false
	if os.Getenv("DEBUG") == "true" {
		debug = true
	}
	if log == nil {
		log = logrus.New()
		log.SetOutput(os.Stdout)
		log.SetReportCaller(true)

		customFormatter := &logrus.TextFormatter{
			DisableTimestamp: true,
			CallerPrettyfier: callerPrettyfier,
		}

		log.SetFormatter(customFormatter)

		if debug {
			log.SetLevel(logrus.DebugLevel)
		} else {
			log.SetLevel(logrus.InfoLevel)
		}
	}

	return log
}

// LogDetail logs a detailed message at DEBUG level
func LogDetail(entry *logrus.Entry, message string) {
	if entry != nil {
		entry.Debug(LogTagDetail + " " + message)
	} else {
		log.Debug(LogTagDetail + " " + message)
	}
}

// LogInfo logs an important operational message at INFO level
func LogInfo(entry *logrus.Entry, message string) {
	if entry != nil {
		entry.Info(LogTagInfo + " " + message)
	} else {
		log.Info(LogTagInfo + " " + message)
	}
}

// LogWarn logs a warning message at WARN level
func LogWarn(entry *logrus.Entry, message string) {
	if entry != nil {
		entry.Warn(LogTagWarn + " " + message)
	} else {
		log.Warn(LogTagWarn + " " + message)
	}
}

// LogError logs an error message at ERROR level
func LogError(entry *logrus.Entry, message string) {
	if entry != nil {
		entry.Error(LogTagError + " " + message)
	} else {
		log.Error(LogTagError + " " + message)
	}
}

// LogOutput logs command output at DEBUG level
func LogOutput(entry *logrus.Entry, message string) {
	if entry != nil {
		entry.Debug(LogTagOutput + " " + message)
	} else {
		log.Debug(LogTagOutput + " " + message)
	}
}

// callerPrettyfier is a custom function to format the caller field without the file path.
func callerPrettyfier(frame *runtime.Frame) (function string, file string) {
	funcName := frame.Function
	parts := strings.Split(funcName, "/")
	function = parts[len(parts)-1]
	return function, ""
}
