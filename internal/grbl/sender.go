package grbl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/DBenYaakov/WriterRobot/internal/gcode"
)

const (
	softReset = byte(0x18)
	statusQ   = byte('?')
)

// Options controls session initialization, streaming, and response timeouts.
type Options struct {
	CommandTimeout time.Duration
	IdleTimeout    time.Duration
	PollInterval   time.Duration
	ResetOnOpen    bool
	HomeOnStart    bool
	StartupDwell   time.Duration
	Verbose        bool
	Log            io.Writer
}

// Sender streams G-code to a GRBL controller.
type Sender struct {
	port io.ReadWriteCloser
	scan *bufio.Scanner
	opts Options
}

func New(port io.ReadWriteCloser, opts Options) *Sender {
	if opts.CommandTimeout <= 0 {
		opts.CommandTimeout = 60 * time.Second
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 2 * time.Minute
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 200 * time.Millisecond
	}
	if opts.StartupDwell < 0 {
		opts.StartupDwell = 0
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	scanner := bufio.NewScanner(port)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	return &Sender{port: port, scan: scanner, opts: opts}
}

func (s *Sender) Close() error { return s.port.Close() }

// Command sends one command and waits for the controller response.
func (s *Sender) Command(ctx context.Context, command string) error {
	return s.sendCommand(ctx, command)
}

// Initialize resets GRBL when requested, then begins every session by homing
// the machine and dwelling for the configured interval.
func (s *Sender) Initialize(ctx context.Context) error {
	if s.opts.ResetOnOpen {
		if err := s.reset(ctx); err != nil {
			return err
		}
	}

	if s.opts.HomeOnStart {
		if err := s.sendCommand(ctx, "$H"); err != nil {
			return fmt.Errorf("home machine: %w", err)
		}
	}

	if s.opts.StartupDwell > 0 {
		command := fmt.Sprintf("G4 P%.3f", s.opts.StartupDwell.Seconds())
		if err := s.sendCommand(ctx, command); err != nil {
			return fmt.Errorf("startup dwell: %w", err)
		}
	}

	return nil
}

func (s *Sender) reset(ctx context.Context) error {
	if s.opts.Verbose {
		fmt.Fprintln(s.opts.Log, "-> <soft reset>")
	}
	if _, err := s.port.Write([]byte{softReset}); err != nil {
		return fmt.Errorf("soft reset: %w", err)
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		line, err := s.readLine(deadlineCtx)
		if err != nil {
			return fmt.Errorf("wait for GRBL startup: %w", err)
		}
		if s.opts.Verbose {
			fmt.Fprintf(s.opts.Log, "<- %s\n", line)
		}
		if strings.HasPrefix(line, "Grbl ") {
			return nil
		}
		if isFatal(line) {
			return errors.New(line)
		}
	}
}

// Send streams commands one at a time, waiting for ok after each command.
func (s *Sender) Send(ctx context.Context, lines []gcode.Line) error {
	for i, line := range lines {
		if err := s.sendCommand(ctx, line.Command); err != nil {
			return fmt.Errorf("source line %d (%d/%d), %q: %w", line.Number, i+1, len(lines), line.Command, err)
		}
	}
	return nil
}

func (s *Sender) sendCommand(ctx context.Context, command string) error {
	if s.opts.Verbose {
		fmt.Fprintf(s.opts.Log, "-> %s\n", command)
	}
	if _, err := io.WriteString(s.port, command+"\n"); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	commandCtx, cancel := context.WithTimeout(ctx, s.opts.CommandTimeout)
	defer cancel()
	for {
		line, err := s.readLine(commandCtx)
		if err != nil {
			return err
		}
		if s.opts.Verbose {
			fmt.Fprintf(s.opts.Log, "<- %s\n", line)
		}
		switch {
		case line == "ok":
			return nil
		case isFatal(line):
			return errors.New(line)
		}
	}
}

// WaitIdle polls GRBL until it reports Idle.
func (s *Sender) WaitIdle(ctx context.Context) error {
	idleCtx, cancel := context.WithTimeout(ctx, s.opts.IdleTimeout)
	defer cancel()

	ticker := time.NewTicker(s.opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-idleCtx.Done():
			return fmt.Errorf("wait for Idle: %w", idleCtx.Err())
		case <-ticker.C:
			if _, err := s.port.Write([]byte{statusQ}); err != nil {
				return fmt.Errorf("request status: %w", err)
			}
			line, err := s.readLine(idleCtx)
			if err != nil {
				return err
			}
			if s.opts.Verbose {
				fmt.Fprintf(s.opts.Log, "<- %s\n", line)
			}
			if strings.HasPrefix(line, "<Idle|") || line == "<Idle>" {
				return nil
			}
			if isFatal(line) {
				return errors.New(line)
			}
		}
	}
}

func (s *Sender) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if s.scan.Scan() {
			ch <- result{line: strings.TrimSpace(s.scan.Text())}
			return
		}
		err := s.scan.Err()
		if err == nil {
			err = io.EOF
		}
		ch <- result{err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.line, r.err
	}
}

func isFatal(line string) bool {
	return strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "ALARM:")
}
