// Package plot plans pen and XY motion for processed drawings.
//
// It preserves drawing order, inserts pen-up travel between disconnected
// strokes, lowers the pen only for drawing moves, and returns an ordered list
// of operations. It does not format G-code or know about GRBL, sessions, serial
// transport, SVG, or CLI flags.
package plot

import (
	"errors"
	"fmt"
	"math"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

const defaultDrawFeed = 600

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
	PenUpZ         float64
	PenDownZ       float64
	PenRaiseFeed   float64
	PenLowerFeed   float64
	DrawFeed       float64
	ReturnToOrigin bool
}

// DefaultOptions returns safe defaults for generated drawing operations.
func DefaultOptions(penUpZ, penDownZ float64) Options {
	return Options{
		PenUpZ:         penUpZ,
		PenDownZ:       penDownZ,
		PenRaiseFeed:   300,
		PenLowerFeed:   200,
		DrawFeed:       defaultDrawFeed,
		ReturnToOrigin: true,
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
	for i, stroke := range d.Strokes {
		if len(stroke.Points) < 2 {
			return nil, fmt.Errorf("stroke %d contains fewer than two points", i+1)
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
	return opts
}

func validateOptions(opts Options) error {
	if !isFinite(opts.PenUpZ) || !isFinite(opts.PenDownZ) {
		return errors.New("pen Z positions must be finite")
	}
	if opts.PenRaiseFeed <= 0 || opts.PenLowerFeed <= 0 || opts.DrawFeed <= 0 {
		return errors.New("feed rates must be greater than zero")
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func samePoint(a, b drawing.Point) bool {
	return math.Abs(a.X-b.X) < 0.0005 && math.Abs(a.Y-b.Y) < 0.0005
}
