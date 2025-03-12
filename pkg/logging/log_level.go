package logging

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// SetLogLevel sets the logging level based on the provided string.
// Valid levels are: debug, info, warn, error
func SetLogLevel(level string) error {
	if log == nil {
		log = SetupLogging()
	}

	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		return fmt.Errorf("invalid log level: %s (valid values: debug, info, warn, error)", level)
	}

	return nil
}
