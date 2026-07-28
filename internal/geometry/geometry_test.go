package geometry_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
	"github.com/DBenYaakov/WriterRobot/internal/geometry"
	svgimport "github.com/DBenYaakov/WriterRobot/internal/svg"
)

func TestProcessValidFixtureSuite(t *testing.T) {
	tests := []fixtureCase{
		{
			file:        "01-line.svg",
			strokes:     1,
			closed:      []bool{false},
			bounds:      drawing.Bounds{MinX: 10, MinY: -40, MaxX: 30, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -20}},
			ends:        []drawing.Point{{X: 30, Y: -40}},
			exactPoints: []int{2},
		},
		{
			file:        "02-rectangle.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 10, MinY: -60, MaxX: 40, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -20}},
			ends:        []drawing.Point{{X: 10, Y: -20}},
			exactPoints: []int{5},
		},
		{
			file:        "03-circle.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 20, MinY: -40, MaxX: 40, MaxY: -20},
			starts:      []drawing.Point{{X: 40, Y: -30}},
			ends:        []drawing.Point{{X: 40, Y: -30}},
			exactPoints: []int{25},
		},
		{
			file:        "04-ellipse.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 20, MinY: -45, MaxX: 60, MaxY: -25},
			starts:      []drawing.Point{{X: 60, Y: -35}},
			ends:        []drawing.Point{{X: 60, Y: -35}},
			exactPoints: []int{33},
		},
		{
			file:        "05-polyline.svg",
			strokes:     1,
			closed:      []bool{false},
			bounds:      drawing.Bounds{MinX: 10, MinY: -30, MaxX: 30, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -20}},
			ends:        []drawing.Point{{X: 30, Y: -20}},
			exactPoints: []int{3},
		},
		{
			file:        "06-polygon.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 10, MinY: -30, MaxX: 30, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -20}},
			ends:        []drawing.Point{{X: 10, Y: -20}},
			exactPoints: []int{4},
		},
		{
			file:        "07-triangle.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 10, MinY: -40, MaxX: 30, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -40}},
			ends:        []drawing.Point{{X: 10, Y: -40}},
			exactPoints: []int{4},
		},
		{
			file:      "08-cubic-bezier.svg",
			strokes:   1,
			closed:    []bool{false},
			bounds:    drawing.Bounds{MinX: 10, MinY: -50, MaxX: 50, MaxY: -27.5},
			starts:    []drawing.Point{{X: 10, Y: -50}},
			ends:      []drawing.Point{{X: 50, Y: -50}},
			minPoints: []int{6},
			flattened: true,
		},
		{
			file:      "09-quadratic-bezier.svg",
			strokes:   1,
			closed:    []bool{false},
			bounds:    drawing.Bounds{MinX: 10, MinY: -65, MaxX: 90, MaxY: -35},
			starts:    []drawing.Point{{X: 10, Y: -50}},
			ends:      []drawing.Point{{X: 90, Y: -50}},
			minPoints: []int{6},
			flattened: true,
		},
		{
			file:        "10-relative-path.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 10, MinY: -40, MaxX: 40, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -20}},
			ends:        []drawing.Point{{X: 10, Y: -20}},
			exactPoints: []int{6},
		},
		{
			file:        "11-closed-path.svg",
			strokes:     1,
			closed:      []bool{true},
			bounds:      drawing.Bounds{MinX: 15, MinY: -35, MaxX: 45, MaxY: -15},
			starts:      []drawing.Point{{X: 15, Y: -15}},
			ends:        []drawing.Point{{X: 15, Y: -15}},
			exactPoints: []int{5},
		},
		{
			file:        "12-multiple-strokes.svg",
			strokes:     2,
			closed:      []bool{false, false},
			bounds:      drawing.Bounds{MinX: 10, MinY: -20, MaxX: 70, MaxY: -10},
			starts:      []drawing.Point{{X: 10, Y: -10}, {X: 50, Y: -20}},
			ends:        []drawing.Point{{X: 30, Y: -10}, {X: 70, Y: -20}},
			exactPoints: []int{2, 2},
		},
		{
			file:        "13-transforms.svg",
			strokes:     1,
			closed:      []bool{false},
			bounds:      drawing.Bounds{MinX: 10, MinY: -20, MaxX: 20, MaxY: -20},
			starts:      []drawing.Point{{X: 10, Y: -20}},
			ends:        []drawing.Point{{X: 20, Y: -20}},
			exactPoints: []int{2},
		},
		{
			file:        "14-viewbox-scale.svg",
			strokes:     1,
			closed:      []bool{false},
			bounds:      drawing.Bounds{MinX: 0, MinY: -25, MaxX: 50, MaxY: 0},
			starts:      []drawing.Point{{X: 0, Y: 0}},
			ends:        []drawing.Point{{X: 50, Y: -25}},
			exactPoints: []int{2},
		},
		{
			file:        "15-nested-transforms.svg",
			strokes:     1,
			closed:      []bool{false},
			bounds:      drawing.Bounds{MinX: 20, MinY: -20, MaxX: 40, MaxY: -20},
			starts:      []drawing.Point{{X: 20, Y: -20}},
			ends:        []drawing.Point{{X: 40, Y: -20}},
			exactPoints: []int{2},
		},
		{
			file:      "16-simple-signature.svg",
			strokes:   2,
			closed:    []bool{false, false},
			bounds:    drawing.Bounds{MinX: 10, MinY: -31.406, MaxX: 91, MaxY: -15.156},
			starts:    []drawing.Point{{X: 10, Y: -30}, {X: 75, Y: -30}},
			ends:      []drawing.Point{{X: 70, Y: -30}, {X: 91, Y: -30}},
			minPoints: []int{8, 4},
			flattened: true,
		},
		{
			file:      "hardware-check.svg",
			strokes:   6,
			closed:    []bool{true, true, true, false, false, false},
			bounds:    drawing.Bounds{MinX: 5, MinY: -55, MaxX: 75, MaxY: -5},
			starts:    []drawing.Point{{X: 5, Y: -5}, {X: 53, Y: -13}, {X: 60, Y: -22}, {X: 8, Y: -45}, {X: 55, Y: -45}, {X: 55, Y: -55}},
			ends:      []drawing.Point{{X: 5, Y: -5}, {X: 53, Y: -13}, {X: 60, Y: -22}, {X: 48, Y: -42}, {X: 75, Y: -45}, {X: 75, Y: -55}},
			minPoints: []int{5, 25, 4, 6, 2, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			d := processFixture(t, tt.file, geometry.DefaultOptions())
			assertFixture(t, d, tt)
		})
	}
}

func TestProcessFixtureFitWidthPreservesAspectRatio(t *testing.T) {
	opts := geometry.DefaultOptions()
	opts.FitWidth = 40
	d := processFixture(t, "14-viewbox-scale.svg", opts)
	assertBounds(t, d.Bounds, drawing.Bounds{MinX: 0, MinY: -20, MaxX: 40, MaxY: 0})
}

func TestProcessFixtureCenterAnchor(t *testing.T) {
	opts := geometry.DefaultOptions()
	opts.Anchor = geometry.AnchorCenter
	d := processFixture(t, "01-line.svg", opts)
	assertBounds(t, d.Bounds, drawing.Bounds{MinX: -40, MinY: 10, MaxX: -20, MaxY: 30})
}

func TestPreflightComputesBounds(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 5, Y: -2}, {X: 25, Y: -12}}},
	})
	if err := geometry.Preflight(d, geometry.WorkBounds(30, 20)); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
}

func TestPreflightRejectsOutOfBoundsDrawing(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 101, Y: 0}}},
	})
	if err := geometry.Preflight(d, geometry.WorkBounds(100, 100)); err == nil {
		t.Fatal("Preflight succeeded, want bounds error")
	}
}

func TestProcessHonorsExplicitZeroTransform(t *testing.T) {
	doc, err := svgimport.Parse(strings.NewReader(`<svg><line transform="matrix(0 0 0 0 0 0)" x1="10" y1="20" x2="30" y2="40"/></svg>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, err = geometry.Process(geometrySource(doc), geometry.DefaultOptions())
	if err == nil {
		t.Fatal("Process succeeded, want collapsed transformed bounds error")
	}
}

type fixtureCase struct {
	file        string
	strokes     int
	closed      []bool
	bounds      drawing.Bounds
	starts      []drawing.Point
	ends        []drawing.Point
	exactPoints []int
	minPoints   []int
	flattened   bool
}

func processFixture(t *testing.T, file string, opts geometry.Options) drawing.Drawing {
	t.Helper()
	doc, err := svgimport.ParseFile(fixturePath(file))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	d, err := geometry.Process(geometrySource(doc), opts)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	return d
}

func geometrySource(doc svgimport.Document) geometry.Source {
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

func assertFixture(t *testing.T, d drawing.Drawing, want fixtureCase) {
	t.Helper()
	if len(d.Strokes) != want.strokes {
		t.Fatalf("strokes = %d, want %d", len(d.Strokes), want.strokes)
	}
	assertBounds(t, d.Bounds, want.bounds)
	for i, stroke := range d.Strokes {
		if stroke.Closed != want.closed[i] {
			t.Fatalf("stroke %d closed = %v, want %v", i+1, stroke.Closed, want.closed[i])
		}
		assertPoint(t, stroke.Points[0], want.starts[i], "start")
		assertPoint(t, stroke.Points[len(stroke.Points)-1], want.ends[i], "end")
		if len(want.exactPoints) > i && want.exactPoints[i] > 0 && len(stroke.Points) != want.exactPoints[i] {
			t.Fatalf("stroke %d point count = %d, want %d", i+1, len(stroke.Points), want.exactPoints[i])
		}
		if len(want.minPoints) > i && want.minPoints[i] > 0 && len(stroke.Points) < want.minPoints[i] {
			t.Fatalf("stroke %d point count = %d, want at least %d", i+1, len(stroke.Points), want.minPoints[i])
		}
	}
	if want.flattened && !hasFlattenedStroke(d) {
		t.Fatal("fixture did not produce any flattened curve segments")
	}
}

func hasFlattenedStroke(d drawing.Drawing) bool {
	for _, stroke := range d.Strokes {
		if len(stroke.Points) > 3 {
			return true
		}
	}
	return false
}

func fixturePath(file string) string {
	return filepath.Join("..", "..", "testdata", "svg", file)
}

func mustDrawing(t *testing.T, strokes []drawing.Stroke) drawing.Drawing {
	t.Helper()
	d, err := drawing.New(strokes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func assertBounds(t *testing.T, got, want drawing.Bounds) {
	t.Helper()
	assertAlmost(t, got.MinX, want.MinX, "MinX")
	assertAlmost(t, got.MinY, want.MinY, "MinY")
	assertAlmost(t, got.MaxX, want.MaxX, "MaxX")
	assertAlmost(t, got.MaxY, want.MaxY, "MaxY")
}

func assertPoint(t *testing.T, got, want drawing.Point, name string) {
	t.Helper()
	assertAlmost(t, got.X, want.X, name+" X")
	assertAlmost(t, got.Y, want.Y, name+" Y")
}

func assertAlmost(t *testing.T, got, want float64, name string) {
	t.Helper()
	if got > want+0.01 || got < want-0.01 {
		t.Fatalf("%s = %.3f, want %.3f", name, got, want)
	}
}
