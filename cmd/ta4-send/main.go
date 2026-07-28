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
		calibrate       = flag.Bool("calibrate", false, "interactively calibrate and save the pen-down Z position")
		calibrationStep = flag.Float64("calibration-step", 0.05, "Z movement in millimeters for each calibration arrow press")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] file.gcode\n       %s --port DEVICE --calibrate\n\n", os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *calibrate {
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

	if *calibrate {
		if *calibrationStep <= 0 {
			return fail("calibrate", errors.New("--calibration-step must be greater than zero"))
		}
		if err := sender.Initialize(ctx); err != nil {
			return fail("initialize GRBL", err)
		}
		if err := calibratePen(ctx, sender, &cfg, *calibrationStep); err != nil {
			return fail("calibrate", err)
		}
		path, err := config.Save(cfg)
		if err != nil {
			return fail("save configuration", err)
		}
		fmt.Printf("Saved pen-down position Z%.3f to %s\n", cfg.PenDown, path)
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
	}

	fmt.Printf("Using configuration %s (pen up Z%.3f, pen down Z%.3f)\n", cfgPath, cfg.PenUp, cfg.PenDown)
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

func calibratePen(ctx context.Context, sender *grbl.Sender, cfg *config.Config, step float64) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("standard input is not an interactive terminal")
	}
	if err := sender.Command(ctx, "G21"); err != nil {
		return err
	}
	if err := sender.Command(ctx, "G90"); err != nil {
		return err
	}
	z := cfg.PenDown
	if err := sender.Command(ctx, fmt.Sprintf("G1 Z%.3f F200", z)); err != nil {
		return err
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw terminal mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Printf("\r\nPen-down calibration (current Z%.3f)\r\n", z)
	fmt.Printf("Down arrow: lower pen (+%.3f mm) | Up arrow: raise pen (-%.3f mm) | Enter: save | Esc/Ctrl-C: cancel\r\n", step, step)
	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf[:1])
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		switch buf[0] {
		case '\r', '\n':
			cfg.PenDown = z
			fmt.Printf("\r\nSelected Z%.3f\r\n", z)
			return nil
		case 3:
			return context.Canceled
		case 27:
			if _, err := io.ReadFull(os.Stdin, buf[1:3]); err != nil {
				return err
			}
			if buf[1] != '[' {
				return context.Canceled
			}
			switch buf[2] {
			case 'A':
				z -= step
			case 'B':
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
