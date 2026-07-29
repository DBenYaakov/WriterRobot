package plot

import (
	"math"
	"reflect"
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

func TestPlanDoesNotSelectNearestStrokeOutsideLookaheadWindow(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 101, Y: 0}}},
		{Points: []drawing.Point{{X: 90, Y: 0}, {X: 91, Y: 0}}},
		{Points: []drawing.Point{{X: 80, Y: 0}, {X: 81, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := rapidMovePoints(ops)[0], (drawing.Point{X: 80, Y: 0}); got != want {
		t.Fatalf("first selected stroke = %+v, want nearest stroke inside lookahead window %+v", got, want)
	}
}

func TestPlanSelectsOldestStrokeAfterMaximumDeferral(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 101, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 3, Y: 0}, {X: 4, Y: 0}}},
		{Points: []drawing.Point{{X: 5, Y: 0}, {X: 6, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, firstN(rapidMovePoints(ops), 3), []drawing.Point{{X: 1, Y: 0}, {X: 3, Y: 0}, {X: 100, Y: 0}})
}

func TestPlanDeferralAccountingOnlyIncrementsEarlierSkippedStrokes(t *testing.T) {
	remaining := []plannedStroke{
		{order: 0},
		{order: 1},
		{order: 2},
		{order: 3},
	}

	got, selected := removeSelectedStroke(remaining, 2)

	if selected.order != 2 {
		t.Fatalf("selected order = %d, want 2", selected.order)
	}
	want := []plannedStroke{
		{order: 0, deferrals: 1},
		{order: 1, deferrals: 1},
		{order: 3, deferrals: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining = %+v, want %+v", got, want)
	}
}

func TestPlanDoesNotBypassAnyStrokeMoreThanMaximumDeferral(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 101, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 3, Y: 0}, {X: 4, Y: 0}}},
		{Points: []drawing.Point{{X: 5, Y: 0}, {X: 6, Y: 0}}},
		{Points: []drawing.Point{{X: 7, Y: 0}, {X: 8, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	position := indexOfPoint(rapidMovePoints(ops), drawing.Point{X: 100, Y: 0})
	if position < 0 {
		t.Fatalf("oldest stroke was not plotted: %+v", rapidMovePoints(ops))
	}
	if position > maximumDeferral {
		t.Fatalf("oldest stroke was bypassed %d times, want at most %d", position, maximumDeferral)
	}
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

func TestPlanComparesClosedPathsFromNearestEntryPoint(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 10, Y: 0}, {X: 11, Y: 0}}},
		{
			Points: []drawing.Point{
				{X: 100, Y: 0},
				{X: 1, Y: 0},
				{X: 100, Y: -10},
				{X: 100, Y: 0},
			},
			Closed: true,
		},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := rapidMovePoints(ops)[0], (drawing.Point{X: 1, Y: 0}); got != want {
		t.Fatalf("first selected entry = %+v, want closed-path nearest entry %+v", got, want)
	}
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

func TestCurvatureTercilesMapBottomMiddleTopToSlowNormalFast(t *testing.T) {
	segments := []drawSegment{
		{curvature: 0.1, length: 10},
		{curvature: 0.5, length: 10},
		{curvature: 1.0, length: 10},
	}

	levels := curvatureTercileLevels(segments)

	want := []drawingFeedLevel{feedSlow, feedNormal, feedFast}
	if !reflect.DeepEqual(levels, want) {
		t.Fatalf("levels = %+v, want %+v", levels, want)
	}
}

func TestPlanUsesFixedDrawFeedByDefault(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 30, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []float64{normalDrawingFeed(opts.DrawFeed), normalDrawingFeed(opts.DrawFeed), normalDrawingFeed(opts.DrawFeed)}
	assertFeeds(t, effectiveDrawFeeds(ops), want)
	if got, want := nonzeroDrawFeedCount(ops), 1; got != want {
		t.Fatalf("draw feed changes = %d, want %d", got, want)
	}
}

func TestPlanModulatesStraightRunByDistanceWeightedCurvatureTerciles(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 30, Y: 0}, {X: 40, Y: 0}, {X: 50, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []float64{
		slowDrawingFeed(opts.DrawFeed),
		slowDrawingFeed(opts.DrawFeed),
		normalDrawingFeed(opts.DrawFeed),
		fastDrawingFeed(opts.DrawFeed),
		fastDrawingFeed(opts.DrawFeed),
	}
	assertFeeds(t, effectiveDrawFeeds(ops), want)
}

func TestPlanUsesFastFeedForHighCurvatureBand(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: -10}, {X: 20, Y: -20}, {X: 30, Y: -20}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if !containsFeed(effectiveDrawFeeds(ops), fastDrawingFeed(opts.DrawFeed)) {
		t.Fatalf("feeds = %+v, want high-curvature band to include fast feed %.3f", effectiveDrawFeeds(ops), fastDrawingFeed(opts.DrawFeed))
	}
}

func TestPlanUsesNormalFeedForMiddleCurvatureBand(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{
			{X: 0, Y: 0},
			{X: 10, Y: 0},
			{X: 18.660, Y: 5},
			{X: 27.320, Y: 10},
			{X: 37.320, Y: 10},
		}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if !containsFeed(effectiveDrawFeeds(ops), normalDrawingFeed(opts.DrawFeed)) {
		t.Fatalf("feeds = %+v, want middle-curvature band to include normal feed %.3f", effectiveDrawFeeds(ops), normalDrawingFeed(opts.DrawFeed))
	}
}

func TestPlanDoesNotTreatClosedPathEntryAsEndpointSlowdown(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{regularClosedPolygon(10, 36)})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	feeds := effectiveDrawFeeds(ops)
	if !containsFeed(feeds, slowDrawingFeed(opts.DrawFeed)) ||
		!containsFeed(feeds, normalDrawingFeed(opts.DrawFeed)) ||
		!containsFeed(feeds, fastDrawingFeed(opts.DrawFeed)) {
		t.Fatalf("closed path feeds = %+v, want histogram-based spread across all feed bands", feeds)
	}
}

func TestPlanUsesHistogramForShortOpenStroke(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 0.25, Y: 0}, {X: 0.50, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []float64{slowDrawingFeed(opts.DrawFeed), normalDrawingFeed(opts.DrawFeed)}
	assertFeeds(t, effectiveDrawFeeds(ops), want)
}

func TestPlanAvoidsFeedChatterAcrossTinyFlattenedSegments(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	points := make([]drawing.Point, 0, 12)
	for i := 0; i < 12; i++ {
		points = append(points, drawing.Point{X: float64(i) * 0.1, Y: 0})
	}
	d := mustDrawing(t, []drawing.Stroke{{Points: points}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := nonzeroDrawFeedCount(ops), 3; got != want {
		t.Fatalf("draw feed changes = %d, want %d", got, want)
	}
	if hasDirectSlowFastFeedTransition(effectiveDrawFeeds(ops), opts.DrawFeed) {
		t.Fatalf("feeds = %+v, want no direct slow/fast chatter", effectiveDrawFeeds(ops))
	}
}

func TestPlanDoesNotTransitionDirectlyFromSlowToFast(t *testing.T) {
	opts := signatureOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 30, Y: 0}, {X: 40, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	feeds := effectiveDrawFeeds(ops)
	if got, want := feeds[0], slowDrawingFeed(opts.DrawFeed); !sameFeed(got, want) {
		t.Fatalf("first feed = %.3f, want slow %.3f", got, want)
	}
	if got, want := feeds[1], normalDrawingFeed(opts.DrawFeed); !sameFeed(got, want) {
		t.Fatalf("second feed = %.3f, want normal %.3f before fast", got, want)
	}
}

func TestAnalyzeDrawFeedsReportsEstimatedTimeByMode(t *testing.T) {
	ops := []Operation{
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
		{Kind: OperationRapidMove, Point: drawing.Point{}},
		{Kind: OperationPenDown, Z: 1.7, Feed: 200},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 10, Y: 0}, Feed: slowDrawingFeed(defaultDrawFeed)},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 20, Y: 0}},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 30, Y: 0}, Feed: normalDrawingFeed(defaultDrawFeed)},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 40, Y: 0}, Feed: fastDrawingFeed(defaultDrawFeed)},
	}

	stats, err := AnalyzeDrawFeeds(ops, defaultDrawFeed)
	if err != nil {
		t.Fatalf("AnalyzeDrawFeeds: %v", err)
	}

	if got, want := stats.Slow.Moves, 2; got != want {
		t.Fatalf("slow moves = %d, want %d", got, want)
	}
	if got, want := stats.Slow.Distance, 20.0; math.Abs(got-want) > DefaultContiguousTolerance {
		t.Fatalf("slow distance = %.6f, want %.6f", got, want)
	}
	if got, want := stats.Slow.Seconds, 20.0/slowDrawingFeed(defaultDrawFeed)*60; math.Abs(got-want) > DefaultContiguousTolerance {
		t.Fatalf("slow seconds = %.6f, want %.6f", got, want)
	}
	if got, want := stats.Normal.Moves, 1; got != want {
		t.Fatalf("normal moves = %d, want %d", got, want)
	}
	if got, want := stats.Fast.Moves, 1; got != want {
		t.Fatalf("fast moves = %d, want %d", got, want)
	}
	if stats.Other.Moves != 0 {
		t.Fatalf("other moves = %d, want 0", stats.Other.Moves)
	}
}

func TestAnalyzeDrawFeedsRejectsMissingEffectiveFeed(t *testing.T) {
	_, err := AnalyzeDrawFeeds([]Operation{
		{Kind: OperationRapidMove, Point: drawing.Point{}},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 10, Y: 0}},
	}, defaultDrawFeed)
	if err == nil {
		t.Fatal("AnalyzeDrawFeeds succeeded without an effective draw feed")
	}
	if !strings.Contains(err.Error(), "no effective feed") {
		t.Fatalf("error = %v, want missing feed context", err)
	}
}

func TestAnalyzeCurvatureReportsHistogramByFeedBand(t *testing.T) {
	ops := []Operation{
		{Kind: OperationPenUp, Z: 0.5, Feed: 300},
		{Kind: OperationRapidMove, Point: drawing.Point{}},
		{Kind: OperationPenDown, Z: 1.7, Feed: 200},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 10, Y: 0}, Feed: slowDrawingFeed(defaultDrawFeed)},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 10, Y: -10}, Feed: normalDrawingFeed(defaultDrawFeed)},
		{Kind: OperationDrawMove, Point: drawing.Point{X: 20, Y: -10}, Feed: fastDrawingFeed(defaultDrawFeed)},
	}

	histogram, err := AnalyzeCurvature(ops, defaultDrawFeed, DefaultContiguousTolerance)
	if err != nil {
		t.Fatalf("AnalyzeCurvature: %v", err)
	}

	if got, want := histogram.Slow.Moves, 1; got != want {
		t.Fatalf("slow moves = %d, want %d", got, want)
	}
	if got, want := histogram.Normal.Moves, 1; got != want {
		t.Fatalf("normal moves = %d, want %d", got, want)
	}
	if got, want := histogram.Fast.Moves, 1; got != want {
		t.Fatalf("fast moves = %d, want %d", got, want)
	}
	if histogram.Normal.MaxDegrees < 89 || histogram.Normal.MaxDegrees > 91 {
		t.Fatalf("normal curvature max degrees = %.3f, want about 90", histogram.Normal.MaxDegrees)
	}
	if got, want := histogram.TotalDistance(), 30.0; math.Abs(got-want) > DefaultContiguousTolerance {
		t.Fatalf("total curvature distance = %.6f, want %.6f", got, want)
	}
}

func TestPlanSelectsNearestClosedPathVertex(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{{
		Points: []drawing.Point{
			{X: 20, Y: 0},
			{X: 20, Y: -10},
			{X: 1, Y: 0},
			{X: 20, Y: 0},
		},
		Closed: true,
	}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}})
}

func TestPlanRotatesClosedPathAndPreservesClosure(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{{
		Points: []drawing.Point{
			{X: 10, Y: 0},
			{X: 10, Y: -10},
			{X: 1, Y: -1},
			{X: 0, Y: -10},
			{X: 10, Y: 0},
		},
		Closed: true,
	}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []drawing.Point{
		{X: 0, Y: -10},
		{X: 10, Y: 0},
		{X: 10, Y: -10},
		{X: 1, Y: -1},
	}
	assertPoints(t, drawMovePoints(ops), want)
	draws := drawMovePoints(ops)
	entry := rapidMovePoints(ops)[0]
	if draws[len(draws)-1] != entry {
		t.Fatalf("rotated closed path ended at %+v, want entry point %+v", draws[len(draws)-1], entry)
	}
}

func TestPlanPreservesClosedPathOrientation(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{{
		Points: []drawing.Point{
			{X: 10, Y: 0},
			{X: 10, Y: -10},
			{X: 1, Y: -1},
			{X: 0, Y: -10},
			{X: 10, Y: 0},
		},
		Closed: true,
	}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []testSegment{
		{a: drawing.Point{X: 1, Y: -1}, b: drawing.Point{X: 0, Y: -10}},
		{a: drawing.Point{X: 0, Y: -10}, b: drawing.Point{X: 10, Y: 0}},
		{a: drawing.Point{X: 10, Y: 0}, b: drawing.Point{X: 10, Y: -10}},
		{a: drawing.Point{X: 10, Y: -10}, b: drawing.Point{X: 1, Y: -1}},
	}
	if got := drawnOrientedSegments(ops); !sameOrientedSegments(got, want) {
		t.Fatalf("oriented segments = %+v, want %+v", got, want)
	}
}

func TestPlanPreservesClosedPathGeometryAfterRotation(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{{
		Points: []drawing.Point{
			{X: 10, Y: 0},
			{X: 10, Y: -10},
			{X: 1, Y: -1},
			{X: 0, Y: -10},
			{X: 10, Y: 0},
		},
		Closed: true,
	}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := drawnSegments(ops), drawingSegments(d); !sameSegments(got, want) {
		t.Fatalf("drawn segments = %+v, want same geometry as %+v", got, want)
	}
}

func TestPlanBreaksClosedPathVertexTiesByOriginalVertexOrder(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{{
		Points: []drawing.Point{
			{X: 1, Y: 0},
			{X: 0, Y: 1},
			{X: 5, Y: 0},
			{X: 1, Y: 0},
		},
		Closed: true,
	}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}})
}

func TestPlanBreaksClosedPathSelectionTiesByDocumentOrder(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{
			Points: []drawing.Point{
				{X: 10, Y: 0},
				{X: 1, Y: 0},
				{X: 10, Y: -10},
				{X: 10, Y: 0},
			},
			Closed: true,
		},
		{
			Points: []drawing.Point{
				{X: -10, Y: 0},
				{X: 0, Y: 1},
				{X: -10, Y: -10},
				{X: -10, Y: 0},
			},
			Closed: true,
		},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got, want := rapidMovePoints(ops)[0], (drawing.Point{X: 1, Y: 0}); got != want {
		t.Fatalf("first selected closed path entry = %+v, want document-order tie winner %+v", got, want)
	}
}

func TestPlanClosedPathEntryDoesNotAffectOpenPaths(t *testing.T) {
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

func TestPlanReducesPenUpTravelToClosedPath(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{{
		Points: []drawing.Point{
			{X: 100, Y: 0},
			{X: 100, Y: -10},
			{X: 1, Y: 0},
			{X: 100, Y: 0},
		},
		Closed: true,
	}})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	got := penUpTravelDistance(ops)
	fixedStart := distance(drawing.Point{}, d.Strokes[0].Points[0])
	if !(got < fixedStart) {
		t.Fatalf("pen-up travel = %.6f, want less than fixed-start %.6f", got, fixedStart)
	}
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

	assertPoints(t, rapidMovePoints(ops), []drawing.Point{{X: 1, Y: 0}})
	assertPoints(t, drawMovePoints(ops), []drawing.Point{{X: 10, Y: 0}, {X: 5, Y: -5}, {X: 1, Y: 0}})
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

func TestPlanConstrainedNearestNeighborKeepsEarlyStrokeNearSourceOrder(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 101, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 3, Y: 0}, {X: 4, Y: 0}}},
		{Points: []drawing.Point{{X: 5, Y: 0}, {X: 6, Y: 0}}},
		{Points: []drawing.Point{{X: 7, Y: 0}, {X: 8, Y: 0}}},
		{Points: []drawing.Point{{X: 9, Y: 0}, {X: 10, Y: 0}}},
	})

	ops, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	position := indexOfPoint(rapidMovePoints(ops), drawing.Point{X: 100, Y: 0})
	if position < 0 {
		t.Fatalf("early source-order stroke was not plotted: %+v", rapidMovePoints(ops))
	}
	if position > maximumDeferral {
		t.Fatalf("early source-order stroke plotted at position %d, want no later than %d", position, maximumDeferral)
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	opts := DefaultOptions(0.5, 1.7)
	opts.ReturnToOrigin = false
	d := mustDrawing(t, []drawing.Stroke{
		{Points: []drawing.Point{{X: 100, Y: 0}, {X: 101, Y: 0}}},
		{Points: []drawing.Point{{X: 1, Y: 0}, {X: 2, Y: 0}}},
		{Points: []drawing.Point{{X: 20, Y: 0}, {X: 4, Y: 0}}},
		{
			Points: []drawing.Point{
				{X: 50, Y: 0},
				{X: 6, Y: 0},
				{X: 50, Y: -10},
				{X: 50, Y: 0},
			},
			Closed: true,
		},
	})

	want, err := Plan(d, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for i := 0; i < 25; i++ {
		got, err := Plan(d, opts)
		if err != nil {
			t.Fatalf("Plan run %d: %v", i+1, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Plan run %d = %+v, want deterministic %+v", i+1, got, want)
		}
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

func signatureOptions(penUpZ, penDownZ float64) Options {
	opts := DefaultOptions(penUpZ, penDownZ)
	opts.ModulateDrawFeed = true
	return opts
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

func effectiveDrawFeeds(ops []Operation) []float64 {
	var feeds []float64
	currentFeed := 0.0
	for _, op := range ops {
		if op.Kind != OperationDrawMove {
			continue
		}
		if op.Feed > 0 {
			currentFeed = op.Feed
		}
		feeds = append(feeds, currentFeed)
	}
	return feeds
}

func nonzeroDrawFeedCount(ops []Operation) int {
	count := 0
	for _, op := range ops {
		if op.Kind == OperationDrawMove && op.Feed > 0 {
			count++
		}
	}
	return count
}

func containsFeed(feeds []float64, want float64) bool {
	for _, feed := range feeds {
		if sameFeed(feed, want) {
			return true
		}
	}
	return false
}

func hasDirectSlowFastFeedTransition(feeds []float64, normalFeed float64) bool {
	slow := slowDrawingFeed(normalFeed)
	fast := fastDrawingFeed(normalFeed)
	for i := 1; i < len(feeds); i++ {
		if (sameFeed(feeds[i-1], slow) && sameFeed(feeds[i], fast)) ||
			(sameFeed(feeds[i-1], fast) && sameFeed(feeds[i], slow)) {
			return true
		}
	}
	return false
}

func firstN(points []drawing.Point, n int) []drawing.Point {
	if len(points) < n {
		return points
	}
	return points[:n]
}

func indexOfPoint(points []drawing.Point, want drawing.Point) int {
	for i, point := range points {
		if point == want {
			return i
		}
	}
	return -1
}

func assertPoints(t *testing.T, got, want []drawing.Point) {
	t.Helper()
	if !samePoints(got, want) {
		t.Fatalf("points = %+v, want %+v", got, want)
	}
}

func assertFeeds(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("feeds = %+v, want %+v", got, want)
	}
	for i := range want {
		if !sameFeed(got[i], want[i]) {
			t.Fatalf("feed %d = %.3f, want %.3f", i+1, got[i], want[i])
		}
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

func regularClosedPolygon(radius float64, vertices int) drawing.Stroke {
	points := make([]drawing.Point, 0, vertices+1)
	for i := 0; i < vertices; i++ {
		angle := 2 * math.Pi * float64(i) / float64(vertices)
		points = append(points, drawing.Point{
			X: radius * math.Cos(angle),
			Y: radius * math.Sin(angle),
		})
	}
	points = append(points, points[0])
	return drawing.Stroke{Points: points, Closed: true}
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

func drawnOrientedSegments(ops []Operation) []testSegment {
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
				segments = append(segments, testSegment{a: current, b: op.Point})
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
	for _, entry := range remainingStrokes(d.Strokes, opts.ContiguousTolerance) {
		stroke := chooseStrokeEntry(entry.stroke, current, opts.ContiguousTolerance)
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

func sameOrientedSegments(a, b []testSegment) bool {
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
