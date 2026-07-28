package grbl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/DBenYaakov/WriterRobot/internal/console"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
)

const (
	softReset = byte(0x18)
	feedHold  = byte('!')
	statusQ   = byte('?')
)

// ErrDesynchronized means a command response may still be in flight after a
// timeout or cancellation. Send a soft reset before issuing more commands.
var ErrDesynchronized = errors.New("grbl sender desynchronized; soft reset required")

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
	Logger         console.Logger
}

type receivedLine struct {
	line string
	err  error
}

// Sender streams G-code to a GRBL controller.
type Sender struct {
	port io.ReadWriteCloser
	opts Options

	lines    chan receivedLine
	events   chan string
	opSem    chan struct{}
	writeSem chan struct{}
	closing  chan struct{}

	closeOnce sync.Once
	closeErr  error

	stateMu      sync.Mutex
	readerErr    error
	desynced     bool
	eventHistory []string
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
	if opts.Logger == nil {
		opts.Logger = console.New(opts.Log)
	}
	s := &Sender{
		port:     port,
		opts:     opts,
		lines:    make(chan receivedLine, 256),
		events:   make(chan string, 256),
		opSem:    make(chan struct{}, 1),
		writeSem: make(chan struct{}, 1),
		closing:  make(chan struct{}),
	}
	s.opSem <- struct{}{}
	s.writeSem <- struct{}{}
	go s.readLoop()
	return s
}

// Events receives asynchronous GRBL push messages and status reports that were
// not the terminal response for the current operation.
func (s *Sender) Events() <-chan string { return s.events }

// EventHistory returns a snapshot of asynchronous GRBL messages seen so far.
func (s *Sender) EventHistory() []string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return append([]string(nil), s.eventHistory...)
}

func (s *Sender) readLoop() {
	var err error
	defer func() {
		if err == nil {
			err = io.EOF
		}
		s.setReaderErr(err)
		close(s.lines)
	}()

	scanner := bufio.NewScanner(s.port)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		select {
		case s.lines <- receivedLine{line: line}:
		case <-s.closing:
			err = io.EOF
			return
		}
	}
	err = scanner.Err()
}

func (s *Sender) setReaderErr(err error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.readerErr = err
}

func (s *Sender) readerFailure() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.readerErr != nil {
		return s.readerErr
	}
	return io.EOF
}

func (s *Sender) isDesynced() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.desynced
}

func (s *Sender) setDesynced(desynced bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.desynced = desynced
}

func (s *Sender) recordEvent(line string) {
	s.stateMu.Lock()
	s.eventHistory = append(s.eventHistory, line)
	s.stateMu.Unlock()

	select {
	case s.events <- line:
	default:
	}
}

// Close closes the serial port and wakes any current or future waiters.
func (s *Sender) Close() error {
	s.closeOnce.Do(func() {
		close(s.closing)
		s.closeErr = s.port.Close()
	})
	return s.closeErr
}

// Command sends one command and waits for the controller response.
func (s *Sender) Command(ctx context.Context, command string) error {
	return s.sendCommand(ctx, command)
}

// FeedHold sends GRBL's real-time feed-hold command.
func (s *Sender) FeedHold() error {
	s.logTransmitted("<feed hold>")
	if err := s.writeBytes([]byte{feedHold}); err != nil {
		return fmt.Errorf("feed hold: %w", err)
	}
	return nil
}

// SoftReset sends GRBL's real-time soft-reset command and waits for startup.
func (s *Sender) SoftReset(ctx context.Context) error {
	return s.reset(ctx)
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
	release, err := s.beginOperation(ctx, true)
	if err != nil {
		return err
	}
	defer release()

	s.setDesynced(true)
	s.drainPending()

	s.logTransmitted("<soft reset>")
	if err := s.writeBytes([]byte{softReset}); err != nil {
		return fmt.Errorf("soft reset: %w", err)
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		line, err := s.nextLine(deadlineCtx)
		if err != nil {
			return fmt.Errorf("wait for GRBL startup: %w", err)
		}
		s.logReceived(line)
		if strings.HasPrefix(line, "Grbl ") {
			s.setDesynced(false)
			return nil
		}
		if isFatal(line) {
			return errors.New(line)
		}
		s.recordEvent(line)
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
	release, err := s.beginOperation(ctx, false)
	if err != nil {
		return err
	}
	defer release()

	s.logTransmitted(command)
	if err := s.writeString(command + "\n"); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	commandCtx, cancel := context.WithTimeout(ctx, s.opts.CommandTimeout)
	defer cancel()
	for {
		line, err := s.nextLine(commandCtx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				s.setDesynced(true)
			}
			return err
		}
		s.logReceived(line)
		switch {
		case line == "ok":
			return nil
		case strings.HasPrefix(line, "error:"):
			return errors.New(line)
		case strings.HasPrefix(line, "ALARM:"):
			return errors.New(line)
		default:
			s.recordEvent(line)
		}
	}
}

// Status requests and returns the next GRBL real-time status report.
func (s *Sender) Status(ctx context.Context) (string, error) {
	release, err := s.beginOperation(ctx, true)
	if err != nil {
		return "", err
	}
	defer release()
	return s.statusLocked(ctx)
}

func (s *Sender) statusLocked(ctx context.Context) (string, error) {
	if err := s.writeBytes([]byte{statusQ}); err != nil {
		return "", fmt.Errorf("request status: %w", err)
	}
	for {
		line, err := s.nextLine(ctx)
		if err != nil {
			return "", err
		}
		s.logReceived(line)
		if strings.HasPrefix(line, "<") {
			return line, nil
		}
		if isFatal(line) {
			return "", errors.New(line)
		}
		s.recordEvent(line)
	}
}

// WaitForState polls status until GRBL reports one of the requested states.
func (s *Sender) WaitForState(ctx context.Context, states ...string) (string, string, error) {
	if len(states) == 0 {
		return "", "", errors.New("at least one state is required")
	}
	release, err := s.beginOperation(ctx, true)
	if err != nil {
		return "", "", err
	}
	defer release()

	wanted := make(map[string]bool, len(states))
	for _, state := range states {
		wanted[state] = true
	}

	for {
		report, err := s.statusLocked(ctx)
		if err != nil {
			return "", "", err
		}
		state := StatusState(report)
		if wanted[state] || wanted[stateBase(state)] {
			return state, report, nil
		}

		timer := time.NewTimer(s.opts.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", "", ctx.Err()
		case <-timer.C:
		}
	}
}

// WaitIdle polls GRBL until it reports Idle.
func (s *Sender) WaitIdle(ctx context.Context) error {
	idleCtx, cancel := context.WithTimeout(ctx, s.opts.IdleTimeout)
	defer cancel()

	if _, _, err := s.WaitForState(idleCtx, "Idle"); err != nil {
		return fmt.Errorf("wait for Idle: %w", err)
	}
	return nil
}

// StatusState returns the state token from a GRBL status report.
func StatusState(report string) string {
	report = strings.TrimSpace(report)
	report = strings.TrimPrefix(report, "<")
	report = strings.TrimSuffix(report, ">")
	end := len(report)
	for _, sep := range []string{"|", ","} {
		if i := strings.Index(report, sep); i >= 0 && i < end {
			end = i
		}
	}
	return report[:end]
}

func stateBase(state string) string {
	if i := strings.IndexByte(state, ':'); i >= 0 {
		return state[:i]
	}
	return state
}

func (s *Sender) beginOperation(ctx context.Context, allowDesynced bool) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closing:
		return nil, io.ErrClosedPipe
	case <-s.opSem:
	}

	if !allowDesynced && s.isDesynced() {
		s.opSem <- struct{}{}
		return nil, ErrDesynchronized
	}

	return func() { s.opSem <- struct{}{} }, nil
}

func (s *Sender) writeString(value string) error {
	return s.writeBytes([]byte(value))
}

func (s *Sender) writeBytes(value []byte) error {
	select {
	case <-s.closing:
		return io.ErrClosedPipe
	case <-s.writeSem:
	}
	defer func() { s.writeSem <- struct{}{} }()

	select {
	case <-s.closing:
		return io.ErrClosedPipe
	default:
	}
	n, err := s.port.Write(value)
	if err != nil {
		return err
	}
	if n != len(value) {
		return io.ErrShortWrite
	}
	return nil
}

func (s *Sender) nextLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case received, ok := <-s.lines:
		if !ok {
			return "", s.readerFailure()
		}
		if received.err != nil {
			return "", received.err
		}
		return received.line, nil
	}
}

func (s *Sender) drainPending() {
	for {
		select {
		case received, ok := <-s.lines:
			if !ok {
				return
			}
			if received.err == nil {
				s.recordEvent(received.line)
			}
		default:
			return
		}
	}
}

func (s *Sender) logReceived(line string) {
	if s.opts.Verbose {
		s.opts.Logger.Rx(line)
	}
}

func (s *Sender) logTransmitted(line string) {
	if s.opts.Verbose {
		s.opts.Logger.Tx(line)
	}
}

func isFatal(line string) bool {
	return strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "ALARM:")
}
