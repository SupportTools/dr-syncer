package controllers

import (
	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/logging"
)

// Initialize package logger
var log *logrus.Logger

func init() {
	// Use the existing logger from the logging package
	log = logging.SetupLogging()
}
