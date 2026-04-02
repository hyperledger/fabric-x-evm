/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the logging verbosity level
type LogLevel int

const (
	// SILENT disables all logging
	SILENT LogLevel = iota
	// ERROR logs only errors
	ERROR
	// WARN logs warnings and errors
	WARN
	// INFO logs info, warnings, and errors
	INFO
	// DEBUG logs everything including debug messages
	DEBUG
)

const timeFormat = "2006-01-02 15:04:05.000"

// String returns the string representation of a log level
func (l LogLevel) String() string {
	switch l {
	case SILENT:
		return "SILENT"
	case ERROR:
		return "ERROR"
	case WARN:
		return "WARN"
	case INFO:
		return "INFO"
	case DEBUG:
		return "DEBUG"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel converts a string to a LogLevel
func ParseLogLevel(s string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SILENT":
		return SILENT
	case "ERROR":
		return ERROR
	case "WARN", "WARNING":
		return WARN
	case "INFO":
		return INFO
	case "DEBUG":
		return DEBUG
	default:
		return ERROR // Default to ERROR if unknown
	}
}

// Logger interface defines the logging methods
type Logger interface {
	Debugf(format string, v ...any)
	Infof(format string, v ...any)
	Warnf(format string, v ...any)
	Errorf(format string, v ...any)
}

// ConfigurableLogger implements Logger with configurable log levels
type ConfigurableLogger struct {
	component string
	level     LogLevel
	mu        sync.RWMutex
}

var (
	// Global default log level
	globalLogLevel = ERROR
	// Component-specific log levels
	componentLevels = make(map[string]LogLevel)
	// Mutex for thread-safe access to log levels
	levelMu sync.RWMutex
)

func init() {
	// Read global log level from environment
	if envLevel := os.Getenv("EVM_LOG_LEVEL"); envLevel != "" {
		globalLogLevel = ParseLogLevel(envLevel)
	}

	// Read component-specific log levels
	// Format: EVM_LOG_LEVEL_ComponentName=debug
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "EVM_LOG_LEVEL_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				// Extract component name from EVM_LOG_LEVEL_ComponentName
				componentName := strings.TrimPrefix(parts[0], "EVM_LOG_LEVEL_")
				level := ParseLogLevel(parts[1])
				componentLevels[componentName] = level
			}
		}
	}
}

// NewLogger creates a new configurable logger for the given component
func NewLogger(component string) *ConfigurableLogger {
	levelMu.RLock()
	level, hasComponentLevel := componentLevels[component]
	levelMu.RUnlock()

	if !hasComponentLevel {
		level = globalLogLevel
	}

	return &ConfigurableLogger{
		component: component,
		level:     level,
	}
}

// SetLevel sets the log level for this logger instance
func (l *ConfigurableLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *ConfigurableLogger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// SetGlobalLogLevel sets the global default log level for all new loggers
func SetGlobalLogLevel(level LogLevel) {
	levelMu.Lock()
	defer levelMu.Unlock()
	globalLogLevel = level
}

// SetComponentLogLevel sets the log level for a specific component
func SetComponentLogLevel(component string, level LogLevel) {
	levelMu.Lock()
	defer levelMu.Unlock()
	componentLevels[component] = level
}

// formatLog formats a log message with timestamp, level, and component
// Format: "2026-04-02 09:23:07.719 LEVEL [Component] message"
func (l *ConfigurableLogger) formatLog(levelStr string, format string, v ...any) string {
	timestamp := time.Now().Format(timeFormat)
	message := fmt.Sprintf(format, v...)
	return fmt.Sprintf("%s %s [%s] %s\n", timestamp, levelStr, l.component, message)
}

// Debugf logs a debug message if the log level is DEBUG or higher
func (l *ConfigurableLogger) Debugf(format string, v ...any) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()

	if level >= DEBUG {
		fmt.Fprint(os.Stderr, l.formatLog("DEBUG", format, v...))
	}
}

// Infof logs an info message if the log level is INFO or higher
func (l *ConfigurableLogger) Infof(format string, v ...any) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()

	if level >= INFO {
		fmt.Fprint(os.Stderr, l.formatLog("INFO ", format, v...))
	}
}

// Warnf logs a warning message if the log level is WARN or higher
func (l *ConfigurableLogger) Warnf(format string, v ...any) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()

	if level >= WARN {
		fmt.Fprint(os.Stderr, l.formatLog("WARN ", format, v...))
	}
}

// Errorf logs an error message if the log level is ERROR or higher
func (l *ConfigurableLogger) Errorf(format string, v ...any) {
	l.mu.RLock()
	level := l.level
	l.mu.RUnlock()

	if level >= ERROR {
		fmt.Fprint(os.Stderr, l.formatLog("ERROR", format, v...))
	}
}

// String returns a string representation of the logger
func (l *ConfigurableLogger) String() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return fmt.Sprintf("ConfigurableLogger{component=%s, level=%s}", l.component, l.level)
}

// Ensure ConfigurableLogger implements Logger
var _ Logger = (*ConfigurableLogger)(nil)

// Made with Bob
