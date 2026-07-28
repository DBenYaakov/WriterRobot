package console

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestLoggerMethodsEmitOneCompleteLine(t *testing.T) {
	tests := []struct {
		name string
		log  func(*LineLogger)
		want string
	}{
		{"tx", func(l *LineLogger) { l.Tx("G1 X1") }, "TX  G1 X1\r\n"},
		{"rx", func(l *LineLogger) { l.Rx("ok") }, "RX  ok\r\n"},
		{"info", func(l *LineLogger) { l.Info("ready") }, "INFO ready\r\n"},
		{"warn", func(l *LineLogger) { l.Warn("careful") }, "WARN careful\r\n"},
		{"error", func(l *LineLogger) { l.Error(errors.New("failed")) }, "ERROR failed\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(&buf)

			tt.log(logger)

			if got := buf.String(); got != tt.want {
				t.Fatalf("log output = %q, want %q", got, tt.want)
			}
			if strings.Count(buf.String(), "\n") != 1 {
				t.Fatalf("log output should contain exactly one newline: %q", buf.String())
			}
		})
	}
}

func TestLoggerSanitizesEmbeddedNewlines(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Info("first\nsecond\rthird")

	if got, want := buf.String(), "INFO first second third\r\n"; got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

func TestConcurrentLoggingDoesNotInterleavePartialLines(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	const workers = 20
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.Infof("message-%02d", i)
		}(i)
	}
	wg.Wait()

	output := buf.String()
	if strings.Count(output, "\n") != workers {
		t.Fatalf("newline count = %d, want %d; output=%q", strings.Count(output, "\n"), workers, output)
	}
	trimmed := strings.TrimSuffix(output, "\r\n")
	for _, line := range strings.Split(trimmed, "\r\n") {
		if !strings.HasPrefix(line, "INFO message-") {
			t.Fatalf("interleaved or malformed line: %q in %q", line, output)
		}
	}
}

func TestFormattedHelpers(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Warnf("state %s", "Hold")
	logger.Errorf("failure %d", 1)

	got := buf.String()
	want := fmt.Sprintf("WARN state Hold\r\nERROR failure 1\r\n")
	if got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}
