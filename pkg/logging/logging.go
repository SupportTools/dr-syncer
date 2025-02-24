package logging

import (
	"os"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
)

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
			DisableTimestamp:       true,
			CallerPrettyfier:       callerPrettyfier,
			DisableColors:          true,
			DisableLevelTruncation: true,
			DisableSorting:         true,
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

// callerPrettyfier is a custom function to format the caller field without the file path.
func callerPrettyfier(frame *runtime.Frame) (function string, file string) {
	funcName := frame.Function
	parts := strings.Split(funcName, "/")
	function = "[" + parts[len(parts)-1] + "]"
	return function, ""
}
