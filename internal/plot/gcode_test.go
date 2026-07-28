package plot_test

import (
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
	"github.com/DBenYaakov/WriterRobot/internal/plot"
)

func TestGeneratedGCodeHasFewerPenTransitionsAfterContiguousMerge(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 10.0002, Y: 0}, {X: 20, Y: 0}}},
	})

	merged := mustProgram(t, d, plot.DefaultOptions(0.5, 1.7))
	noMergeOpts := plot.DefaultOptions(0.5, 1.7)
	noMergeOpts.ContiguousTolerance = 0.00001
	unmerged := mustProgram(t, d, noMergeOpts)

	if got, want := countPrefix(merged, "G1 Z1.700"), countPrefix(unmerged, "G1 Z1.700")-1; got != want {
		t.Fatalf("merged pen-down commands = %d, want %d", got, want)
	}
	if got, want := countPrefix(merged, "G1 Z0.500"), countPrefix(unmerged, "G1 Z0.500")-1; got != want {
		t.Fatalf("merged pen-up commands = %d, want %d", got, want)
	}
}

func TestGeneratedGCodeUsesCloserEndpointForReversedOpenStroke(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 1, Y: 0}}},
	})

	lines := mustProgram(t, d, plot.DefaultOptions(0.5, 1.7))

	if got, want := lines[1].Command, "G0 X1.000 Y0.000"; got != want {
		t.Fatalf("first rapid move = %q, want %q", got, want)
	}
	if got, want := lines[3].Command, "G1 X10.000 Y0.000 F600"; got != want {
		t.Fatalf("draw move = %q, want %q", got, want)
	}
}

func mustProgram(t *testing.T, d drawing.Drawing, opts plot.Options) []gcode.Line {
	t.Helper()
	ops, err := plot.Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	lines, err := machine.ProgramFromPlan(ops)
	if err != nil {
		t.Fatalf("ProgramFromPlan: %v", err)
	}
	return lines
}

func mustDrawing(t *testing.T, strokes []drawing.Stroke) drawing.Drawing {
	t.Helper()
	d, err := drawing.New(strokes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func countPrefix(lines []gcode.Line, prefix string) int {
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line.Command, prefix) {
			count++
		}
	}
	return count
}
