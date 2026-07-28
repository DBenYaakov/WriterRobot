package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DBenYaakov/WriterRobot/internal/config"
	"github.com/DBenYaakov/WriterRobot/internal/diagnostics"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/grbl"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
	"go.bug.st/serial"
	"golang.org/x/term"
)

func main() { os.Exit(run()) }

func run() int {
	var (
		portName        = flag.String("port", "/dev/cu.usbmodem201912341", "serial device")
		baud            = flag.Int("baud", 115200, "serial baud rate")
		commandTO       = flag.Duration("command-timeout", 60*time.Second, "maximum time to wait for ok")
		idleTO          = flag.Duration("idle-timeout", 2*time.Minute, "maximum time to wait for Idle")
		reset           = flag.Bool("reset", true, "send a GRBL soft reset after opening the port")
		home            = flag.Bool("home", true, "home the machine at the beginning of every session")
		startupDwell    = flag.Duration("startup-dwell", 300*time.Millisecond, "dwell after homing before continuing")
		waitIdle        = flag.Bool("wait-idle", true, "wait until GRBL reports Idle after streaming")
		verbose         = flag.Bool("verbose", true, "print commands and controller responses")
		calibrateMode   = flag.Bool("calibrate", false, "interactively calibrate pen height and starting position")
		calibrationStep = flag.Float64("calibration-step", 0.05, "Z movement in millimeters for each pen-calibration arrow press")
		positionStep    = flag.Float64("position-step", 1.0, "X/Y movement in millimeters for each position-calibration arrow press")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] file.gcode\n       %s --port DEVICE --calibrate\n\n", os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *calibrateMode {
		if flag.NArg() != 0 {
			flag.Usage()
			return 2
		}
	} else if flag.NArg() != 1 {
		flag.Usage()
		return 2
	}

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return fail("load configuration", err)
	}

	mode := &serial.Mode{BaudRate: *baud}
	port, err := serial.Open(*portName, mode)
	if err != nil {
		return fail("open serial port", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sender := grbl.New(port, grbl.Options{CommandTimeout: *commandTO, IdleTimeout: *idleTO, ResetOnOpen: *reset, HomeOnStart: *home, StartupDwell: *startupDwell, Verbose: *verbose, Log: os.Stdout})
	defer sender.Close()
	robot := machine.New(sender)
	pen := machine.NewPen(robot, cfg.PenUp, cfg.PenDown)
	recovery := newMachineRecovery(sender, robot, pen, os.Stdout, defaultRecoveryTimings)

	if *calibrateMode {
		if *calibrationStep <= 0 {
			return fail("calibrate", errors.New("--calibration-step must be greater than zero"))
		}
		if *positionStep <= 0 {
			return fail("calibrate", errors.New("--position-step must be greater than zero"))
		}
		if err := sender.Initialize(ctx); err != nil {
			return fail("initialize GRBL", err)
		}
		if err := calibrate(ctx, sender, robot, pen, &cfg, *calibrationStep, *positionStep, recovery); err != nil {
			return fail("calibrate", err)
		}
		path, err := config.Save(cfg)
		if err != nil {
			return fail("save configuration", err)
		}
		fmt.Printf("Saved calibration to %s\n", path)
		fmt.Printf("Pen up Z%.3f, pen down Z%.3f, start X%.3f Y%.3f\n", cfg.PenUp, cfg.PenDown, cfg.StartX, cfg.StartY)
		return 0
	}

	file, err := os.Open(flag.Arg(0))
	if err != nil {
		return fail("open G-code", err)
	}
	defer file.Close()
	lines, err := gcode.Read(file)
	if err != nil {
		return fail("parse G-code", err)
	}
	if len(lines) == 0 {
		return fail("parse G-code", errors.New("file contains no commands"))
	}
	for i := range lines {
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{PEN_DOWN}}", fmt.Sprintf("%.3f", cfg.PenDown))
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{PEN_UP}}", fmt.Sprintf("%.3f", cfg.PenUp))
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{START_X}}", fmt.Sprintf("%.3f", cfg.StartX))
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{START_Y}}", fmt.Sprintf("%.3f", cfg.StartY))
	}

	fmt.Printf("Using configuration %s (pen up Z%.3f, pen down Z%.3f, start X%.3f Y%.3f)\n", cfgPath, cfg.PenUp, cfg.PenDown, cfg.StartX, cfg.StartY)
	fmt.Printf("Sending %d commands to %s at %d baud\n", len(lines), *portName, *baud)
	if err := sender.Initialize(ctx); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail("initialize GRBL", err)
	}
	if err := robot.MoveMachineXYTo(ctx, cfg.StartX, cfg.StartY); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail("move to paper origin", err)
	}
	if err := robot.SetProgramXYOrigin(ctx); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail("set paper origin", err)
	}
	fmt.Printf("Paper origin set to machine X%.3f Y%.3f; program coordinates are now relative to that point.\n", cfg.StartX, cfg.StartY)
	if err := sender.Send(ctx, lines); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail("stream G-code", err)
	}
	if *waitIdle {
		fmt.Println("Waiting for machine to become Idle...")
		if err := sender.WaitIdle(ctx); err != nil {
			recoverAfterInterrupt(ctx, recovery, err)
			return fail("wait for completion", err)
		}
	}
	fmt.Println("Done.")
	return 0
}

type recoveryTimings struct {
	holdWait       time.Duration
	resetWait      time.Duration
	commandWait    time.Duration
	finalStateWait time.Duration
}

type gcodeStreamer interface {
	Send(context.Context, []gcode.Line) error
}

var defaultRecoveryTimings = recoveryTimings{
	holdWait:       2 * time.Second,
	resetWait:      5 * time.Second,
	commandWait:    5 * time.Second,
	finalStateWait: 2 * time.Second,
}

type recoveryReport struct {
	feedHoldSent       bool
	feedHoldErr        error
	stopStateConfirmed bool
	stopState          string
	stopStateErr       error
	resetSent          bool
	resetErr           error
	unlockAttempted    bool
	unlockErr          error
	penUpAttempted     bool
	penUpErr           error
	penUpSkippedReason string
	finalConfirmed     bool
	finalState         string
	finalStateErr      error
}

type machineRecovery struct {
	sender  *grbl.Sender
	machine *machine.Machine
	pen     *machine.Pen
	log     io.Writer
	timings recoveryTimings

	mu       sync.Mutex
	done     chan struct{}
	report   recoveryReport
	finished bool
}

func newMachineRecovery(sender *grbl.Sender, robot *machine.Machine, pen *machine.Pen, log io.Writer, timings recoveryTimings) *machineRecovery {
	if log == nil {
		log = io.Discard
	}
	return &machineRecovery{sender: sender, machine: robot, pen: pen, log: log, timings: timings}
}

func recoverAfterInterrupt(ctx context.Context, recovery *machineRecovery, err error) {
	if err == nil || recovery == nil || ctx.Err() == nil {
		return
	}
	recovery.interrupt()
}

func (r *machineRecovery) interrupt() recoveryReport {
	return r.run(true)
}

func (r *machineRecovery) penUpOnly() recoveryReport {
	return r.run(false)
}

func (r *machineRecovery) run(feedHold bool) recoveryReport {
	r.mu.Lock()
	if r.done != nil {
		done := r.done
		r.mu.Unlock()
		<-done
		r.mu.Lock()
		report := r.report
		r.mu.Unlock()
		return report
	}
	done := make(chan struct{})
	r.done = done
	r.mu.Unlock()

	var report recoveryReport
	if feedHold {
		report.feedHoldSent = true
		report.feedHoldErr = r.sender.FeedHold()
		if report.feedHoldErr != nil {
			fmt.Fprintf(r.log, "Recovery: feed hold could not be sent: %v\n", report.feedHoldErr)
		} else {
			fmt.Fprintln(r.log, "Recovery: feed hold sent.")
			state, _, err := r.waitForState(r.timings.holdWait, "Hold", "Idle")
			if err != nil {
				report.stopStateErr = err
				fmt.Fprintf(r.log, "Recovery: GRBL stop/Hold state could not be confirmed: %v\n", err)
			} else {
				report.stopStateConfirmed = true
				report.stopState = state
				fmt.Fprintf(r.log, "Recovery: GRBL reported state %s.\n", state)
			}
		}
	}

	needsReset := feedHold && (!report.stopStateConfirmed || stateBase(report.stopState) != "Idle")
	if needsReset {
		report.resetSent = true
		resetCtx, cancel := context.WithTimeout(context.Background(), r.timings.resetWait)
		report.resetErr = r.sender.SoftReset(resetCtx)
		cancel()
		if report.resetErr != nil {
			if strings.Contains(report.resetErr.Error(), "soft reset:") {
				fmt.Fprintf(r.log, "Recovery: GRBL reset could not be sent: %v\n", report.resetErr)
			} else {
				fmt.Fprintf(r.log, "Recovery: GRBL reset sent, but startup was not confirmed: %v\n", report.resetErr)
			}
			report.penUpSkippedReason = "ordinary G-code may not execute safely after feed hold when reset fails"
			fmt.Fprintf(r.log, "Recovery: pen-up was not attempted because %s.\n", report.penUpSkippedReason)
			return r.finish(report)
		}
		fmt.Fprintln(r.log, "Recovery: GRBL reset sent and startup confirmed.")

		report.unlockAttempted = true
		unlockCtx, cancel := context.WithTimeout(context.Background(), r.timings.commandWait)
		report.unlockErr = r.sender.Command(unlockCtx, "$X")
		cancel()
		if report.unlockErr != nil {
			fmt.Fprintf(r.log, "Recovery: GRBL alarm unlock was attempted but failed: %v\n", report.unlockErr)
		}
	}

	report.penUpAttempted = true
	fmt.Fprintf(r.log, "Recovery: pen-up attempted at Z%.3f.\n", r.pen.UpZ())
	if err := r.runWithCommandTimeout(r.machine.SetUnitsMillimeters); err != nil {
		report.penUpErr = fmt.Errorf("set millimeters before pen-up: %w", err)
		fmt.Fprintf(r.log, "Recovery: pen-up failed: %v\n", report.penUpErr)
		return r.finish(report)
	}
	if err := r.runWithCommandTimeout(r.pen.Raise); err != nil {
		report.penUpErr = err
		fmt.Fprintf(r.log, "Recovery: pen-up failed: %v\n", err)
		return r.finish(report)
	}
	fmt.Fprintln(r.log, "Recovery: pen-up command accepted by GRBL.")

	state, _, err := r.waitForState(r.timings.finalStateWait, "Idle")
	if err != nil {
		report.finalStateErr = err
		fmt.Fprintf(r.log, "Recovery: final machine state could not be confirmed: %v\n", err)
	} else {
		report.finalConfirmed = true
		report.finalState = state
		fmt.Fprintf(r.log, "Recovery: final GRBL state confirmed as %s.\n", state)
	}

	return r.finish(report)
}

func (r *machineRecovery) runWithCommandTimeout(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.timings.commandWait)
	defer cancel()
	return fn(ctx)
}

func (r *machineRecovery) waitForState(timeout time.Duration, states ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.sender.WaitForState(ctx, states...)
}

func (r *machineRecovery) finish(report recoveryReport) recoveryReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report = report
	r.finished = true
	close(r.done)
	return report
}

func stateBase(state string) string {
	if i := strings.IndexByte(state, ':'); i >= 0 {
		return state[:i]
	}
	return state
}

func calibrate(ctx context.Context, sender *grbl.Sender, robot *machine.Machine, pen *machine.Pen, cfg *config.Config, penStep, positionStep float64, recovery *machineRecovery) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("standard input is not an interactive terminal")
	}
	if err := robot.SetUnitsMillimeters(ctx); err != nil {
		return err
	}
	if err := sender.Command(ctx, "G90"); err != nil {
		return err
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw terminal mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	if err := calibratePen(ctx, pen, cfg, penStep, recovery); err != nil {
		return err
	}
	return calibrateStartPosition(ctx, sender, robot, pen, cfg, positionStep, recovery)
}

func calibratePen(ctx context.Context, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery) error {
	return calibratePenFrom(ctx, pen, cfg, step, recovery, os.Stdin)
}

func calibratePenFrom(ctx context.Context, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery, input io.Reader) (err error) {
	z := cfg.PenDown
	if err := pen.MoveTo(ctx, z, machine.DefaultPenLowerFeed); err != nil {
		return err
	}
	penDown := true
	defer func() {
		if err != nil && penDown && recovery != nil {
			recovery.penUpOnly()
		}
	}()

	fmt.Printf("\r\nStep 1 of 2: pen-down calibration (current Z%.3f)\r\n", z)
	fmt.Printf("Down arrow: lower (+%.3f mm) | Up arrow: raise (-%.3f mm) | Enter: accept | Esc/Ctrl-C: cancel\r\n", step, step)
	for {
		key, err := readKey(input)
		if err != nil {
			return err
		}
		switch key {
		case keyEnter:
			cfg.PenDown = z
			pen.SetDownZ(z)
			if err := pen.Raise(ctx); err != nil {
				return err
			}
			penDown = false
			fmt.Printf("\r\nSelected pen-down Z%.3f; pen raised.\r\n", z)
			return nil
		case keyCancel:
			return context.Canceled
		case keyUp:
			z -= step
		case keyDown:
			z += step
		default:
			continue
		}
		if z < 0 {
			z = 0
		}
		if err := pen.MoveTo(ctx, z, machine.DefaultPenLowerFeed); err != nil {
			return err
		}
		fmt.Printf("\rZ%.3f   ", z)
	}
}

func calibrateStartPosition(ctx context.Context, streamer gcodeStreamer, robot *machine.Machine, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery) error {
	return calibrateStartPositionFrom(ctx, streamer, robot, pen, cfg, step, recovery, os.Stdin)
}

func calibrateStartPositionFrom(ctx context.Context, streamer gcodeStreamer, robot *machine.Machine, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery, input io.Reader) (err error) {
	x, y := cfg.StartX, cfg.StartY
	penDown := false
	defer func() {
		if err != nil && penDown && recovery != nil {
			recovery.penUpOnly()
		}
	}()
	if err := robot.MoveProgramXYTo(ctx, x, y, machine.ProgramRapid); err != nil {
		return err
	}

	fmt.Printf("\r\nStep 2 of 2: starting-position calibration\r\n")
	fmt.Printf("Arrow keys: move %.3f mm | U: pen up | D: pen down | Enter: set origin | Esc/Ctrl-C: cancel\r\n", step)
	fmt.Printf("X%.3f Y%.3f | pen UP\r\n", x, y)
	for {
		key, err := readKey(input)
		if err != nil {
			return err
		}
		switch key {
		case keyEnter:
			if penDown {
				if err := pen.Raise(ctx); err != nil {
					return err
				}
			}
			cfg.StartX, cfg.StartY = x, y
			if err := robot.SetProgramXYOrigin(ctx); err != nil {
				return err
			}
			fmt.Printf("\r\nSelected start X%.3f Y%.3f; program origin set and pen raised.\r\n", x, y)
			return calibrateDiagnosticsFrom(ctx, streamer, cfg, recovery, input)
		case keyCancel:
			return context.Canceled
		case keyLeft:
			x -= step
		case keyRight:
			x += step
		case keyUp:
			y += step
		case keyDown:
			y -= step
		case keyPenUp:
			if err := pen.Raise(ctx); err != nil {
				return err
			}
			penDown = false
			fmt.Printf("\rX%.3f Y%.3f | pen UP     ", x, y)
			continue
		case keyPenDown:
			if err := pen.Lower(ctx); err != nil {
				return err
			}
			penDown = true
			fmt.Printf("\rX%.3f Y%.3f | pen DOWN   ", x, y)
			continue
		default:
			continue
		}

		if x < 0 {
			x = 0
		}
		if y > 0 {
			y = 0
		}
		move := machine.ProgramRapid
		if penDown {
			move = machine.ProgramLinear
		}
		if err := robot.MoveProgramXYTo(ctx, x, y, move); err != nil {
			return err
		}
		state := "UP"
		if penDown {
			state = "DOWN"
		}
		fmt.Printf("\rX%.3f Y%.3f | pen %-4s   ", x, y, state)
	}
}

func calibrateDiagnosticsFrom(ctx context.Context, streamer gcodeStreamer, cfg *config.Config, recovery *machineRecovery, input io.Reader) error {
	for {
		printDiagnosticMenu()
		key, err := readKey(input)
		if err != nil {
			return err
		}
		if key == keyEnter {
			fmt.Printf("\r\nDiagnostics complete.\r\n")
			return nil
		}
		if key == keyCancel {
			return context.Canceled
		}

		pattern, ok := diagnosticPattern(key)
		if !ok {
			continue
		}
		opts := diagnostics.DefaultOptions(cfg.PenUp, cfg.PenDown)
		lines, err := pattern.Generate(opts)
		if err != nil {
			return fmt.Errorf("generate %s: %w", pattern.Name(), err)
		}
		fmt.Printf("\r\nDrawing %s...\r\n", pattern.Name())
		if err := streamer.Send(ctx, lines); err != nil {
			recoverAfterInterrupt(ctx, recovery, err)
			return fmt.Errorf("draw %s: %w", pattern.Name(), err)
		}
		fmt.Printf("Finished %s; pen raised at program origin.\r\n", pattern.Name())
	}
}

func printDiagnosticMenu() {
	fmt.Printf("\r\nOptional diagnostics from calibrated program origin:\r\n")
	labels := make([]string, 0, len(diagnosticChoices)+1)
	for _, choice := range diagnosticChoices {
		labels = append(labels, choice.label)
	}
	labels = append(labels, "Enter: save", "Esc/Ctrl-C: cancel")
	fmt.Printf("%s\r\n", strings.Join(labels, " | "))
}

func diagnosticPattern(key key) (diagnostics.Pattern, bool) {
	for _, choice := range diagnosticChoices {
		if choice.key == key {
			return choice.pattern, true
		}
	}
	return nil, false
}

type diagnosticChoice struct {
	key     key
	label   string
	pattern diagnostics.Pattern
}

var diagnosticChoices = []diagnosticChoice{
	{key: keyPatternCircles, label: "C: circles", pattern: diagnostics.CirclePattern{}},
	{key: keyPatternSquares, label: "S: squares", pattern: diagnostics.SquarePattern{}},
	{key: keyPatternTriangles, label: "T: triangles", pattern: diagnostics.TrianglePattern{}},
	{key: keyPatternSine, label: "V: sine waves", pattern: diagnostics.SinePattern{}},
	{key: keyPatternGrid, label: "G: grid", pattern: diagnostics.GridPattern{}},
	{key: keyPatternCrosshair, label: "X: crosshair", pattern: diagnostics.CrosshairPattern{}},
}

type key int

const (
	keyUnknown key = iota
	keyEnter
	keyCancel
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPenUp
	keyPenDown
	keyPatternCircles
	keyPatternSquares
	keyPatternTriangles
	keyPatternSine
	keyPatternGrid
	keyPatternCrosshair
)

func readKey(r io.Reader) (key, error) {
	var first [1]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return keyUnknown, err
	}
	switch first[0] {
	case '\r', '\n':
		return keyEnter, nil
	case 3:
		return keyCancel, nil
	case 'u', 'U':
		return keyPenUp, nil
	case 'd', 'D':
		return keyPenDown, nil
	case 'c', 'C':
		return keyPatternCircles, nil
	case 's', 'S':
		return keyPatternSquares, nil
	case 't', 'T':
		return keyPatternTriangles, nil
	case 'v', 'V':
		return keyPatternSine, nil
	case 'g', 'G':
		return keyPatternGrid, nil
	case 'x', 'X':
		return keyPatternCrosshair, nil
	case 27:
		var seq [2]byte
		if _, err := io.ReadFull(r, seq[:]); err != nil {
			return keyCancel, nil
		}
		if seq[0] != '[' {
			return keyCancel, nil
		}
		switch seq[1] {
		case 'A':
			return keyUp, nil
		case 'B':
			return keyDown, nil
		case 'C':
			return keyRight, nil
		case 'D':
			return keyLeft, nil
		}
		return keyUnknown, nil
	default:
		return keyUnknown, nil
	}
}

func fail(action string, err error) int {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "Interrupted.")
		return 130
	}
	if errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "%s: serial connection closed\n", action)
		return 1
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	return 1
}
