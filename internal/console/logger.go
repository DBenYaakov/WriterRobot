package console

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// Logger emits user-facing console messages as complete, prefixed lines.
type Logger interface {
	Tx(string)
	Rx(string)
	Info(string)
	Warn(string)
	Error(error)
}

// LineLogger writes deterministic, line-oriented console output.
type LineLogger struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a concurrency-safe line logger writing to w.
func New(w io.Writer) *LineLogger {
	if w == nil {
		w = io.Discard
	}
	return &LineLogger{w: w}
}

// Tx logs a transmitted command.
func (l *LineLogger) Tx(message string) {
	l.write("TX", message)
}

// Rx logs a received controller response.
func (l *LineLogger) Rx(message string) {
	l.write("RX", message)
}

// Info logs an informational message.
func (l *LineLogger) Info(message string) {
	l.write("INFO", message)
}

// Infof formats and logs an informational message.
func (l *LineLogger) Infof(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...))
}

// Warn logs a warning message.
func (l *LineLogger) Warn(message string) {
	l.write("WARN", message)
}

// Warnf formats and logs a warning message.
func (l *LineLogger) Warnf(format string, args ...any) {
	l.Warn(fmt.Sprintf(format, args...))
}

// Error logs an error message.
func (l *LineLogger) Error(err error) {
	if err == nil {
		l.write("ERROR", "<nil>")
		return
	}
	l.write("ERROR", err.Error())
}

// Errorf formats and logs an error message.
func (l *LineLogger) Errorf(format string, args ...any) {
	l.write("ERROR", fmt.Sprintf(format, args...))
}

func (l *LineLogger) write(prefix, message string) {
	if l == nil {
		return
	}
	separator := " "
	if len(prefix) == 2 {
		separator = "  "
	}
	line := prefix + separator + singleLine(message) + "\r\n"

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, line)
}

func singleLine(message string) string {
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	return strings.TrimSpace(message)
}
