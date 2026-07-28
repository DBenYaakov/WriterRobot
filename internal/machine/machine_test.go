package machine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMoveZToEmitsAbsoluteZWithFeed(t *testing.T) {
	rec := &recordingCommander{}
	robot := New(rec)

	if err := robot.MoveZTo(context.Background(), 1.234, 300); err != nil {
		t.Fatalf("MoveZTo: %v", err)
	}
	assertCommands(t, rec, "G1 Z1.234 F300")
}

func TestMoveZToFormatsNonIntegerFeed(t *testing.T) {
	rec := &recordingCommander{}
	robot := New(rec)

	if err := robot.MoveZTo(context.Background(), 1.2, 123.456); err != nil {
		t.Fatalf("MoveZTo: %v", err)
	}
	assertCommands(t, rec, "G1 Z1.200 F123.456")
}

func TestMoveProgramXYToEmitsProgramCoordinateMove(t *testing.T) {
	rec := &recordingCommander{}
	robot := New(rec)

	if err := robot.MoveProgramXYTo(context.Background(), 10.125, -20.5, ProgramRapid); err != nil {
		t.Fatalf("MoveProgramXYTo rapid: %v", err)
	}
	if err := robot.MoveProgramXYTo(context.Background(), 11, -21, ProgramLinear); err != nil {
		t.Fatalf("MoveProgramXYTo linear: %v", err)
	}
	assertCommands(t, rec,
		"G0 X10.125 Y-20.500",
		"G1 X11.000 Y-21.000",
	)
}

func TestMoveMachineXYToEmitsMachineCoordinateMove(t *testing.T) {
	rec := &recordingCommander{}
	robot := New(rec)

	if err := robot.MoveMachineXYTo(context.Background(), 10.125, -20.5); err != nil {
		t.Fatalf("MoveMachineXYTo: %v", err)
	}
	assertCommands(t, rec, "G53 G0 X10.125 Y-20.500")
}

func TestSetProgramXYOriginEmitsG92(t *testing.T) {
	rec := &recordingCommander{}
	robot := New(rec)

	if err := robot.SetProgramXYOrigin(context.Background()); err != nil {
		t.Fatalf("SetProgramXYOrigin: %v", err)
	}
	assertCommands(t, rec, "G92 X0 Y0")
}

func TestModalAndOffsetCommands(t *testing.T) {
	rec := &recordingCommander{}
	robot := New(rec)

	if err := robot.SetUnitsMillimeters(context.Background()); err != nil {
		t.Fatalf("SetUnitsMillimeters: %v", err)
	}
	if err := robot.SetAbsolutePositioning(context.Background()); err != nil {
		t.Fatalf("SetAbsolutePositioning: %v", err)
	}
	if err := robot.SelectXYPlane(context.Background()); err != nil {
		t.Fatalf("SelectXYPlane: %v", err)
	}
	if err := robot.SetFeedRateUnitsPerMinute(context.Background()); err != nil {
		t.Fatalf("SetFeedRateUnitsPerMinute: %v", err)
	}
	if err := robot.SelectDefaultWorkCoordinateSystem(context.Background()); err != nil {
		t.Fatalf("SelectDefaultWorkCoordinateSystem: %v", err)
	}
	if err := robot.ClearProgramOffset(context.Background()); err != nil {
		t.Fatalf("ClearProgramOffset: %v", err)
	}
	assertCommands(t, rec,
		"G21",
		"G90",
		"G17",
		"G94",
		"G54",
		"G92.1",
	)
}

func TestPenRaiseUsesConfiguredPenUp(t *testing.T) {
	rec := &recordingCommander{}
	pen := NewPen(New(rec), 0.5, 1.7)

	if err := pen.Raise(context.Background()); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	assertCommands(t, rec, "G1 Z0.500 F300")
}

func TestPenLowerUsesConfiguredPenDown(t *testing.T) {
	rec := &recordingCommander{}
	pen := NewPen(New(rec), 0.5, 1.7)

	if err := pen.Lower(context.Background()); err != nil {
		t.Fatalf("Lower: %v", err)
	}
	assertCommands(t, rec, "G1 Z1.700 F200")
}

func TestPenMoveToUsesArbitraryAbsoluteZ(t *testing.T) {
	rec := &recordingCommander{}
	pen := NewPen(New(rec), 0.5, 1.7)

	if err := pen.MoveTo(context.Background(), 1.9, DefaultPenLowerFeed); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	assertCommands(t, rec, "G1 Z1.900 F200")
}

func TestPenMoveToDoesNotChangeConfiguredPenDown(t *testing.T) {
	rec := &recordingCommander{}
	pen := NewPen(New(rec), 0.5, 1.7)

	if err := pen.MoveTo(context.Background(), 1.9, DefaultPenLowerFeed); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if err := pen.Lower(context.Background()); err != nil {
		t.Fatalf("Lower: %v", err)
	}
	assertCommands(t, rec,
		"G1 Z1.900 F200",
		"G1 Z1.700 F200",
	)
}

func TestPenDownZCanBeUpdatedAfterCalibration(t *testing.T) {
	rec := &recordingCommander{}
	pen := NewPen(New(rec), 0.5, 1.7)

	pen.SetDownZ(1.9)
	if err := pen.Lower(context.Background()); err != nil {
		t.Fatalf("Lower: %v", err)
	}
	assertCommands(t, rec, "G1 Z1.900 F200")
}

func TestErrorsWrapPhysicalOperation(t *testing.T) {
	wantErr := errors.New("serial failed")
	rec := &recordingCommander{err: wantErr}
	robot := New(rec)

	err := robot.MoveMachineXYTo(context.Background(), 10.125, -20.5)
	if !errors.Is(err, wantErr) {
		t.Fatalf("MoveMachineXYTo error = %v, want wrapped serial error", err)
	}
	if !strings.Contains(err.Error(), "move machine XY to X10.125 Y-20.500") {
		t.Fatalf("MoveMachineXYTo error missing operation context: %v", err)
	}
}

type recordingCommander struct {
	commands []string
	err      error
}

func (r *recordingCommander) Command(_ context.Context, command string) error {
	r.commands = append(r.commands, command)
	return r.err
}

func assertCommands(t *testing.T, rec *recordingCommander, want ...string) {
	t.Helper()
	if len(rec.commands) != len(want) {
		t.Fatalf("commands = %v, want %v", rec.commands, want)
	}
	for i := range want {
		if rec.commands[i] != want[i] {
			t.Fatalf("command %d = %q, want %q", i, rec.commands[i], want[i])
		}
	}
}
