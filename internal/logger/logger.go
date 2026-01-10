// Package logger provides a simple logging interface for rr components.
// It allows packages to log debug, info, warn, and error messages without
// being coupled to a specific logging implementation.
package logger

import (
	"fmt"
	"log"
	"os"
)

// Logger defines the interface for logging operations.
// All methods accept a format string and arguments, similar to fmt.Printf.
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// envLogger implements Logger and logs to stdout/stderr based on environment.
// Debug messages are only printed when RR_DEBUG is set.
type envLogger struct {
	prefix string
}

// NewEnvLogger creates a logger that respects the RR_DEBUG environment variable.
// The prefix is prepended to all log messages (e.g., "[lock]" or "[sync]").
func NewEnvLogger(prefix string) Logger {
	return &envLogger{prefix: prefix}
}

func (l *envLogger) Debug(format string, args ...interface{}) {
	if os.Getenv("RR_DEBUG") != "" {
		log.Printf(l.prefix+" "+format, args...)
	}
}

func (l *envLogger) Info(format string, args ...interface{}) {
	log.Printf(l.prefix+" "+format, args...)
}

func (l *envLogger) Warn(format string, args ...interface{}) {
	log.Printf(l.prefix+" WARN: "+format, args...)
}

func (l *envLogger) Error(format string, args ...interface{}) {
	log.Printf(l.prefix+" ERROR: "+format, args...)
}

// noopLogger implements Logger but discards all messages.
// Useful for testing or when logging is not desired.
type noopLogger struct{}

// Noop returns a logger that discards all messages.
func Noop() Logger {
	return &noopLogger{}
}

func (l *noopLogger) Debug(format string, args ...interface{}) {}
func (l *noopLogger) Info(format string, args ...interface{})  {}
func (l *noopLogger) Warn(format string, args ...interface{})  {}
func (l *noopLogger) Error(format string, args ...interface{}) {}

// LogMessage represents a captured log message.
type LogMessage struct {
	Level   string
	Message string
}

// BufferLogger captures log messages for testing.
// Exported for use in test assertions.
type BufferLogger struct {
	Messages []LogMessage
}

// NewBufferLogger creates a logger that captures messages for inspection.
// Useful for testing that code logs expected messages.
func NewBufferLogger() *BufferLogger {
	return &BufferLogger{
		Messages: make([]LogMessage, 0),
	}
}

func (l *BufferLogger) Debug(format string, args ...interface{}) {
	l.Messages = append(l.Messages, LogMessage{Level: "debug", Message: fmt.Sprintf(format, args...)})
}

func (l *BufferLogger) Info(format string, args ...interface{}) {
	l.Messages = append(l.Messages, LogMessage{Level: "info", Message: fmt.Sprintf(format, args...)})
}

func (l *BufferLogger) Warn(format string, args ...interface{}) {
	l.Messages = append(l.Messages, LogMessage{Level: "warn", Message: fmt.Sprintf(format, args...)})
}

func (l *BufferLogger) Error(format string, args ...interface{}) {
	l.Messages = append(l.Messages, LogMessage{Level: "error", Message: fmt.Sprintf(format, args...)})
}

// HasLevel returns true if any message was logged at the given level.
func (l *BufferLogger) HasLevel(level string) bool {
	for _, m := range l.Messages {
		if m.Level == level {
			return true
		}
	}
	return false
}

// Clear removes all captured messages.
func (l *BufferLogger) Clear() {
	l.Messages = l.Messages[:0]
}

// defaultLogger is the package-level default logger.
var defaultLogger = NewEnvLogger("")

// Default returns the default logger for the package.
// This is an environment-based logger with no prefix.
func Default() Logger {
	return defaultLogger
}

// SetDefault sets the default logger for the package.
// This is useful for testing or to configure logging globally.
func SetDefault(l Logger) {
	defaultLogger = l
}
