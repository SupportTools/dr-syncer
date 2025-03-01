package replication

import (
	"github.com/sirupsen/logrus"
)

// log is the package-level logger
var log *logrus.Entry

// init initializes the package-level logger
func init() {
	log = logrus.WithField("component", "replication-keys")
}
