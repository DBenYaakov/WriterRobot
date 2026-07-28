package drawing

import (
	"math"
	"testing"
)

func TestNewComputesBoundsAndClosesClosedStroke(t *testing.T) {
	d := mustDrawing(t, []Stroke{
		{Points: []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: -10}}, Closed: true},
	})

	assertBounds(t, d.Bounds, Bounds{MinX: 0, MinY: -10, MaxX: 10, MaxY: 0})
	points := d.Strokes[0].Points
	if len(points) != 4 {
		t.Fatalf("closed stroke points = %d, want 4", len(points))
	}
	assertPoint(t, points[len(points)-1], Point{X: 0, Y: 0})
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

func TestTransformComposition(t *testing.T) {
	parent := Transform{A: 1, D: 1, E: 10, F: 20}
	child := Transform{A: 2, D: 2}

	got := parent.Then(child).Apply(Point{X: 5, Y: 7})
	assertPoint(t, got, Point{X: 20, Y: 34})
}

func mustDrawing(t *testing.T, strokes []Stroke) Drawing {
	t.Helper()
	d, err := New(strokes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func assertBounds(t *testing.T, got, want Bounds) {
	t.Helper()
	assertAlmost(t, got.MinX, want.MinX, "MinX")
	assertAlmost(t, got.MinY, want.MinY, "MinY")
	assertAlmost(t, got.MaxX, want.MaxX, "MaxX")
	assertAlmost(t, got.MaxY, want.MaxY, "MaxY")
}

func assertPoint(t *testing.T, got, want Point) {
	t.Helper()
	assertAlmost(t, got.X, want.X, "X")
	assertAlmost(t, got.Y, want.Y, "Y")
}

func assertAlmost(t *testing.T, got, want float64, name string) {
	t.Helper()
	if got > want+0.001 || got < want-0.001 {
		t.Fatalf("%s = %.3f, want %.3f", name, got, want)
	}
}
