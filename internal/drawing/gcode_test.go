package drawing

import (
	"math"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/gcode"
)

func TestGenerateGCodeUsesSafePenSequencing(t *testing.T) {
	d := mustDrawing(t, []Stroke{
		{Points: []Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []Point{{X: 20, Y: -5}, {X: 30, Y: -5}}},
	})
	opts := DefaultOptions(0.5, 1.7)

	lines, err := GenerateGCode(d, opts)
	if err != nil {
		t.Fatalf("GenerateGCode: %v", err)
	}

	want := []string{
		"G1 Z0.500 F300",
		"G0 X0.000 Y0.000",
		"G1 Z1.700 F200",
		"G1 X10.000 Y0.000 F600",
		"G1 Z0.500 F300",
		"G0 X20.000 Y-5.000",
		"G1 Z1.700 F200",
		"G1 X30.000 Y-5.000 F600",
		"G1 Z0.500 F300",
		"G0 X0.000 Y0.000",
		"G1 Z0.500 F300",
	}
	assertCommands(t, linesToCommands(lines), want)
	assertNoRapidWhilePenDown(t, linesToCommands(lines))
}

func TestGenerateGCodeClosesClosedStroke(t *testing.T) {
	d := mustDrawing(t, []Stroke{
		{Points: []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: -10}}, Closed: true},
	})
	lines, err := GenerateGCode(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("GenerateGCode: %v", err)
	}
	if !containsCommand(linesToCommands(lines), "G1 X0.000 Y0.000") {
		t.Fatalf("closed stroke did not return to its first point: %v", linesToCommands(lines))
	}
}

func TestGenerateGCodeRejectsOutOfBoundsDrawingBeforeCommands(t *testing.T) {
	d := mustDrawing(t, []Stroke{
		{Points: []Point{{X: 0, Y: 0}, {X: 101, Y: 0}}},
	})
	_, err := GenerateGCode(d, DefaultOptions(0.5, 1.7))
	if err == nil {
		t.Fatal("GenerateGCode succeeded for out-of-bounds drawing")
	}
	if !strings.Contains(err.Error(), "exceed work bounds") {
		t.Fatalf("error = %v, want work-bounds context", err)
	}
}

func TestNewRejectsInvalidGeometry(t *testing.T) {
	tests := []struct {
		name    string
		strokes []Stroke
	}{
		{name: "empty", strokes: nil},
		{name: "one point", strokes: []Stroke{{Points: []Point{{X: 0, Y: 0}}}}},
		{name: "nan", strokes: []Stroke{{Points: []Point{{X: 0, Y: 0}, {X: math.NaN(), Y: 0}}}}},
		{name: "infinity", strokes: []Stroke{{Points: []Point{{X: 0, Y: 0}, {X: 1, Y: math.Inf(1)}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.strokes); err == nil {
				t.Fatal("New succeeded for invalid geometry")
			}
		})
	}
}

func TestPreflightComputesBounds(t *testing.T) {
	d := mustDrawing(t, []Stroke{
		{Points: []Point{{X: 5, Y: -2}, {X: 25, Y: -12}}},
	})
	if err := Preflight(d, WorkBounds(30, 20)); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
}

func mustDrawing(t *testing.T, strokes []Stroke) Drawing {
	t.Helper()
	d, err := New(strokes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func linesToCommands(lines []gcode.Line) []string {
	commands := make([]string, 0, len(lines))
	for _, line := range lines {
		commands = append(commands, line.Command)
	}
	return commands
}

func assertCommands(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func containsCommand(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

func assertNoRapidWhilePenDown(t *testing.T, commands []string) {
	t.Helper()
	penDown := false
	for _, command := range commands {
		switch {
		case strings.HasPrefix(command, "G1 Z1.700"):
			penDown = true
		case strings.HasPrefix(command, "G1 Z0.500"):
			penDown = false
		case strings.HasPrefix(command, "G0 ") && penDown:
			t.Fatalf("rapid move while pen is down: %q", command)
		}
	}
	if penDown {
		t.Fatal("program ended with pen down")
	}
}
