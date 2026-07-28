package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/machine"
)

func TestEstablishModalStateEmitsExpectedOrder(t *testing.T) {
	rec := &recordingCommander{}
	drawingSession := New(machine.New(rec))

	if err := drawingSession.EstablishModalState(context.Background()); err != nil {
		t.Fatalf("EstablishModalState: %v", err)
	}
	assertCommands(t, rec,
		"G21",
		"G90",
		"G17",
		"G94",
		"G54",
	)
}

func TestBeginRecreatesPaperOriginFromMachineCoordinates(t *testing.T) {
	rec := &recordingCommander{}
	drawingSession := New(machine.New(rec))

	if err := drawingSession.Begin(context.Background(), 10.125, -20.5); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	assertCommands(t, rec,
		"G21",
		"G90",
		"G17",
		"G94",
		"G54",
		"G92.1",
		"G53 G0 X10.125 Y-20.500",
		"G92 X0 Y0",
	)
}

func TestBeginSetsProgramOriginOnlyAfterMachineCoordinateMove(t *testing.T) {
	rec := &recordingCommander{}
	drawingSession := New(machine.New(rec))

	if err := drawingSession.Begin(context.Background(), 10.125, -20.5); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	clearOffsetIndex := indexCommand(rec.commands, "G92.1")
	machineMoveIndex := indexCommand(rec.commands, "G53 G0 X10.125 Y-20.500")
	setOriginIndex := indexCommand(rec.commands, "G92 X0 Y0")
	if clearOffsetIndex < 0 || machineMoveIndex < 0 || setOriginIndex < 0 {
		t.Fatalf("commands missing expected origin lifecycle entries: %v", rec.commands)
	}
	if !(clearOffsetIndex < machineMoveIndex && machineMoveIndex < setOriginIndex) {
		t.Fatalf("origin lifecycle order = %v, want G92.1 before G53 move before G92", rec.commands)
	}
}

func TestEndClearsProgramOffsetAndRestoresModalState(t *testing.T) {
	rec := &recordingCommander{}
	drawingSession := New(machine.New(rec))

	if err := drawingSession.End(context.Background()); err != nil {
		t.Fatalf("End: %v", err)
	}
	assertCommands(t, rec,
		"G92.1",
		"G21",
		"G90",
		"G17",
		"G94",
		"G54",
	)
}

func TestPrepareInterruptedRecoveryRestoresStateAndClearsOffset(t *testing.T) {
	rec := &recordingCommander{}
	drawingSession := New(machine.New(rec))

	if err := drawingSession.PrepareInterruptedRecovery(context.Background()); err != nil {
		t.Fatalf("PrepareInterruptedRecovery: %v", err)
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

func TestBeginWrapsMachineErrors(t *testing.T) {
	wantErr := errors.New("serial failed")
	rec := &recordingCommander{errByCommand: map[string]error{"G92.1": wantErr}}
	drawingSession := New(machine.New(rec))

	err := drawingSession.Begin(context.Background(), 10.125, -20.5)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Begin error = %v, want wrapped serial error", err)
	}
	if !strings.Contains(err.Error(), "begin session: clear stale program offset") {
		t.Fatalf("Begin error missing session context: %v", err)
	}
}

type recordingCommander struct {
	commands     []string
	errByCommand map[string]error
}

func (r *recordingCommander) Command(_ context.Context, command string) error {
	r.commands = append(r.commands, command)
	if r.errByCommand != nil {
		return r.errByCommand[command]
	}
	return nil
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

func indexCommand(commands []string, want string) int {
	for i, command := range commands {
		if command == want {
			return i
		}
	}
	return -1
}
