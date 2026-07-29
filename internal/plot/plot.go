// Package plot plans pen and XY motion for processed drawings.
//
// It first merges consecutive open strokes whose endpoints are already
// contiguous, then uses deterministic constrained nearest-neighbor planning. At
// each step it considers only the first three remaining source-order strokes,
// and a stroke that has been bypassed twice becomes mandatory. Each candidate
// open stroke may be drawn in reverse when its endpoint is closer to the current
// pen position. Closed strokes keep their original orientation, but rotate to
// enter at the nearest vertex. Equal-distance ties preserve original document
// order, original stroke direction, and the earliest closed-path vertex. After
// stroke geometry is selected, pen-down moves use the configured fixed drawing
// feed by default. Optional curvature-aware drawing feed modulation ranks local
// curvature across the whole plan by drawing distance: the lower third uses the
// slow feed, the middle third uses the configured normal feed, and the upper
// third uses the fast feed. A small smoothing pass suppresses direct slow/fast
// chatter. The planner inserts pen-up travel between disconnected strokes,
// lowers the pen only for drawing moves, and returns an ordered list of
// operations. It does not format G-code or know about GRBL, sessions, serial
// transport, SVG, or CLI flags.
package plot

import (
	"errors"
	"fmt"
	"math"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

const (
	defaultDrawFeed = 600
	lookaheadWindow = 3
	maximumDeferral = 2

	// DefaultContiguousTolerance is the maximum endpoint distance in each axis
	// for consecutive open strokes to be planned as one continuous stroke. It
	// also breaks effectively equal stroke-direction travel distances.
	DefaultContiguousTolerance = 0.0005
)

// OperationKind identifies one planned plotting operation.
type OperationKind int

const (
	// OperationPenUp raises the pen to a Z height.
	OperationPenUp OperationKind = iota
	// OperationPenDown lowers the pen to a Z height.
	OperationPenDown
	// OperationRapidMove moves X/Y with the pen raised.
	OperationRapidMove
	// OperationDrawMove moves X/Y with the pen lowered.
	OperationDrawMove
)

// Operation is one planned machine-neutral plotting step.
type Operation struct {
	Kind  OperationKind
	Point drawing.Point
	Z     float64
	Feed  float64
}

// Options controls drawing-to-plot planning.
type Options struct {
	PenUpZ              float64
	PenDownZ            float64
	PenRaiseFeed        float64
	PenLowerFeed        float64
	DrawFeed            float64
	ReturnToOrigin      bool
	ContiguousTolerance float64
	ModulateDrawFeed    bool
}

// DefaultOptions returns safe defaults for generated drawing operations.
func DefaultOptions(penUpZ, penDownZ float64) Options {
	return Options{
		PenUpZ:              penUpZ,
		PenDownZ:            penDownZ,
		PenRaiseFeed:        300,
		PenLowerFeed:        200,
		DrawFeed:            defaultDrawFeed,
		ReturnToOrigin:      true,
		ContiguousTolerance: DefaultContiguousTolerance,
	}
}

// Plan converts a processed drawing into ordered pen and X/Y operations.
func Plan(d drawing.Drawing, opts Options) ([]Operation, error) {
	opts = opts.withDefaults()
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if _, err := drawing.New(d.Strokes); err != nil {
		return nil, err
	}

	ops := []Operation{{
		Kind: OperationPenUp,
		Z:    opts.PenUpZ,
		Feed: opts.PenRaiseFeed,
	}}
	remaining := remainingStrokes(d.Strokes, opts.ContiguousTolerance)
	current := drawing.Point{}
	for len(remaining) > 0 {
		selected := selectStrokeIndex(remaining, current, opts.ContiguousTolerance)
		var entry plannedStroke
		remaining, entry = removeSelectedStroke(remaining, selected)
		stroke := chooseStrokeEntry(entry.stroke, current, opts.ContiguousTolerance)
		if len(stroke.Points) < 2 {
			return nil, fmt.Errorf("stroke %d contains fewer than two points", entry.order+1)
		}
		start := stroke.Points[0]
		ops = append(ops,
			Operation{Kind: OperationRapidMove, Point: start},
			Operation{Kind: OperationPenDown, Z: opts.PenDownZ, Feed: opts.PenLowerFeed},
		)
		for _, point := range drawablePoints(stroke) {
			ops = append(ops, Operation{Kind: OperationDrawMove, Point: point})
		}
		ops = append(ops, Operation{Kind: OperationPenUp, Z: opts.PenUpZ, Feed: opts.PenRaiseFeed})
		current = strokeEnd(stroke)
	}
	if opts.ReturnToOrigin {
		ops = append(ops,
			Operation{Kind: OperationRapidMove, Point: drawing.Point{}},
			Operation{Kind: OperationPenUp, Z: opts.PenUpZ, Feed: opts.PenRaiseFeed},
		)
	}
	if opts.ModulateDrawFeed {
		annotateCurvatureFeeds(ops, opts.DrawFeed, opts.ContiguousTolerance)
	} else {
		annotateFixedDrawFeeds(ops, opts.DrawFeed)
	}
	return ops, nil
}

func drawablePoints(stroke drawing.Stroke) []drawing.Point {
	points := stroke.Points[1:]
	if stroke.Closed && !samePoint(stroke.Points[0], stroke.Points[len(stroke.Points)-1]) {
		points = append(append([]drawing.Point(nil), points...), stroke.Points[0])
	}
	return points
}

type plannedStroke struct {
	stroke    drawing.Stroke
	order     int
	deferrals int
}

func remainingStrokes(strokes []drawing.Stroke, tolerance float64) []plannedStroke {
	remaining := make([]plannedStroke, 0, len(strokes))
	for i, stroke := range strokes {
		if len(remaining) == 0 {
			remaining = append(remaining, plannedStroke{stroke: cloneStroke(stroke), order: i})
			continue
		}
		last := &remaining[len(remaining)-1]
		if canMerge(last.stroke, stroke, tolerance) {
			last.stroke.Points = append(last.stroke.Points, stroke.Points[1:]...)
			continue
		}
		remaining = append(remaining, plannedStroke{stroke: cloneStroke(stroke), order: i})
	}
	return remaining
}

func selectStrokeIndex(remaining []plannedStroke, current drawing.Point, tolerance float64) int {
	if remaining[0].deferrals >= maximumDeferral {
		return 0
	}
	bestIndex := 0
	bestDistance := strokeTravelDistance(remaining[0].stroke, current, tolerance)
	for i := 1; i < candidateCount(remaining); i++ {
		candidateDistance := strokeTravelDistance(remaining[i].stroke, current, tolerance)
		if betterStrokeCandidate(candidateDistance, bestDistance, remaining[i], remaining[bestIndex], tolerance) {
			bestIndex = i
			bestDistance = candidateDistance
		}
	}
	return bestIndex
}

func candidateCount(remaining []plannedStroke) int {
	if len(remaining) < lookaheadWindow {
		return len(remaining)
	}
	return lookaheadWindow
}

func betterStrokeCandidate(candidateDistance, bestDistance float64, candidate, best plannedStroke, tolerance float64) bool {
	if candidateDistance+tolerance < bestDistance {
		return true
	}
	return math.Abs(candidateDistance-bestDistance) <= tolerance && candidate.order < best.order
}

func removeSelectedStroke(remaining []plannedStroke, selected int) ([]plannedStroke, plannedStroke) {
	entry := remaining[selected]
	for i := 0; i < selected; i++ {
		remaining[i].deferrals++
	}
	remaining = append(remaining[:selected], remaining[selected+1:]...)
	return remaining, entry
}

func strokeTravelDistance(stroke drawing.Stroke, current drawing.Point, tolerance float64) float64 {
	if stroke.Closed {
		return distance(current, stroke.Points[nearestClosedVertexIndex(stroke, current, tolerance)])
	}
	return distance(current, orientStroke(stroke, current, tolerance).Points[0])
}

func chooseStrokeEntry(stroke drawing.Stroke, current drawing.Point, tolerance float64) drawing.Stroke {
	if stroke.Closed {
		return rotateClosedStroke(stroke, nearestClosedVertexIndex(stroke, current, tolerance))
	}
	return orientStroke(stroke, current, tolerance)
}

func orientStroke(stroke drawing.Stroke, current drawing.Point, tolerance float64) drawing.Stroke {
	if !shouldReverseStroke(stroke, current, tolerance) {
		return stroke
	}
	return reverseStroke(stroke)
}

func shouldReverseStroke(stroke drawing.Stroke, current drawing.Point, tolerance float64) bool {
	if stroke.Closed || len(stroke.Points) < 2 {
		return false
	}
	firstDistance := distance(current, stroke.Points[0])
	lastDistance := distance(current, stroke.Points[len(stroke.Points)-1])
	return lastDistance+tolerance < firstDistance
}

func reverseStroke(stroke drawing.Stroke) drawing.Stroke {
	points := make([]drawing.Point, len(stroke.Points))
	for i := range stroke.Points {
		points[i] = stroke.Points[len(stroke.Points)-1-i]
	}
	return drawing.Stroke{Points: points, Closed: stroke.Closed}
}

func strokeEnd(stroke drawing.Stroke) drawing.Point {
	if stroke.Closed {
		return stroke.Points[0]
	}
	return stroke.Points[len(stroke.Points)-1]
}

func nearestClosedVertexIndex(stroke drawing.Stroke, current drawing.Point, tolerance float64) int {
	vertexCount := closedVertexCount(stroke)
	bestIndex := 0
	bestDistance := distance(current, stroke.Points[0])
	for i := 1; i < vertexCount; i++ {
		candidateDistance := distance(current, stroke.Points[i])
		if candidateDistance+tolerance < bestDistance {
			bestIndex = i
			bestDistance = candidateDistance
		}
	}
	return bestIndex
}

func rotateClosedStroke(stroke drawing.Stroke, startIndex int) drawing.Stroke {
	vertexCount := closedVertexCount(stroke)
	if !stroke.Closed || startIndex <= 0 || vertexCount <= 1 {
		return stroke
	}
	points := make([]drawing.Point, 0, len(stroke.Points))
	points = append(points, stroke.Points[startIndex:vertexCount]...)
	points = append(points, stroke.Points[:startIndex]...)
	if hasExplicitClosure(stroke) {
		points = append(points, points[0])
	}
	return drawing.Stroke{Points: points, Closed: true}
}

func closedVertexCount(stroke drawing.Stroke) int {
	vertexCount := len(stroke.Points)
	if hasExplicitClosure(stroke) {
		vertexCount--
	}
	return vertexCount
}

func hasExplicitClosure(stroke drawing.Stroke) bool {
	return stroke.Closed &&
		len(stroke.Points) > 1 &&
		samePoint(stroke.Points[0], stroke.Points[len(stroke.Points)-1])
}

func mergeContiguousStrokes(strokes []drawing.Stroke, tolerance float64) []drawing.Stroke {
	remaining := remainingStrokes(strokes, tolerance)
	merged := make([]drawing.Stroke, len(remaining))
	for i, entry := range remaining {
		merged[i] = cloneStroke(entry.stroke)
	}
	return merged
}

func cloneStroke(stroke drawing.Stroke) drawing.Stroke {
	points := make([]drawing.Point, len(stroke.Points))
	copy(points, stroke.Points)
	return drawing.Stroke{Points: points, Closed: stroke.Closed}
}

func canMerge(a, b drawing.Stroke, tolerance float64) bool {
	if a.Closed || b.Closed || len(a.Points) == 0 || len(b.Points) == 0 {
		return false
	}
	return samePointWithin(a.Points[len(a.Points)-1], b.Points[0], tolerance)
}

func (opts Options) withDefaults() Options {
	if opts.PenRaiseFeed <= 0 {
		opts.PenRaiseFeed = DefaultOptions(opts.PenUpZ, opts.PenDownZ).PenRaiseFeed
	}
	if opts.PenLowerFeed <= 0 {
		opts.PenLowerFeed = DefaultOptions(opts.PenUpZ, opts.PenDownZ).PenLowerFeed
	}
	if opts.DrawFeed <= 0 {
		opts.DrawFeed = defaultDrawFeed
	}
	if opts.ContiguousTolerance == 0 {
		opts.ContiguousTolerance = DefaultContiguousTolerance
	}
	return opts
}

func validateOptions(opts Options) error {
	if !isFinite(opts.PenUpZ) || !isFinite(opts.PenDownZ) {
		return errors.New("pen Z positions must be finite")
	}
	if opts.PenRaiseFeed <= 0 || opts.PenLowerFeed <= 0 || opts.DrawFeed <= 0 {
		return errors.New("feed rates must be greater than zero")
	}
	if opts.ContiguousTolerance < 0 || !isFinite(opts.ContiguousTolerance) {
		return errors.New("contiguous stroke tolerance must be finite and non-negative")
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func samePoint(a, b drawing.Point) bool {
	return math.Abs(a.X-b.X) < DefaultContiguousTolerance && math.Abs(a.Y-b.Y) < DefaultContiguousTolerance
}

func samePointWithin(a, b drawing.Point, tolerance float64) bool {
	return math.Abs(a.X-b.X) <= tolerance && math.Abs(a.Y-b.Y) <= tolerance
}

func distance(a, b drawing.Point) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}
