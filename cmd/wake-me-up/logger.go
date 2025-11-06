package main

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

// InitLogger initializes the logger with the configured log level
func InitLogger(level string) error {
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	parsedLevel, err := logrus.ParseLevel(level)
	if err != nil {
		log.Warnf("Invalid log level '%s', defaulting to 'info'", level)
		parsedLevel = logrus.InfoLevel
	}
	log.SetLevel(parsedLevel)

	return nil
}
