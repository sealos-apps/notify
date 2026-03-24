// Package logger provides logging functionality for sealos-notify
package logger

import (
	log "github.com/sirupsen/logrus"
)

// Option is a functional option for configuring the logger
type Option func(*log.Logger)

// WithLevel sets the log level
func WithLevel(level string) Option {
	return func(l *log.Logger) {
		logLevel, err := log.ParseLevel(level)
		if err != nil {
			log.WithError(err).Warn("Invalid log level, using info")
			logLevel = log.InfoLevel
		}
		l.SetLevel(logLevel)
	}
}

// WithFormat sets the log format (json or text)
func WithFormat(format string) Option {
	return func(l *log.Logger) {
		switch format {
		case "json":
			l.SetFormatter(&log.JSONFormatter{
				TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			})
		case "text":
			l.SetFormatter(&log.TextFormatter{
				FullTimestamp:   true,
				TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			})
		default:
			log.WithField("format", format).Warn("Unknown log format, using json")
			l.SetFormatter(&log.JSONFormatter{
				TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			})
		}
	}
}

// WithDebug enables debug mode
func WithDebug(debug bool) Option {
	return func(l *log.Logger) {
		if debug {
			l.SetLevel(log.DebugLevel)
		}
	}
}

// InitLog initializes the logger with the given options
// If logger is nil, creates a new logger; otherwise updates the existing one
func InitLog(logger *log.Logger, opts ...Option) *log.Logger {
	if logger == nil {
		logger = log.New()
	}

	// Apply options
	for _, opt := range opts {
		opt(logger)
	}

	return logger
}
