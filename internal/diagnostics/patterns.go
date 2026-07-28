package diagnostics

import (
	"errors"
	"fmt"
	"math"

	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
)

const defaultDrawFeed = 600

// Options controls diagnostic pattern G-code generation.
type Options struct {
	PenUpZ       float64
	PenDownZ     float64
	PenRaiseFeed float64
	PenLowerFeed float64
	DrawFeed     float64
}

// DefaultOptions returns diagnostic generation options for the calibrated pen.
func DefaultOptions(penUpZ, penDownZ float64) Options {
	return Options{
		PenUpZ:       penUpZ,
		PenDownZ:     penDownZ,
		PenRaiseFeed: machine.DefaultPenRaiseFeed,
		PenLowerFeed: machine.DefaultPenLowerFeed,
		DrawFeed:     defaultDrawFeed,
	}
}

// Pattern generates a built-in machine diagnostic G-code program.
type Pattern interface {
	Name() string
	Generate(Options) ([]gcode.Line, error)
}

type point struct {
	x float64
	y float64
}

type stroke []point

// CirclePattern draws concentric circles near the calibrated program origin.
type CirclePattern struct{}

func (CirclePattern) Name() string { return "concentric circles" }

func (CirclePattern) Generate(opts Options) ([]gcode.Line, error) {
	center := point{35, -35}
	var strokes []stroke
	for _, radius := range []float64{5, 10, 15, 20} {
		strokes = append(strokes, circle(center, radius, 96))
	}
	return generateStrokes(strokes, opts)
}

// SquarePattern draws centered squares near the calibrated program origin.
type SquarePattern struct{}

func (SquarePattern) Name() string { return "concentric squares" }

func (SquarePattern) Generate(opts Options) ([]gcode.Line, error) {
	center := point{35, -35}
	var strokes []stroke
	for _, side := range []float64{10, 20, 30, 40} {
		strokes = append(strokes, square(center, side))
	}
	return generateStrokes(strokes, opts)
}

// TrianglePattern draws centered equilateral triangles near the origin.
type TrianglePattern struct{}

func (TrianglePattern) Name() string { return "concentric triangles" }

func (TrianglePattern) Generate(opts Options) ([]gcode.Line, error) {
	center := point{35, -35}
	var strokes []stroke
	for _, side := range []float64{12, 24, 36, 48} {
		strokes = append(strokes, triangle(center, side))
	}
	return generateStrokes(strokes, opts)
}

// SinePattern draws stacked horizontal sine-wave passes.
type SinePattern struct{}

func (SinePattern) Name() string { return "sine waves" }

func (SinePattern) Generate(opts Options) ([]gcode.Line, error) {
	var strokes []stroke
	for _, y := range []float64{-10, -20, -30, -40} {
		strokes = append(strokes, sineWave(5, 65, y, 3, 20, 96))
	}
	return generateStrokes(strokes, opts)
}

// GridPattern draws a regular 5-by-5 calibration grid.
type GridPattern struct{}

func (GridPattern) Name() string { return "calibration grid" }

func (GridPattern) Generate(opts Options) ([]gcode.Line, error) {
	const (
		size    = 50.0
		spacing = 10.0
	)
	var strokes []stroke
	for x := 0.0; x <= size; x += spacing {
		strokes = append(strokes, stroke{{x, 0}, {x, -size}})
	}
	for y := 0.0; y >= -size; y -= spacing {
		strokes = append(strokes, stroke{{0, y}, {size, y}})
	}
	return generateStrokes(strokes, opts)
}

// CrosshairPattern draws X/Y axes with tick marks from the calibrated origin.
type CrosshairPattern struct{}

func (CrosshairPattern) Name() string { return "X/Y crosshair" }

func (CrosshairPattern) Generate(opts Options) ([]gcode.Line, error) {
	const (
		length  = 60.0
		spacing = 10.0
		tick    = 3.0
	)
	strokes := []stroke{
		{{0, 0}, {length, 0}},
		{{0, 0}, {0, -length}},
	}
	for x := spacing; x <= length; x += spacing {
		strokes = append(strokes, stroke{{x, 0}, {x, -tick}})
	}
	for y := -spacing; y >= -length; y -= spacing {
		strokes = append(strokes, stroke{{0, y}, {tick, y}})
	}
	return generateStrokes(strokes, opts)
}

func generateStrokes(strokes []stroke, opts Options) ([]gcode.Line, error) {
	opts = opts.withDefaults()
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if len(strokes) == 0 {
		return nil, errors.New("pattern contains no strokes")
	}

	var commands []string
	appendCommand := func(format string, args ...any) {
		commands = append(commands, fmt.Sprintf(format, args...))
	}

	appendCommand("G1 Z%.3f F%s", opts.PenUpZ, formatFeed(opts.PenRaiseFeed))
	for i, s := range strokes {
		if len(s) < 2 {
			return nil, fmt.Errorf("stroke %d contains fewer than two points", i+1)
		}
		for _, p := range s {
			if err := validatePoint(p); err != nil {
				return nil, fmt.Errorf("stroke %d: %w", i+1, err)
			}
		}

		start := s[0]
		appendCommand("G0 X%.3f Y%.3f", start.x, start.y)
		appendCommand("G1 Z%.3f F%s", opts.PenDownZ, formatFeed(opts.PenLowerFeed))
		for j, p := range s[1:] {
			if j == 0 {
				appendCommand("G1 X%.3f Y%.3f F%s", p.x, p.y, formatFeed(opts.DrawFeed))
			} else {
				appendCommand("G1 X%.3f Y%.3f", p.x, p.y)
			}
		}
		appendCommand("G1 Z%.3f F%s", opts.PenUpZ, formatFeed(opts.PenRaiseFeed))
	}
	appendCommand("G0 X0.000 Y0.000")
	appendCommand("G1 Z%.3f F%s", opts.PenUpZ, formatFeed(opts.PenRaiseFeed))

	lines := make([]gcode.Line, 0, len(commands))
	for i, command := range commands {
		lines = append(lines, gcode.Line{Number: i + 1, Command: command})
	}
	return lines, nil
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

func validatePoint(p point) error {
	if !isFinite(p.x) || !isFinite(p.y) {
		return fmt.Errorf("non-finite point X%.3f Y%.3f", p.x, p.y)
	}
	if p.x < -0.001 || p.x > 100 || p.y > 0.001 || p.y < -100 {
		return fmt.Errorf("point X%.3f Y%.3f outside diagnostic work area", p.x, p.y)
	}
	return nil
}

func circle(center point, radius float64, segments int) stroke {
	result := make(stroke, 0, segments+1)
	for i := 0; i <= segments; i++ {
		angle := 2 * math.Pi * float64(i) / float64(segments)
		result = append(result, point{
			x: center.x + radius*math.Cos(angle),
			y: center.y + radius*math.Sin(angle),
		})
	}
	return result
}

func square(center point, side float64) stroke {
	half := side / 2
	return stroke{
		{center.x - half, center.y + half},
		{center.x + half, center.y + half},
		{center.x + half, center.y - half},
		{center.x - half, center.y - half},
		{center.x - half, center.y + half},
	}
}

func triangle(center point, side float64) stroke {
	height := side * math.Sqrt(3) / 2
	return stroke{
		{center.x, center.y + (2 * height / 3)},
		{center.x + side/2, center.y - (height / 3)},
		{center.x - side/2, center.y - (height / 3)},
		{center.x, center.y + (2 * height / 3)},
	}
}

func sineWave(startX, endX, centerY, amplitude, wavelength float64, segments int) stroke {
	result := make(stroke, 0, segments+1)
	width := endX - startX
	for i := 0; i <= segments; i++ {
		t := float64(i) / float64(segments)
		x := startX + width*t
		y := centerY + amplitude*math.Sin(2*math.Pi*(x-startX)/wavelength)
		result = append(result, point{x: x, y: y})
	}
	return result
}

func formatFeed(feed float64) string {
	if math.Abs(feed-math.Round(feed)) < 0.0005 {
		return fmt.Sprintf("%.0f", feed)
	}
	return fmt.Sprintf("%.3f", feed)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
