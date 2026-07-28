package machine

import (
	"context"
	"fmt"
	"math"
)

const (
	// DefaultPenRaiseFeed is the default absolute Z feed for lifting the pen.
	DefaultPenRaiseFeed = 300
	// DefaultPenLowerFeed is the default absolute Z feed for lowering the pen.
	DefaultPenLowerFeed = 200
)

// Commander is the command surface needed to issue ordinary G-code.
type Commander interface {
	Command(context.Context, string) error
}

// ProgramMove selects the absolute program-coordinate X/Y move command.
type ProgramMove string

const (
	// ProgramRapid moves in program coordinates with G0.
	ProgramRapid ProgramMove = "G0"
	// ProgramLinear moves in program coordinates with G1.
	ProgramLinear ProgramMove = "G1"
)

// Machine translates physical machine operations into G-code commands.
type Machine struct {
	commander Commander
}

// New returns a machine command wrapper around commander.
func New(commander Commander) *Machine {
	return &Machine{commander: commander}
}

// SetUnitsMillimeters sets GRBL's distance units to millimeters.
func (m *Machine) SetUnitsMillimeters(ctx context.Context) error {
	if err := m.commander.Command(ctx, "G21"); err != nil {
		return fmt.Errorf("set units to millimeters: %w", err)
	}
	return nil
}

// MoveZTo moves to an absolute Z position at the given feed rate.
func (m *Machine) MoveZTo(ctx context.Context, z, feed float64) error {
	command := fmt.Sprintf("G1 Z%.3f F%s", z, formatFeed(feed))
	if err := m.commander.Command(ctx, command); err != nil {
		return fmt.Errorf("move Z to %.3f at F%s: %w", z, formatFeed(feed), err)
	}
	return nil
}

// MoveProgramXYTo moves to an absolute X/Y position in the current program
// coordinate system.
func (m *Machine) MoveProgramXYTo(ctx context.Context, x, y float64, move ProgramMove) error {
	command := fmt.Sprintf("%s X%.3f Y%.3f", move, x, y)
	if err := m.commander.Command(ctx, command); err != nil {
		return fmt.Errorf("move program XY to X%.3f Y%.3f using %s: %w", x, y, move, err)
	}
	return nil
}

// MoveMachineXYTo moves to an absolute machine-coordinate X/Y position.
func (m *Machine) MoveMachineXYTo(ctx context.Context, x, y float64) error {
	if err := m.commander.Command(ctx, fmt.Sprintf("G53 G0 X%.3f Y%.3f", x, y)); err != nil {
		return fmt.Errorf("move machine XY to X%.3f Y%.3f: %w", x, y, err)
	}
	return nil
}

// SetProgramXYOrigin sets the current program-coordinate X/Y position to zero.
func (m *Machine) SetProgramXYOrigin(ctx context.Context) error {
	if err := m.commander.Command(ctx, "G92 X0 Y0"); err != nil {
		return fmt.Errorf("set current program XY position to X0 Y0: %w", err)
	}
	return nil
}

// Pen owns calibrated pen positions and Z feed rates.
type Pen struct {
	machine  *Machine
	up       float64
	down     float64
	raiseFPM float64
	lowerFPM float64
}

// NewPen returns a pen controller using the default raise/lower Z feeds.
func NewPen(machine *Machine, penUp, penDown float64) *Pen {
	return &Pen{
		machine:  machine,
		up:       penUp,
		down:     penDown,
		raiseFPM: DefaultPenRaiseFeed,
		lowerFPM: DefaultPenLowerFeed,
	}
}

// UpZ returns the calibrated absolute Z position for pen-up.
func (p *Pen) UpZ() float64 {
	return p.up
}

// SetDownZ updates the calibrated absolute Z position for pen-down.
func (p *Pen) SetDownZ(z float64) {
	p.down = z
}

// Raise moves the pen to the calibrated pen-up Z position.
func (p *Pen) Raise(ctx context.Context) error {
	if err := p.MoveTo(ctx, p.up, p.raiseFPM); err != nil {
		return fmt.Errorf("raise pen to Z%.3f: %w", p.up, err)
	}
	return nil
}

// Lower moves the pen to the calibrated pen-down Z position.
func (p *Pen) Lower(ctx context.Context) error {
	if err := p.MoveTo(ctx, p.down, p.lowerFPM); err != nil {
		return fmt.Errorf("lower pen to Z%.3f: %w", p.down, err)
	}
	return nil
}

// MoveTo moves the pen to an arbitrary absolute Z position.
func (p *Pen) MoveTo(ctx context.Context, position, feed float64) error {
	if err := p.machine.MoveZTo(ctx, position, feed); err != nil {
		return fmt.Errorf("move pen to absolute Z%.3f: %w", position, err)
	}
	return nil
}

func formatFeed(feed float64) string {
	if math.Abs(feed-math.Round(feed)) < 0.0005 {
		return fmt.Sprintf("%.0f", feed)
	}
	return fmt.Sprintf("%.3f", feed)
}
