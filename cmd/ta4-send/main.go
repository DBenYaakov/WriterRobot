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
	"github.com/DBenYaakov/WriterRobot/internal/console"
	"github.com/DBenYaakov/WriterRobot/internal/diagnostics"
	"github.com/DBenYaakov/WriterRobot/internal/drawing"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/geometry"
	"github.com/DBenYaakov/WriterRobot/internal/grbl"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
	"github.com/DBenYaakov/WriterRobot/internal/plot"
	"github.com/DBenYaakov/WriterRobot/internal/session"
	svgimport "github.com/DBenYaakov/WriterRobot/internal/svg"
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
		svgPath         = flag.String("svg", "", "local SVG file to import and plot")
		svgTolerance    = flag.Float64("svg-tolerance", 0.10, "SVG curve flattening tolerance in SVG units")
		svgFitWidth     = flag.Float64("svg-fit-width", 0, "fit imported SVG to this width in millimeters; 0 uses work width")
		svgFitHeight    = flag.Float64("svg-fit-height", 0, "fit imported SVG to this height in millimeters; 0 uses work height")
		svgAnchor       = flag.String("svg-anchor", "top-left", "SVG placement anchor: top-left or center")
		workWidth       = flag.Float64("work-width", 100, "maximum drawable width in millimeters")
		workHeight      = flag.Float64("work-height", 100, "maximum drawable height in millimeters")
		drawFeed        = flag.Float64("draw-feed", 600, "drawing feed rate in millimeters per minute")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] file.gcode\n       %s [options] --svg drawing.svg\n       %s --port DEVICE --calibrate\n\n", os.Args[0], os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	log := console.New(os.Stdout)
	svgMode := *svgPath != ""

	if *calibrateMode {
		if flag.NArg() != 0 || svgMode {
			flag.Usage()
			return 2
		}
	} else if svgMode {
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
		return fail(log, "load configuration", err)
	}

	var lines []gcode.Line
	var inputDescription string
	if !*calibrateMode {
		if svgMode {
			program, err := prepareSVGProgram(*svgPath, cfg, svgProgramOptions{
				tolerance:  *svgTolerance,
				fitWidth:   *svgFitWidth,
				fitHeight:  *svgFitHeight,
				anchor:     *svgAnchor,
				workWidth:  *workWidth,
				workHeight: *workHeight,
				drawFeed:   *drawFeed,
			})
			if err != nil {
				return fail(log, "prepare SVG", err)
			}
			lines = program.lines
			logSVGPreparation(log, program)
			bounds := program.bounds
			inputDescription = fmt.Sprintf("SVG %s (bounds X%.3f..%.3f Y%.3f..%.3f)", *svgPath, bounds.MinX, bounds.MaxX, bounds.MinY, bounds.MaxY)
		} else {
			lines, err = loadGCodeProgram(flag.Arg(0), cfg)
			if err != nil {
				return fail(log, "prepare G-code", err)
			}
			inputDescription = flag.Arg(0)
		}
	}

	mode := &serial.Mode{BaudRate: *baud}
	port, err := serial.Open(*portName, mode)
	if err != nil {
		return fail(log, "open serial port", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sender := grbl.New(port, grbl.Options{CommandTimeout: *commandTO, IdleTimeout: *idleTO, ResetOnOpen: *reset, HomeOnStart: *home, StartupDwell: *startupDwell, Verbose: *verbose, Log: os.Stdout, Logger: log})
	defer sender.Close()
	robot := machine.New(sender)
	drawingSession := session.New(robot)
	pen := machine.NewPen(robot, cfg.PenUp, cfg.PenDown)
	recovery := newMachineRecovery(sender, drawingSession, pen, log, defaultRecoveryTimings)

	if *calibrateMode {
		if *calibrationStep <= 0 {
			return fail(log, "calibrate", errors.New("--calibration-step must be greater than zero"))
		}
		if *positionStep <= 0 {
			return fail(log, "calibrate", errors.New("--position-step must be greater than zero"))
		}
		if err := sender.Initialize(ctx); err != nil {
			return fail(log, "initialize GRBL", err)
		}
		if err := calibrate(ctx, sender, drawingSession, robot, pen, &cfg, *calibrationStep, *positionStep, recovery, log); err != nil {
			return fail(log, "calibrate", err)
		}
		path, err := config.Save(cfg)
		if err != nil {
			return fail(log, "save configuration", err)
		}
		log.Info(fmt.Sprintf("Saved calibration to %s", path))
		log.Info(fmt.Sprintf("Pen up Z%.3f, pen down Z%.3f, start X%.3f Y%.3f", cfg.PenUp, cfg.PenDown, cfg.StartX, cfg.StartY))
		return 0
	}

	log.Info(fmt.Sprintf("Using configuration %s (pen up Z%.3f, pen down Z%.3f, start X%.3f Y%.3f)", cfgPath, cfg.PenUp, cfg.PenDown, cfg.StartX, cfg.StartY))
	log.Info(fmt.Sprintf("Sending %d commands from %s to %s at %d baud", len(lines), inputDescription, *portName, *baud))
	if err := sender.Initialize(ctx); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail(log, "initialize GRBL", err)
	}
	if err := drawingSession.Begin(ctx, cfg.StartX, cfg.StartY); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail(log, "begin session", err)
	}
	log.Info(fmt.Sprintf("Paper origin set to machine X%.3f Y%.3f; program coordinates are now relative to that point.", cfg.StartX, cfg.StartY))
	if err := sender.Send(ctx, lines); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail(log, "stream G-code", err)
	}
	if *waitIdle {
		log.Info("Waiting for machine to become Idle...")
		if err := sender.WaitIdle(ctx); err != nil {
			recoverAfterInterrupt(ctx, recovery, err)
			return fail(log, "wait for completion", err)
		}
	}
	if err := drawingSession.End(ctx); err != nil {
		recoverAfterInterrupt(ctx, recovery, err)
		return fail(log, "end session", err)
	}
	log.Info("Session ended; temporary program origin cleared.")
	log.Info("Done.")
	return 0
}

type svgProgramOptions struct {
	tolerance  float64
	fitWidth   float64
	fitHeight  float64
	anchor     string
	workWidth  float64
	workHeight float64
	drawFeed   float64
}

type svgProgram struct {
	lines        []gcode.Line
	bounds       drawing.Bounds
	sourceBounds drawing.Bounds
	scale        float64
	strokes      int
}

func loadGCodeProgram(path string, cfg config.Config) ([]gcode.Line, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open G-code: %w", err)
	}
	defer file.Close()

	lines, err := gcode.Read(file)
	if err != nil {
		return nil, fmt.Errorf("parse G-code: %w", err)
	}
	if len(lines) == 0 {
		return nil, errors.New("parse G-code: file contains no commands")
	}
	for i := range lines {
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{PEN_DOWN}}", fmt.Sprintf("%.3f", cfg.PenDown))
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{PEN_UP}}", fmt.Sprintf("%.3f", cfg.PenUp))
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{START_X}}", fmt.Sprintf("%.3f", cfg.StartX))
		lines[i].Command = strings.ReplaceAll(lines[i].Command, "{{START_Y}}", fmt.Sprintf("%.3f", cfg.StartY))
	}
	return lines, nil
}

func loadSVGProgram(path string, cfg config.Config, opts svgProgramOptions) ([]gcode.Line, drawing.Bounds, error) {
	program, err := prepareSVGProgram(path, cfg, opts)
	if err != nil {
		return nil, drawing.Bounds{}, err
	}
	return program.lines, program.bounds, nil
}

func prepareSVGProgram(path string, cfg config.Config, opts svgProgramOptions) (svgProgram, error) {
	if opts.tolerance <= 0 {
		return svgProgram{}, errors.New("SVG tolerance must be greater than zero")
	}
	if opts.workWidth <= 0 || opts.workHeight <= 0 {
		return svgProgram{}, errors.New("work width and height must be greater than zero")
	}
	if opts.drawFeed <= 0 {
		return svgProgram{}, errors.New("draw feed must be greater than zero")
	}

	doc, err := svgimport.ParseFile(path)
	if err != nil {
		return svgProgram{}, err
	}
	geometryOpts := geometry.DefaultOptions()
	geometryOpts.FlattenTolerance = opts.tolerance
	geometryOpts.FitWidth = opts.fitWidth
	geometryOpts.FitHeight = opts.fitHeight
	geometryOpts.Anchor = geometry.Anchor(opts.anchor)
	geometryOpts.FitToWorkArea = true
	geometryOpts.WorkWidth = opts.workWidth
	geometryOpts.WorkHeight = opts.workHeight
	result, err := geometry.ProcessWithReport(svgGeometrySource(doc), geometryOpts)
	if err != nil {
		return svgProgram{}, err
	}
	d := result.Drawing

	workBounds := geometry.WorkBounds(opts.workWidth, opts.workHeight)
	if err := geometry.Preflight(d, workBounds); err != nil {
		return svgProgram{}, err
	}
	plotOpts := plot.DefaultOptions(cfg.PenUp, cfg.PenDown)
	plotOpts.DrawFeed = opts.drawFeed
	ops, err := plot.Plan(d, plotOpts)
	if err != nil {
		return svgProgram{}, err
	}
	lines, err := machine.ProgramFromPlan(ops)
	if err != nil {
		return svgProgram{}, err
	}
	return svgProgram{
		lines:        lines,
		bounds:       result.FinalBounds,
		sourceBounds: result.SourceBounds,
		scale:        result.Scale,
		strokes:      len(d.Strokes),
	}, nil
}

func logSVGPreparation(log console.Logger, program svgProgram) {
	log.Info(fmt.Sprintf("SVG source bounds: X%.3f..%.3f Y%.3f..%.3f", program.sourceBounds.MinX, program.sourceBounds.MaxX, program.sourceBounds.MinY, program.sourceBounds.MaxY))
	log.Info(fmt.Sprintf("SVG scale: %.6f", program.scale))
	log.Info(fmt.Sprintf("SVG final bounds: X%.3f..%.3f Y%.3f..%.3f", program.bounds.MinX, program.bounds.MaxX, program.bounds.MinY, program.bounds.MaxY))
	log.Info(fmt.Sprintf("SVG strokes: %d", program.strokes))
}

func svgGeometrySource(doc svgimport.Document) geometry.Source {
	source := geometry.Source{
		Drawing: doc.Drawing,
		Width:   doc.Width,
		Height:  doc.Height,
	}
	if doc.ViewBox != nil {
		source.ViewBox = &geometry.Rect{
			MinX:   doc.ViewBox.MinX,
			MinY:   doc.ViewBox.MinY,
			Width:  doc.ViewBox.Width,
			Height: doc.ViewBox.Height,
		}
	}
	return source
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
	session *session.Session
	pen     *machine.Pen
	log     console.Logger
	timings recoveryTimings

	mu       sync.Mutex
	done     chan struct{}
	report   recoveryReport
	finished bool
}

func newMachineRecovery(sender *grbl.Sender, drawingSession *session.Session, pen *machine.Pen, log console.Logger, timings recoveryTimings) *machineRecovery {
	if log == nil {
		log = console.New(io.Discard)
	}
	return &machineRecovery{sender: sender, session: drawingSession, pen: pen, log: log, timings: timings}
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
			r.log.Warn(fmt.Sprintf("Recovery: feed hold could not be sent: %v", report.feedHoldErr))
		} else {
			r.log.Info("Recovery: feed hold sent.")
			state, _, err := r.waitForState(r.timings.holdWait, "Hold", "Idle")
			if err != nil {
				report.stopStateErr = err
				r.log.Warn(fmt.Sprintf("Recovery: GRBL stop/Hold state could not be confirmed: %v", err))
			} else {
				report.stopStateConfirmed = true
				report.stopState = state
				r.log.Info(fmt.Sprintf("Recovery: GRBL reported state %s.", state))
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
				r.log.Warn(fmt.Sprintf("Recovery: GRBL reset could not be sent: %v", report.resetErr))
			} else {
				r.log.Warn(fmt.Sprintf("Recovery: GRBL reset sent, but startup was not confirmed: %v", report.resetErr))
			}
			report.penUpSkippedReason = "ordinary G-code may not execute safely after feed hold when reset fails"
			r.log.Warn(fmt.Sprintf("Recovery: pen-up was not attempted because %s.", report.penUpSkippedReason))
			return r.finish(report)
		}
		r.log.Info("Recovery: GRBL reset sent and startup confirmed.")

		report.unlockAttempted = true
		unlockCtx, cancel := context.WithTimeout(context.Background(), r.timings.commandWait)
		report.unlockErr = r.sender.Command(unlockCtx, "$X")
		cancel()
		if report.unlockErr != nil {
			r.log.Warn(fmt.Sprintf("Recovery: GRBL alarm unlock was attempted but failed: %v", report.unlockErr))
		}
	}

	report.penUpAttempted = true
	r.log.Info(fmt.Sprintf("Recovery: pen-up attempted at Z%.3f.", r.pen.UpZ()))
	if err := r.runWithCommandTimeout(r.session.PrepareInterruptedRecovery); err != nil {
		report.penUpErr = fmt.Errorf("prepare modal state before pen-up: %w", err)
		r.log.Warn(fmt.Sprintf("Recovery: pen-up failed: %v", report.penUpErr))
		return r.finish(report)
	}
	if err := r.runWithCommandTimeout(r.pen.Raise); err != nil {
		report.penUpErr = err
		r.log.Warn(fmt.Sprintf("Recovery: pen-up failed: %v", err))
		return r.finish(report)
	}
	r.log.Info("Recovery: pen-up command accepted by GRBL.")

	state, _, err := r.waitForState(r.timings.finalStateWait, "Idle")
	if err != nil {
		report.finalStateErr = err
		r.log.Warn(fmt.Sprintf("Recovery: final machine state could not be confirmed: %v", err))
	} else {
		report.finalConfirmed = true
		report.finalState = state
		r.log.Info(fmt.Sprintf("Recovery: final GRBL state confirmed as %s.", state))
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

func calibrate(ctx context.Context, sender *grbl.Sender, drawingSession *session.Session, robot *machine.Machine, pen *machine.Pen, cfg *config.Config, penStep, positionStep float64, recovery *machineRecovery, log console.Logger) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("standard input is not an interactive terminal")
	}
	if err := drawingSession.EstablishModalState(ctx); err != nil {
		return err
	}
	if err := robot.ClearProgramOffset(ctx); err != nil {
		return err
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw terminal mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	if err := calibratePen(ctx, pen, cfg, penStep, recovery, log); err != nil {
		return err
	}
	return calibrateStartPosition(ctx, sender, drawingSession, robot, pen, cfg, positionStep, recovery, log)
}

func calibratePen(ctx context.Context, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery, log console.Logger) error {
	return calibratePenFrom(ctx, pen, cfg, step, recovery, os.Stdin, log)
}

func calibratePenFrom(ctx context.Context, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery, input io.Reader, log console.Logger) (err error) {
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

	log.Info(fmt.Sprintf("Step 1 of 2: pen-down calibration (current Z%.3f)", z))
	log.Info(fmt.Sprintf("Down arrow: lower (+%.3f mm) | Up arrow: raise (-%.3f mm) | Enter: accept | Esc/Ctrl-C: cancel", step, step))
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
			log.Info(fmt.Sprintf("Selected pen-down Z%.3f; pen raised.", z))
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
		log.Info(fmt.Sprintf("Pen-down candidate Z%.3f", z))
	}
}

func calibrateStartPosition(ctx context.Context, streamer gcodeStreamer, drawingSession *session.Session, robot *machine.Machine, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery, log console.Logger) error {
	return calibrateStartPositionFrom(ctx, streamer, drawingSession, robot, pen, cfg, step, recovery, os.Stdin, log)
}

func calibrateStartPositionFrom(ctx context.Context, streamer gcodeStreamer, drawingSession *session.Session, robot *machine.Machine, pen *machine.Pen, cfg *config.Config, step float64, recovery *machineRecovery, input io.Reader, log console.Logger) (err error) {
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

	log.Info("Step 2 of 2: starting-position calibration")
	log.Info(fmt.Sprintf("Arrow keys: move %.3f mm | U: pen up | D: pen down | Enter: set origin | Esc/Ctrl-C: cancel", step))
	log.Info(fmt.Sprintf("Start candidate X%.3f Y%.3f | pen UP", x, y))
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
				penDown = false
			}
			cfg.StartX, cfg.StartY = x, y
			if err := drawingSession.Begin(ctx, x, y); err != nil {
				return err
			}
			log.Info(fmt.Sprintf("Selected start X%.3f Y%.3f; program origin set and pen raised.", x, y))
			diagnosticsErr := calibrateDiagnosticsFrom(ctx, streamer, cfg, recovery, input, log)
			var endErr error
			if ctx.Err() == nil {
				endErr = drawingSession.End(ctx)
			}
			if diagnosticsErr != nil || endErr != nil {
				return errors.Join(diagnosticsErr, endErr)
			}
			log.Info("Calibration drawing session ended; temporary program origin cleared.")
			return nil
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
			log.Info(fmt.Sprintf("Start candidate X%.3f Y%.3f | pen UP", x, y))
			continue
		case keyPenDown:
			if err := pen.Lower(ctx); err != nil {
				return err
			}
			penDown = true
			log.Info(fmt.Sprintf("Start candidate X%.3f Y%.3f | pen DOWN", x, y))
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
		log.Info(fmt.Sprintf("Start candidate X%.3f Y%.3f | pen %s", x, y, state))
	}
}

func calibrateDiagnosticsFrom(ctx context.Context, streamer gcodeStreamer, cfg *config.Config, recovery *machineRecovery, input io.Reader, log console.Logger) error {
	for {
		printDiagnosticMenu(log)
		key, err := readKey(input)
		if err != nil {
			return err
		}
		if key == keyEnter {
			log.Info("Diagnostics complete.")
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
		log.Info(fmt.Sprintf("Drawing %s...", pattern.Name()))
		if err := streamer.Send(ctx, lines); err != nil {
			recoverAfterInterrupt(ctx, recovery, err)
			return fmt.Errorf("draw %s: %w", pattern.Name(), err)
		}
		log.Info(fmt.Sprintf("Finished %s; pen raised at program origin.", pattern.Name()))
	}
}

func printDiagnosticMenu(log console.Logger) {
	log.Info("Optional diagnostics from calibrated program origin:")
	labels := make([]string, 0, len(diagnosticChoices)+1)
	for _, choice := range diagnosticChoices {
		labels = append(labels, choice.label)
	}
	labels = append(labels, "Enter: save", "Esc/Ctrl-C: cancel")
	log.Info(strings.Join(labels, " | "))
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

func fail(log console.Logger, action string, err error) int {
	if log == nil {
		log = console.New(os.Stderr)
	}
	if errors.Is(err, context.Canceled) {
		log.Warn("Interrupted.")
		return 130
	}
	if errors.Is(err, io.EOF) {
		log.Error(fmt.Errorf("%s: serial connection closed", action))
		return 1
	}
	log.Error(fmt.Errorf("%s: %w", action, err))
	return 1
}
