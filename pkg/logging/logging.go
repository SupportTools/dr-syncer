package logging

import (
"fmt"
"os"
"strings"
"sync"

"github.com/sirupsen/logrus"
"github.com/supporttools/dr-syncer/pkg/config"
"github.com/go-logr/logr"
)

var (
logger *logrus.Logger
once   sync.Once
)

// Default settings
const (
defaultLogFormat = "text" // Default to text for better readability
)

// SetupLogging initializes the logger with the appropriate settings.
func SetupLogging() *logrus.Logger {
once.Do(func() {
logger = logrus.New()
logger.SetOutput(os.Stdout)
logger.SetReportCaller(true)

// Set log level based on config
logger.SetLevel(getLogLevel(config.CFG.LogLevel))

// Get log format from environment
logFormat := strings.ToLower(os.Getenv("LOG_FORMAT"))
if logFormat == "" {
logFormat = defaultLogFormat
}

switch logFormat {
case "text":
logger.SetFormatter(&CustomTextFormatter{
DisableTimestamp: false,
})
default:
// JSON formatter with Kubernetes-friendly fields
logger.SetFormatter(&logrus.JSONFormatter{
DisableTimestamp: false,
PrettyPrint:      false,
FieldMap: logrus.FieldMap{
logrus.FieldKeyTime:  "timestamp",
logrus.FieldKeyLevel: "level",
logrus.FieldKeyMsg:   "message",
logrus.FieldKeyFunc:  "function",
logrus.FieldKeyFile:  "caller",
},
})
}

logger.WithFields(logrus.Fields{
"level":  logger.GetLevel().String(),
"format": logFormat,
}).Debug("Logger initialized")
})

return logger
}

// GetLogger returns the configured logger instance
func GetLogger() *logrus.Logger {
if logger == nil {
return SetupLogging()
}
return logger
}

// CustomTextFormatter formats log entries in a more concise way.
type CustomTextFormatter struct {
DisableTimestamp bool
}

func (f *CustomTextFormatter) Format(entry *logrus.Entry) ([]byte, error) {

timestamp := ""
if !f.DisableTimestamp {
timestamp = entry.Time.Format("2006-01-02 15:04:05") + " "
}

// Format for operator logs
logMessage := fmt.Sprintf("%s[%s] %s", timestamp, strings.ToUpper(entry.Level.String()), entry.Message)

// Add only important fields
if len(entry.Data) > 0 {
// Extract controller and resource information
var fields []string
if ctrl, ok := entry.Data["controller"]; ok {
fields = append(fields, fmt.Sprintf("controller=%v", ctrl))
}
if name, ok := entry.Data["name"]; ok {
fields = append(fields, fmt.Sprintf("name=%v", name))
}
if ns, ok := entry.Data["namespace"]; ok {
fields = append(fields, fmt.Sprintf("namespace=%v", ns))
}
if err, ok := entry.Data["error"]; ok {
fields = append(fields, fmt.Sprintf("error=%v", err))
}

if len(fields) > 0 {
logMessage += " | " + strings.Join(fields, " ")
}
}
logMessage += "\n"

return []byte(logMessage), nil
}

// getLogLevel returns the logrus log level based on the input string.
func getLogLevel(level string) logrus.Level {
switch strings.ToLower(level) {
case "debug":
return logrus.DebugLevel
case "warn", "warning":
return logrus.WarnLevel
case "error":
return logrus.ErrorLevel
case "fatal":
return logrus.FatalLevel
case "panic":
return logrus.PanicLevel
default:
return logrus.InfoLevel
}
}

// logrusAdapter is an adapter to use logrus with logr.
type logrusAdapter struct {
entry *logrus.Entry
}

func (l *logrusAdapter) Init(info logr.RuntimeInfo) {}

func (l *logrusAdapter) Enabled(level int) bool {
return l.entry.Logger.IsLevelEnabled(logrus.Level(level))
}

func (l *logrusAdapter) Info(level int, msg string, keysAndValues ...interface{}) {
l.entry.WithFields(convertToLogrusFields(keysAndValues...)).Info(msg)
}

func (l *logrusAdapter) Error(err error, msg string, keysAndValues ...interface{}) {
l.entry.WithFields(convertToLogrusFields(keysAndValues...)).WithError(err).Error(msg)
}

func (l *logrusAdapter) V(level int) logr.LogSink {
return &logrusAdapter{entry: l.entry}
}

func (l *logrusAdapter) WithValues(keysAndValues ...interface{}) logr.LogSink {
return &logrusAdapter{entry: l.entry.WithFields(convertToLogrusFields(keysAndValues...))}
}

func (l *logrusAdapter) WithName(name string) logr.LogSink {
return &logrusAdapter{entry: l.entry.WithField("name", name)}
}

func convertToLogrusFields(keysAndValues ...interface{}) logrus.Fields {
fields := logrus.Fields{}
for i := 0; i < len(keysAndValues); i += 2 {
if i+1 < len(keysAndValues) {
fields[fmt.Sprintf("%v", keysAndValues[i])] = keysAndValues[i+1]
}
}
return fields
}

// NewLogrusLogr returns a new Logrus logr.Logger.
func NewLogrusLogr() logr.Logger {
return logr.New(&logrusAdapter{entry: logger.WithFields(nil)})
}
