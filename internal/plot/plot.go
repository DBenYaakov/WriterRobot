// Package plot plans pen and XY motion for processed drawings.
//
// It first merges consecutive open strokes whose endpoints are already
// contiguous, then uses a deterministic nearest-neighbor pass to choose the next
// stroke. Each candidate open stroke may be drawn in reverse when its endpoint
// is closer to the current pen position; closed strokes keep their original
// orientation. Equal-distance ties preserve original document order and original
// stroke direction. The planner inserts pen-up travel between disconnected
// strokes, lowers the pen only for drawing moves, and returns an ordered list of
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
	remaining := remainingStrokes(mergeContiguousStrokes(d.Strokes, opts.ContiguousTolerance))
	current := drawing.Point{}
	for len(remaining) > 0 {
		selected := nearestStrokeIndex(remaining, current, opts.ContiguousTolerance)
		entry := remaining[selected]
		remaining = append(remaining[:selected], remaining[selected+1:]...)
		stroke := orientStroke(entry.stroke, current, opts.ContiguousTolerance)
		if len(stroke.Points) < 2 {
			return nil, fmt.Errorf("stroke %d contains fewer than two points", entry.order+1)
		}
		start := stroke.Points[0]
		ops = append(ops,
			Operation{Kind: OperationRapidMove, Point: start},
			Operation{Kind: OperationPenDown, Z: opts.PenDownZ, Feed: opts.PenLowerFeed},
		)
		for j, point := range drawablePoints(stroke) {
			feed := 0.0
			if j == 0 {
				feed = opts.DrawFeed
			}
			ops = append(ops, Operation{Kind: OperationDrawMove, Point: point, Feed: feed})
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
	stroke drawing.Stroke
	order  int
}

func remainingStrokes(strokes []drawing.Stroke) []plannedStroke {
	remaining := make([]plannedStroke, len(strokes))
	for i, stroke := range strokes {
		remaining[i] = plannedStroke{stroke: stroke, order: i}
	}
	return remaining
}

func nearestStrokeIndex(remaining []plannedStroke, current drawing.Point, tolerance float64) int {
	bestIndex := 0
	bestDistance := strokeTravelDistance(remaining[0].stroke, current, tolerance)
	for i := 1; i < len(remaining); i++ {
		candidateDistance := strokeTravelDistance(remaining[i].stroke, current, tolerance)
		if candidateDistance+tolerance < bestDistance {
			bestIndex = i
			bestDistance = candidateDistance
		}
	}
	return bestIndex
}

func strokeTravelDistance(stroke drawing.Stroke, current drawing.Point, tolerance float64) float64 {
	stroke = orientStroke(stroke, current, tolerance)
	return distance(current, stroke.Points[0])
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

func mergeContiguousStrokes(strokes []drawing.Stroke, tolerance float64) []drawing.Stroke {
	if len(strokes) < 2 {
		return strokes
	}
	merged := make([]drawing.Stroke, 0, len(strokes))
	for _, stroke := range strokes {
		if len(merged) == 0 {
			merged = append(merged, cloneStroke(stroke))
			continue
		}
		last := &merged[len(merged)-1]
		if canMerge(*last, stroke, tolerance) {
			last.Points = append(last.Points, stroke.Points[1:]...)
			continue
		}
		merged = append(merged, cloneStroke(stroke))
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
