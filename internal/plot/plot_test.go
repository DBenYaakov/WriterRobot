package plot

import (
	"math"
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

func TestPlanMergesStrokesWithIdenticalEndpoints(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 20, Y: 0}}},
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
		{Kind: OperationDrawMove, Point: drawing.Point{X: 20, Y: 0}},
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
		{Kind: OperationRapidMove, Point: drawing.Point{}},
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
	}
	assertOperations(t, ops, want)
}

func TestPlanMergesStrokesWithinTolerance(t *testing.T) {
	delta := DefaultContiguousTolerance / 2
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 10 + delta, Y: delta}, {X: 20, Y: 0}}},
	})

	ops, err := Plan(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got := countKind(ops, OperationPenDown); got != 1 {
		t.Fatalf("pen-down operations = %d, want 1", got)
	}
	if got := countKind(ops, OperationRapidMove); got != 2 {
		t.Fatalf("rapid moves = %d, want first stroke plus return to origin", got)
	}
}

func TestPlanDoesNotMergeStrokesOutsideTolerance(t *testing.T) {
	delta := DefaultContiguousTolerance * 2
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 10 + delta, Y: 0}, {X: 20, Y: 0}}},
	})

	ops, err := Plan(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got := countKind(ops, OperationPenDown); got != 2 {
		t.Fatalf("pen-down operations = %d, want 2", got)
	}
	if got := countKind(ops, OperationRapidMove); got != 3 {
		t.Fatalf("rapid moves = %d, want two strokes plus return to origin", got)
	}
}

func TestPlanDoesNotMergeClosedPaths(t *testing.T) {
	tests := []struct {
		name    string
		strokes []drawing.Stroke
	}{
		{
			name: "closed first",
			strokes: []drawing.Stroke{
				{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 0, Y: 0}}, Closed: true},
				{Points: []drawing.Point{{X: 0, Y: 0}, {X: 20, Y: 0}}},
			},
		},
		{
			name: "closed second",
			strokes: []drawing.Stroke{
				{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
				{Points: []drawing.Point{{X: 10, Y: 0}, {X: 20, Y: 0}, {X: 10, Y: 0}}, Closed: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := Plan(mustDrawing(t, tt.strokes), DefaultOptions(0.5, 1.7))
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if got := countKind(ops, OperationPenDown); got != 2 {
				t.Fatalf("pen-down operations = %d, want 2", got)
			}
		})
	}
}

func TestPlanDoesNotMergeNonConsecutiveCompatibleStrokes(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 30, Y: 0}, {X: 40, Y: 0}}},
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 20, Y: 0}}},
	})

	ops, err := Plan(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := countKind(ops, OperationPenDown); got != 3 {
		t.Fatalf("pen-down operations = %d, want 3", got)
	}
}

func TestPlanMergesContiguousStrokesBeforeNearestSelection(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 50, Y: 0}, {X: 60, Y: 0}}},
		{Points: []drawing.Point{{X: 60, Y: 0}, {X: 70, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}, {X: 50, Y: 0}})
	if got := countKind(ops, OperationPenDown); got != 2 {
		t.Fatalf("pen-down operations = %d, want 2 merged drawing operations", got)
	}
	if containsRapidMove(ops, drawing.Point{X: 60, Y: 0}) {
		t.Fatalf("contiguous stroke boundary became a rapid move: %+v", ops)
	}
}

func TestPlanPreservesMergedStrokePointOrder(t *testing.T) {
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}},
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 20, Y: -5}}},
		{Points: []drawing.Point{{X: 20, Y: -5}, {X: 30, Y: 0}}},
	})

	ops, err := Plan(d, DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	wantDraws := []drawing.Point{{X: 10, Y: 0}, {X: 20, Y: -5}, {X: 30, Y: 0}}
	if got := drawMovePoints(ops); !samePoints(got, wantDraws) {
		t.Fatalf("draw move order = %+v, want %+v", got, wantDraws)
	}
}

func TestPlanSelectsNearestStroke(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 110, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 3, Y: 0}, {X: 4, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}, {X: 3, Y: 0}, {X: 100, Y: 0}})
}

func TestPlanBreaksNearestNeighborTiesByDocumentOrder(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 11, Y: 0}}},
		{Points: []drawing.Point{{X: 0, Y: 10}, {X: 0, Y: 11}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := rapidMovePoints(ops)[0], (drawing.Point{X: 10, Y: 0}); got != want {
		t.Fatalf("first selected stroke start = %+v, want document-order tie winner %+v", got, want)
	}
}

func TestPlanNearestNeighborUsesStrokeReversal(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 110, Y: 0}}},
		{Points: []drawing.Point{{X: 50, Y: 0}, {X: 1, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}, {X: 100, Y: 0}})
	assertPoints(t, firstN(drawMovePoints(ops), 1), []drawing.Point{{X: 50, Y: 0}})
}

func TestPlanUsesOriginalStrokeDirectionWhenStartNearer(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 10, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}})
	assertPoints(t, drawMovePoints(ops), []drawing.Point{{X: 10, Y: 0}})
}

func TestPlanReversesOpenStrokeWhenEndNearer(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 1, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}})
	assertPoints(t, drawMovePoints(ops), []drawing.Point{{X: 10, Y: 0}})
}

func TestPlanPreservesOriginalDirectionWhenEndpointDistancesAreEqualWithinTolerance(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	delta := DefaultContiguousTolerance / 2
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 1 - delta, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}})
	assertPoints(t, drawMovePoints(ops), []drawing.Point{{X: 1 - delta, Y: 0}})
}

func TestPlanNeverReversesClosedPaths(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := drawing.Drawing{
		Strokes: []drawing.Stroke{{
			Points: []drawing.Point{{X: 10, Y: 0}, {X: 5, Y: -5}, {X: 1, Y: 0}},
			Closed: true,
		}},
	}

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 10, Y: 0}})
	assertPoints(t, drawMovePoints(ops), []drawing.Point{{X: 5, Y: -5}, {X: 1, Y: 0}, {X: 10, Y: 0}})
}

func TestPlanUsesCurrentPositionWhenReversingStrokeDirection(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 5, Y: 0}}},
		{Points: []drawing.Point{{X: 20, Y: 0}, {X: 6, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 0, Y: 0}, {X: 6, Y: 0}})
	assertPoints(t, drawMovePoints(ops), []drawing.Point{{X: 5, Y: 0}, {X: 20, Y: 0}})
}

func TestPlanPlotsEveryStrokeSegmentExactlyOnce(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 110, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 30, Y: 0}, {X: 31, Y: 0}, {X: 32, Y: 0}}},
		{Points: []drawing.Point{{X: 6, Y: 0}, {X: 5, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	got := drawnSegments(ops)
	want := drawingSegments(d)
	if len(got) != len(want) {
		t.Fatalf("drawn segment count = %d, want %d", len(got), len(want))
	}
	if !sameSegments(got, want) {
		t.Fatalf("drawn segments = %+v, want same geometry as %+v", got, want)
	}
}

func TestPlanReducesPenUpTravelWhenReversing(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 1, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := penUpTravelDistance(ops), 1.0; math.Abs(got-want) > DefaultContiguousTolerance {
		t.Fatalf("pen-up travel = %.6f, want %.6f", got, want)
	}
}

func TestPlanReducesPenUpTravelComparedWithDocumentOrder(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 101, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 3, Y: 0}, {X: 4, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	got := penUpTravelDistance(ops)
	documentOrder := documentOrderPenUpTravel(d, opts)
	if !(got < documentOrder) {
		t.Fatalf("pen-up travel = %.6f, want less than document-order %.6f", got, documentOrder)
	}
}

func TestPlanPreservesGeneratedDrawingGeometryWhenReversing(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	stroke := drawing.Stroke{Points: []drawing.Point{{X: 10, Y: 0}, {X: 20, Y: 0}, {X: 1, Y: 0}}}
	d := mustDrawing(t, []drawing.Stroke{stroke})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := drawnSegments(ops), strokeSegments(stroke); !sameSegments(got, want) {
		t.Fatalf("drawn segments = %+v, want same geometry as %+v", got, want)
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

func containsRapidMove(ops []Operation, want drawing.Point) bool {
	for _, op := range ops {
		if op.Kind == OperationRapidMove && op.Point == want {
			return true
		}
	}
	return false
}

func countKind(ops []Operation, kind OperationKind) int {
	count := 0
	for _, op := range ops {
		if op.Kind == kind {
			count++
		}
	}
	return count
}

func drawMovePoints(ops []Operation) []drawing.Point {
	var points []drawing.Point
	for _, op := range ops {
		if op.Kind == OperationDrawMove {
			points = append(points, op.Point)
		}
	}
	return points
}

func rapidMovePoints(ops []Operation) []drawing.Point {
	var points []drawing.Point
	for _, op := range ops {
		if op.Kind == OperationRapidMove {
			points = append(points, op.Point)
		}
	}
	return points
}

func firstN(points []drawing.Point, n int) []drawing.Point {
	if len(points) < n {
		return points
	}
	return points[:n]
}

func assertPoints(t *testing.T, got, want []drawing.Point) {
	t.Helper()
	if !samePoints(got, want) {
		t.Fatalf("points = %+v, want %+v", got, want)
	}
}

func samePoints(a, b []drawing.Point) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func penUpTravelDistance(ops []Operation) float64 {
	current := drawing.Point{}
	penDown := false
	total := 0.0
	for _, op := range ops {
		switch op.Kind {
		case OperationPenDown:
			penDown = true
		case OperationPenUp:
			penDown = false
		case OperationRapidMove:
			if !penDown {
				total += distance(current, op.Point)
			}
			current = op.Point
		case OperationDrawMove:
			current = op.Point
		}
	}
	return total
}

type testSegment struct {
	a drawing.Point
	b drawing.Point
}

func drawnSegments(ops []Operation) []testSegment {
	current := drawing.Point{}
	penDown := false
	var segments []testSegment
	for _, op := range ops {
		switch op.Kind {
		case OperationPenDown:
			penDown = true
		case OperationPenUp:
			penDown = false
		case OperationRapidMove:
			current = op.Point
		case OperationDrawMove:
			if penDown {
				segments = append(segments, normalizedSegment(current, op.Point))
			}
			current = op.Point
		}
	}
	return segments
}

func strokeSegments(stroke drawing.Stroke) []testSegment {
	segments := make([]testSegment, 0, len(stroke.Points)-1)
	for i := 1; i < len(stroke.Points); i++ {
		segments = append(segments, normalizedSegment(stroke.Points[i-1], stroke.Points[i]))
	}
	return segments
}

func drawingSegments(d drawing.Drawing) []testSegment {
	var segments []testSegment
	for _, stroke := range d.Strokes {
		segments = append(segments, strokeSegments(stroke)...)
	}
	return segments
}

func documentOrderPenUpTravel(d drawing.Drawing, opts Options) float64 {
	opts = opts.withDefaults()
	current := drawing.Point{}
	total := 0.0
	for _, stroke := range mergeContiguousStrokes(d.Strokes, opts.ContiguousTolerance) {
		stroke = orientStroke(stroke, current, opts.ContiguousTolerance)
		total += distance(current, stroke.Points[0])
		current = strokeEnd(stroke)
	}
	if opts.ReturnToOrigin {
		total += distance(current, drawing.Point{})
	}
	return total
}

func normalizedSegment(a, b drawing.Point) testSegment {
	if lessPoint(b, a) {
		a, b = b, a
	}
	return testSegment{a: a, b: b}
}

func lessPoint(a, b drawing.Point) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	return a.Y < b.Y
}

func sameSegments(a, b []testSegment) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[testSegment]int{}
	for _, segment := range a {
		counts[segment]++
	}
	for _, segment := range b {
		counts[segment]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
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
