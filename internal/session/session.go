// Package session defines the GRBL state lifecycle for a drawing session.
//
// WriterRobot uses G92 as a temporary per-session program-origin offset. GRBL
// documents G92 as non-persistent across reset or power cycling, but it remains
// active during the current parser session until cleared or reset. Each drawing
// session therefore clears stale G92 state before recreating the paper origin
// from calibrated machine coordinates, and clears it again after normal
// completion.
//
// Normal plotting completion leaves the temporary G92 offset cleared and GRBL in
// millimeters, absolute positioning, XY plane, units-per-minute feed mode, and
// G54. When configured, normal completion also raises the pen and returns to the
// configured machine home using G53 machine coordinates. Machine home is
// distinct from the calibrated paper origin. The session lifecycle intentionally
// does not erase persistent G54-G59 work offsets.
package session

import (
	"context"
	"fmt"
	"math"
)

// Machine is the machine-command surface required for session lifecycle.
type Machine interface {
	SetUnitsMillimeters(context.Context) error
	SetAbsolutePositioning(context.Context) error
	SelectXYPlane(context.Context) error
	SetFeedRateUnitsPerMinute(context.Context) error
	SelectDefaultWorkCoordinateSystem(context.Context) error
	ClearProgramOffset(context.Context) error
	MoveZTo(context.Context, float64, float64) error
	MoveMachineXYTo(context.Context, float64, float64) error
	WaitIdle(context.Context) error
	SetProgramXYOrigin(context.Context) error
}

// EndOptions controls optional motion performed after successful drawing.
type EndOptions struct {
	PenUpZ                 float64
	PenRaiseFeed           float64
	ReturnHomeOnCompletion bool
	MachineHomeX           float64
	MachineHomeY           float64
}

// Options configures session lifecycle behavior.
type Options struct {
	End EndOptions
}

// Session owns the modal state and temporary coordinate-origin lifecycle for a
// drawing session.
type Session struct {
	machine Machine
	opts    Options
}

// New returns a session lifecycle wrapper around machine.
func New(machine Machine, options ...Options) *Session {
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	return &Session{machine: machine, opts: opts}
}

// EstablishModalState sets the GRBL parser modes that WriterRobot depends on.
func (s *Session) EstablishModalState(ctx context.Context) error {
	steps := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "set units to millimeters", run: s.machine.SetUnitsMillimeters},
		{name: "set absolute positioning", run: s.machine.SetAbsolutePositioning},
		{name: "select XY plane", run: s.machine.SelectXYPlane},
		{name: "set feed rate mode", run: s.machine.SetFeedRateUnitsPerMinute},
		{name: "select G54 work coordinate system", run: s.machine.SelectDefaultWorkCoordinateSystem},
	}
	for _, step := range steps {
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("establish modal state: %s: %w", step.name, err)
		}
	}
	return nil
}

// Begin starts a drawing session at the calibrated paper origin.
func (s *Session) Begin(ctx context.Context, originX, originY float64) error {
	if err := s.EstablishModalState(ctx); err != nil {
		return err
	}
	if err := s.machine.ClearProgramOffset(ctx); err != nil {
		return fmt.Errorf("begin session: clear stale program offset: %w", err)
	}
	if err := s.machine.MoveMachineXYTo(ctx, originX, originY); err != nil {
		return fmt.Errorf("begin session: move to paper origin: %w", err)
	}
	if err := s.machine.SetProgramXYOrigin(ctx); err != nil {
		return fmt.Errorf("begin session: set paper origin: %w", err)
	}
	return nil
}

// End clears the temporary program offset, restores the known modal state, and
// performs configured completion motion after normal completion.
func (s *Session) End(ctx context.Context) error {
	if err := validateEndOptions(s.opts.End); err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	if err := s.machine.ClearProgramOffset(ctx); err != nil {
		return fmt.Errorf("end session: clear program offset: %w", err)
	}
	if err := s.EstablishModalState(ctx); err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	if s.opts.End.ReturnHomeOnCompletion {
		if err := s.machine.MoveZTo(ctx, s.opts.End.PenUpZ, s.opts.End.PenRaiseFeed); err != nil {
			return fmt.Errorf("end session: raise pen before returning home: %w", err)
		}
		if err := s.machine.MoveMachineXYTo(ctx, s.opts.End.MachineHomeX, s.opts.End.MachineHomeY); err != nil {
			return fmt.Errorf("end session: return to machine home: %w", err)
		}
		if err := s.machine.WaitIdle(ctx); err != nil {
			return fmt.Errorf("end session: wait for machine home: %w", err)
		}
	}
	return nil
}

// PrepareInterruptedRecovery restores the parser state needed before issuing a
// recovery pen-up move. It is safe after a soft reset and also handles the Idle
// recovery path where no reset was required.
func (s *Session) PrepareInterruptedRecovery(ctx context.Context) error {
	if err := s.EstablishModalState(ctx); err != nil {
		return fmt.Errorf("prepare interrupted recovery: %w", err)
	}
	if err := s.machine.ClearProgramOffset(ctx); err != nil {
		return fmt.Errorf("prepare interrupted recovery: clear program offset: %w", err)
	}
	return nil
}

func validateEndOptions(opts EndOptions) error {
	if !opts.ReturnHomeOnCompletion {
		return nil
	}
	if !isFinite(opts.PenUpZ) {
		return fmt.Errorf("pen-up Z must be finite")
	}
	if opts.PenRaiseFeed <= 0 || !isFinite(opts.PenRaiseFeed) {
		return fmt.Errorf("pen raise feed must be finite and greater than zero")
	}
	if !isFinite(opts.MachineHomeX) || !isFinite(opts.MachineHomeY) {
		return fmt.Errorf("machine home coordinates must be finite")
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
