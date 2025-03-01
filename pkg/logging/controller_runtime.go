package logging

import (
	"github.com/go-logr/logr"
	"github.com/sirupsen/logrus"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// LogrusLogAdapter adapts logrus logger to controller-runtime's logr interface
type LogrusLogAdapter struct {
	logger *logrus.Logger
	name   string
}

// Ensure LogrusLogAdapter implements logr.LogSink
var _ logr.LogSink = &LogrusLogAdapter{}

// Init implements logr.LogSink
func (l *LogrusLogAdapter) Init(info logr.RuntimeInfo) {
	// No initialization needed
}

// Enabled implements logr.LogSink
func (l *LogrusLogAdapter) Enabled(level int) bool {
	// Map logr levels to logrus levels
	// logr level 0 is equivalent to logrus.InfoLevel
	// Each increment in logr level means "less important"
	switch {
	case level == 0:
		return l.logger.IsLevelEnabled(logrus.InfoLevel)
	case level >= 1:
		return l.logger.IsLevelEnabled(logrus.DebugLevel)
	default:
		return true
	}
}

// Info implements logr.LogSink
func (l *LogrusLogAdapter) Info(level int, msg string, keysAndValues ...interface{}) {
	entry := l.logger.WithField("name", l.name)

	// Add key-value pairs to the log entry
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			key, ok := keysAndValues[i].(string)
			if !ok {
				key = "unknown"
			}
			entry = entry.WithField(key, keysAndValues[i+1])
		}
	}

	// Map logr levels to logrus levels
	switch {
	case level == 0:
		entry.Info(msg)
	case level >= 1:
		entry.Debug(msg)
	}
}

// Error implements logr.LogSink
func (l *LogrusLogAdapter) Error(err error, msg string, keysAndValues ...interface{}) {
	entry := l.logger.WithField("name", l.name)

	if err != nil {
		entry = entry.WithError(err)
	}

	// Add key-value pairs to the log entry
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			key, ok := keysAndValues[i].(string)
			if !ok {
				key = "unknown"
			}
			entry = entry.WithField(key, keysAndValues[i+1])
		}
	}

	entry.Error(msg)
}

// WithValues implements logr.LogSink
func (l *LogrusLogAdapter) WithValues(keysAndValues ...interface{}) logr.LogSink {
	// Create a new adapter with the same logger
	newAdapter := &LogrusLogAdapter{
		logger: l.logger,
		name:   l.name,
	}

	return newAdapter
}

// WithName implements logr.LogSink
func (l *LogrusLogAdapter) WithName(name string) logr.LogSink {
	// Create a new adapter with the updated name
	newName := name
	if l.name != "" {
		newName = l.name + "." + name
	}

	return &LogrusLogAdapter{
		logger: l.logger,
		name:   newName,
	}
}

// NewControllerRuntimeLogger creates a new logr.Logger that uses our logrus logger
func NewControllerRuntimeLogger(logger *logrus.Logger) logr.Logger {
	return logr.New(&LogrusLogAdapter{
		logger: logger,
		name:   "",
	})
}

// SetupControllerRuntimeLogging configures controller-runtime to use our logrus logger
func SetupControllerRuntimeLogging(logger *logrus.Logger) {
	ctrl := NewControllerRuntimeLogger(logger)
	ctrllog.SetLogger(ctrl)
}
