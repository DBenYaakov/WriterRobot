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
	"syscall"
	"time"

	"github.com/DBenYaakov/WriterRobot/internal/config"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/grbl"
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
		if err := calibrate(ctx, sender, &cfg, *calibrationStep, *positionStep); err != nil {
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
		return fail("initialize GRBL", err)
	}
	if err := sender.Send(ctx, lines); err != nil {
		return fail("stream G-code", err)
	}
	if *waitIdle {
		fmt.Println("Waiting for machine to become Idle...")
		if err := sender.WaitIdle(ctx); err != nil {
			return fail("wait for completion", err)
		}
	}
	fmt.Println("Done.")
	return 0
}

func calibrate(ctx context.Context, sender *grbl.Sender, cfg *config.Config, penStep, positionStep float64) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("standard input is not an interactive terminal")
	}
	if err := sender.Command(ctx, "G21"); err != nil {
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

	if err := calibratePen(ctx, sender, cfg, penStep); err != nil {
		return err
	}
	return calibrateStartPosition(ctx, sender, cfg, positionStep)
}

func calibratePen(ctx context.Context, sender *grbl.Sender, cfg *config.Config, step float64) error {
	z := cfg.PenDown
	if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F200", z)); err != nil {
		return err
	}

	fmt.Printf("\r\nStep 1 of 2: pen-down calibration (current Z%.3f)\r\n", z)
	fmt.Printf("Down arrow: lower (+%.3f mm) | Up arrow: raise (-%.3f mm) | Enter: accept | Esc/Ctrl-C: cancel\r\n", step, step)
	for {
		key, err := readKey(os.Stdin)
		if err != nil {
			return err
		}
		switch key {
		case keyEnter:
			cfg.PenDown = z
			if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F300", cfg.PenUp)); err != nil {
				return err
			}
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
		if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F200", z)); err != nil {
			return err
		}
		fmt.Printf("\rZ%.3f   ", z)
	}
}

func calibrateStartPosition(ctx context.Context, sender *grbl.Sender, cfg *config.Config, step float64) error {
	x, y := cfg.StartX, cfg.StartY
	penDown := false
	if err := sender.Command(ctx, fmt.Sprintf("G0 X%.3f Y%.3f", x, y)); err != nil {
		return err
	}

	fmt.Printf("\r\nStep 2 of 2: starting-position calibration\r\n")
	fmt.Printf("Arrow keys: move %.3f mm | U: pen up | D: pen down | Enter: save | Esc/Ctrl-C: cancel\r\n", step)
	fmt.Printf("X%.3f Y%.3f | pen UP\r\n", x, y)
	for {
		key, err := readKey(os.Stdin)
		if err != nil {
			return err
		}
		switch key {
		case keyEnter:
			if penDown {
				if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F300", cfg.PenUp)); err != nil {
					return err
				}
			}
			cfg.StartX, cfg.StartY = x, y
			fmt.Printf("\r\nSelected start X%.3f Y%.3f; pen raised.\r\n", x, y)
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
			if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F300", cfg.PenUp)); err != nil {
				return err
			}
			penDown = false
			fmt.Printf("\rX%.3f Y%.3f | pen UP     ", x, y)
			continue
		case keyPenDown:
			if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F200", cfg.PenDown)); err != nil {
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
		move := "G0"
		if penDown {
			move = "G1"
		}
		if err := sender.Command(ctx, fmt.Sprintf("%s X%.3f Y%.3f", move, x, y)); err != nil {
			return err
		}
		state := "UP"
		if penDown {
			state = "DOWN"
		}
		fmt.Printf("\rX%.3f Y%.3f | pen %-4s   ", x, y, state)
	}
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
