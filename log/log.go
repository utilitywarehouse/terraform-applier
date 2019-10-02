package log

import (
	"log"
	"os"
	"strings"
)

// LogLevel represents a log level
type LogLevel int

// String returns log level as a string
func (l LogLevel) String() string {
	return [...]string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}[l]
}

const (
	// TRACE trace
	TRACE LogLevel = iota
	// DEBUG debug
	DEBUG
	// INFO info
	INFO
	// WARN warn
	WARN
	// ERROR error
	ERROR
	// FATAL fatal
	FATAL
)

var (
	// Level is the current log level
	Level = INFO
)

// LevelFromString takes a string and returns the corresponding log level
func LevelFromString(level string) LogLevel {
	level = strings.ToUpper(strings.TrimSpace(level))
	switch level {
	case "TRACE":
		return TRACE
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}

// printf prints a message at a given log level
func printf(level LogLevel, msg string, args ...interface{}) {
	if level >= Level {
		log.Printf("["+level.String()+"] "+msg, args...)
	}
}

// Trace logs a message at trace level
func Trace(msg string, args ...interface{}) {
	printf(TRACE, msg, args...)
}

// Debug logs a message at debug level
func Debug(msg string, args ...interface{}) {
	printf(DEBUG, msg, args...)
}

// Info logs a message at info level
func Info(msg string, args ...interface{}) {
	printf(INFO, msg, args...)
}

// Warn logs a message at warn level
func Warn(msg string, args ...interface{}) {
	printf(WARN, msg, args...)
}

// Error logs a message at error level
func Error(msg string, args ...interface{}) {
	printf(ERROR, msg, args...)
}

// Fatal logs a message at fatal level and exits
func Fatal(msg string, args ...interface{}) {
	printf(FATAL, msg, args...)
	os.Exit(1)
}
