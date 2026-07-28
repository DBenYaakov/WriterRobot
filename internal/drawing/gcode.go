package drawing

import (
	"errors"
	"fmt"
	"math"

	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
)

const defaultDrawFeed = 600

// Options controls drawing-to-G-code conversion.
type Options struct {
	PenUpZ         float64
	PenDownZ       float64
	PenRaiseFeed   float64
	PenLowerFeed   float64
	DrawFeed       float64
	WorkBounds     Bounds
	EnforceBounds  bool
	ReturnToOrigin bool
}

// DefaultOptions returns safe defaults for generated drawing programs.
func DefaultOptions(penUpZ, penDownZ float64) Options {
	return Options{
		PenUpZ:         penUpZ,
		PenDownZ:       penDownZ,
		PenRaiseFeed:   machine.DefaultPenRaiseFeed,
		PenLowerFeed:   machine.DefaultPenLowerFeed,
		DrawFeed:       defaultDrawFeed,
		WorkBounds:     WorkBounds(100, 100),
		EnforceBounds:  true,
		ReturnToOrigin: true,
	}
}

// GenerateGCode converts a preflighted drawing to safe absolute program-coordinate
// motion.
func GenerateGCode(d Drawing, opts Options) ([]gcode.Line, error) {
	opts = opts.withDefaults()
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if opts.EnforceBounds {
		if err := Preflight(d, opts.WorkBounds); err != nil {
			return nil, err
		}
	} else if _, err := New(d.Strokes); err != nil {
		return nil, err
	}

	var commands []string
	appendCommand := func(format string, args ...any) {
		commands = append(commands, fmt.Sprintf(format, args...))
	}

	appendCommand("G1 Z%.3f F%s", opts.PenUpZ, formatFeed(opts.PenRaiseFeed))
	for i, stroke := range d.Strokes {
		if len(stroke.Points) < 2 {
			return nil, fmt.Errorf("stroke %d contains fewer than two points", i+1)
		}
		start := stroke.Points[0]
		appendCommand("G0 X%.3f Y%.3f", start.X, start.Y)
		appendCommand("G1 Z%.3f F%s", opts.PenDownZ, formatFeed(opts.PenLowerFeed))
		for j, point := range drawablePoints(stroke) {
			if j == 0 {
				appendCommand("G1 X%.3f Y%.3f F%s", point.X, point.Y, formatFeed(opts.DrawFeed))
			} else {
				appendCommand("G1 X%.3f Y%.3f", point.X, point.Y)
			}
		}
		appendCommand("G1 Z%.3f F%s", opts.PenUpZ, formatFeed(opts.PenRaiseFeed))
	}
	if opts.ReturnToOrigin {
		appendCommand("G0 X0.000 Y0.000")
		appendCommand("G1 Z%.3f F%s", opts.PenUpZ, formatFeed(opts.PenRaiseFeed))
	}

	lines := make([]gcode.Line, 0, len(commands))
	for i, command := range commands {
		lines = append(lines, gcode.Line{Number: i + 1, Command: command})
	}
	return lines, nil
}

func drawablePoints(stroke Stroke) []Point {
	points := stroke.Points[1:]
	if stroke.Closed && !samePoint(stroke.Points[0], stroke.Points[len(stroke.Points)-1]) {
		points = append(append([]Point(nil), points...), stroke.Points[0])
	}
	return points
}

func (opts Options) withDefaults() Options {
	if opts.PenRaiseFeed <= 0 {
		opts.PenRaiseFeed = machine.DefaultPenRaiseFeed
	}
	if opts.PenLowerFeed <= 0 {
		opts.PenLowerFeed = machine.DefaultPenLowerFeed
	}
	if opts.DrawFeed <= 0 {
		opts.DrawFeed = defaultDrawFeed
	}
	if opts.WorkBounds == (Bounds{}) {
		opts.WorkBounds = WorkBounds(100, 100)
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
	if opts.EnforceBounds && !opts.WorkBounds.Valid() {
		return errors.New("work bounds must be finite and non-empty")
	}
	return nil
}

func formatFeed(feed float64) string {
	if math.Abs(feed-math.Round(feed)) < 0.0005 {
		return fmt.Sprintf("%.0f", feed)
	}
	return fmt.Sprintf("%.3f", feed)
}
