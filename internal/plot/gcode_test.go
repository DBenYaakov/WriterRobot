package plot_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/geometry"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
	"github.com/DBenYaakov/WriterRobot/internal/plot"
	svgimport "github.com/DBenYaakov/WriterRobot/internal/svg"
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

func TestGeneratedGCodeUsesFixedDrawFeedByDefault(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 30, Y: 0}, {X: 40, Y: 0}, {X: 50, Y: 0}}},
	})

	lines := mustProgram(t, d, plot.DefaultOptions(0.5, 1.7))

	want := []string{
		"G1 X10.000 Y0.000 F600",
		"G1 X20.000 Y0.000",
		"G1 X30.000 Y0.000",
		"G1 X40.000 Y0.000",
		"G1 X50.000 Y0.000",
	}
	if got := drawCommands(lines); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("draw commands = %v, want %v", got, want)
	}
}

func TestGeneratedGCodeSignatureModeEmitsDrawFeedOnlyWhenItChanges(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 30, Y: 0}, {X: 40, Y: 0}, {X: 50, Y: 0}}},
	})

	opts := plot.DefaultOptions(0.5, 1.7)
	opts.ModulateDrawFeed = true
	lines := mustProgram(t, d, opts)

	want := []string{
		"G1 X10.000 Y0.000 F240",
		"G1 X20.000 Y0.000",
		"G1 X30.000 Y0.000 F600",
		"G1 X40.000 Y0.000 F810",
		"G1 X50.000 Y0.000",
	}
	if got := drawCommands(lines); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("draw commands = %v, want %v", got, want)
	}
}

func TestGeneratedGCodeSignatureFeedModulationRegression(t *testing.T) {
	doc, err := svgimport.ParseFile(filepath.Join("..", "..", "testdata", "svg", "17-inkscape-signature.svg"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	geometryOpts := geometry.DefaultOptions()
	geometryOpts.FitWidth = 80
	result, err := geometry.Process(geometry.Source{
		Drawing: doc.Drawing,
		Width:   doc.Width,
		Height:  doc.Height,
		ViewBox: geometryViewBox(doc),
	}, geometryOpts)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	plotOpts := plot.DefaultOptions(0.5, 1.7)
	plotOpts.ModulateDrawFeed = true
	ops, err := plot.Plan(result, plotOpts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	lines, err := machine.ProgramFromPlan(ops)
	if err != nil {
		t.Fatalf("ProgramFromPlan: %v", err)
	}

	if got := countCommand(lines, "G4"); got != 0 {
		t.Fatalf("dwell commands = %d, want 0", got)
	}
	if countDrawFeed(lines, "F240") == 0 {
		t.Fatal("signature G-code has no slow drawing feed")
	}
	if countDrawFeed(lines, "F600") == 0 {
		t.Fatal("signature G-code has no normal drawing feed")
	}
	if countDrawFeed(lines, "F810") == 0 {
		t.Fatal("signature G-code has no fast drawing feed")
	}
	if hasExcessiveDrawFeedChatter(lines) {
		t.Fatal("signature G-code contains excessive alternating feed chatter")
	}

	again, err := machine.ProgramFromPlan(ops)
	if err != nil {
		t.Fatalf("ProgramFromPlan second run: %v", err)
	}
	if strings.Join(lineCommands(lines), "\n") != strings.Join(lineCommands(again), "\n") {
		t.Fatal("signature G-code is not deterministic")
	}
}

func geometryViewBox(doc svgimport.Document) *geometry.Rect {
	if doc.ViewBox == nil {
		return nil
	}
	return &geometry.Rect{
		MinX:   doc.ViewBox.MinX,
		MinY:   doc.ViewBox.MinY,
		Width:  doc.ViewBox.Width,
		Height: doc.ViewBox.Height,
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

func lineCommands(lines []gcode.Line) []string {
	commands := make([]string, len(lines))
	for i, line := range lines {
		commands[i] = line.Command
	}
	return commands
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

func drawCommands(lines []gcode.Line) []string {
	var commands []string
	for _, line := range lines {
		if strings.HasPrefix(line.Command, "G1 X") {
			commands = append(commands, line.Command)
		}
	}
	return commands
}

func countCommand(lines []gcode.Line, command string) int {
	count := 0
	for _, line := range lines {
		if line.Command == command {
			count++
		}
	}
	return count
}

func countDrawFeed(lines []gcode.Line, feed string) int {
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line.Command, "G1 X") && strings.Contains(line.Command, feed) {
			count++
		}
	}
	return count
}

func hasExcessiveDrawFeedChatter(lines []gcode.Line) bool {
	var feeds []string
	for _, line := range lines {
		if !strings.HasPrefix(line.Command, "G1 X") || !strings.Contains(line.Command, " F") {
			continue
		}
		feeds = append(feeds, line.Command[strings.LastIndex(line.Command, " F")+1:])
	}
	for i := 0; i+4 < len(feeds); i++ {
		if feeds[i] == "F810" &&
			feeds[i+1] == "F240" &&
			feeds[i+2] == "F810" &&
			feeds[i+3] == "F240" &&
			feeds[i+4] == "F810" {
			return true
		}
		if feeds[i] == "F240" &&
			feeds[i+1] == "F810" &&
			feeds[i+2] == "F240" &&
			feeds[i+3] == "F810" &&
			feeds[i+4] == "F240" {
			return true
		}
	}
	return false
}
