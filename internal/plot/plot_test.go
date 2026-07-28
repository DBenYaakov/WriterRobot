package plot

import (
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

func TestPlanUsesSafePenSequencing(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 20, Y: -5}, {X: 30, Y: -5}}},
	})

	ops, err := Plan(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []Operation{
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
		{Kind: OperationRapidMove, Point: drawing.Point{X: 0, Y: 0}},
		{Kind: OperationPenDown, Z: 1.7, Feed: 200},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 10, Y: 0}, Feed: 600},
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
		{Kind: OperationRapidMove, Point: drawing.Point{X: 20, Y: -5}},
		{Kind: OperationPenDown, Z: 1.7, Feed: 200},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 30, Y: -5}, Feed: 600},
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
		{Kind: OperationRapidMove, Point: drawing.Point{}},
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
	}
	assertOperations(t, ops, want)
	assertNoRapidWhilePenDown(t, ops)
}

func TestPlanClosesClosedStroke(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: -10}}, Closed: true},
	})
	ops, err := Plan(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !containsDrawMove(ops, drawing.Point{X: 0, Y: 0}) {
		t.Fatalf("closed stroke did not return to its first point: %+v", ops)
	}
}

func TestPlanRejectsInvalidGeometryBeforeOperations(t *testing.T) {
	_, err := Plan(drawing.Drawing{
		Strokes: []drawing.Stroke{{Points: []drawing.Point{{X: 0, Y: 0}}}},
	}, DefaultOptions(0.5, 1.7))
	if err == nil {
		t.Fatal("Plan succeeded for invalid drawing")
	}
	if !strings.Contains(err.Error(), "fewer than two points") {
		t.Fatalf("error = %v, want geometry context", err)
	}
}

func mustDrawing(t *testing.T, strokes []drawing.Stroke) drawing.Drawing {
	t.Helper()
	d, err := drawing.New(strokes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

func assertOperations(t *testing.T, got, want []Operation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("operations = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("operation %d = %+v, want %+v", i+1, got[i], want[i])
		}
	}
}

func containsDrawMove(ops []Operation, want drawing.Point) bool {
	for _, op := range ops {
		if op.Kind == OperationDrawMove && op.Point == want {
			return true
		}
	}
	return false
}

func assertNoRapidWhilePenDown(t *testing.T, ops []Operation) {
	t.Helper()
	penDown := false
	for _, op := range ops {
		switch op.Kind {
		case OperationPenDown:
			if penDown {
				t.Fatal("lowered pen while already down")
			}
			penDown = true
		case OperationPenUp:
			penDown = false
		case OperationRapidMove:
			if penDown {
				t.Fatalf("rapid move while pen is down: %+v", op)
			}
		}
	}
	if penDown {
		t.Fatal("program ended with pen down")
	}
}
