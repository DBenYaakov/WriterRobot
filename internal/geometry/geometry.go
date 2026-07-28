// Package geometry processes neutral drawing geometry.
//
// It sits between parsers and plot planning. Responsibilities here include
// applying affine transforms, flattening curves, handling source rectangles such
// as SVG viewBox data, scaling, Y-axis inversion into WriterRobot program
// coordinates, bounds computation, and work-area validation. It does not know
// about SVG XML, G-code, GRBL, machine sessions, or CLI flags.
package geometry

import (
	"errors"
	"fmt"
	"math"

	"github.com/DBenYaakov/WriterRobot/internal/bezier"
	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

// Anchor controls where processed geometry lands relative to the program origin.
type Anchor string

const (
	// AnchorTopLeft places the source top-left corner at X0 Y0.
	AnchorTopLeft Anchor = "top-left"
	// AnchorCenter places the source center at X0 Y0.
	AnchorCenter Anchor = "center"
)

// Source describes parsed vector geometry and optional source-viewport metadata.
type Source struct {
	Drawing drawing.VectorDrawing
	ViewBox *Rect
	Width   *float64
	Height  *float64
}

// Rect is a source-coordinate rectangle.
type Rect struct {
	MinX   float64
	MinY   float64
	Width  float64
	Height float64
}

// Options controls geometry processing.
type Options struct {
	FlattenTolerance float64
	FitWidth         float64
	FitHeight        float64
	Anchor           Anchor
	FitToWorkArea    bool
	WorkWidth        float64
	WorkHeight       float64
}

// Result describes processed geometry and the key fitting values used to
// produce it.
type Result struct {
	Drawing      drawing.Drawing
	SourceBounds drawing.Bounds
	Scale        float64
	FinalBounds  drawing.Bounds
}

// DefaultOptions returns the default SVG plotting geometry behavior.
func DefaultOptions() Options {
	return Options{FlattenTolerance: 0.10, Anchor: AnchorTopLeft}
}

// Process converts parsed vector geometry into flattened WriterRobot program
// coordinates.
func Process(source Source, opts Options) (drawing.Drawing, error) {
	result, err := ProcessWithReport(source, opts)
	if err != nil {
		return drawing.Drawing{}, err
	}
	return result.Drawing, nil
}

// ProcessWithReport converts parsed vector geometry into flattened
// WriterRobot program coordinates and reports the fitting values used.
func ProcessWithReport(source Source, opts Options) (Result, error) {
	opts = opts.withDefaults()
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}
	strokes, err := Flatten(source.Drawing, opts.FlattenTolerance)
	if err != nil {
		return Result{}, err
	}
	if len(strokes) == 0 {
		return Result{}, errors.New("drawing contains no strokes")
	}

	actualBounds, err := drawing.ComputeBounds(strokes)
	if err != nil {
		return Result{}, err
	}
	if opts.FitToWorkArea {
		d, scale, err := fitToWorkArea(strokes, actualBounds, opts)
		if err != nil {
			return Result{}, err
		}
		return Result{Drawing: d, SourceBounds: actualBounds, Scale: scale, FinalBounds: d.Bounds}, nil
	}

	sourceBounds, err := source.bounds(strokes)
	if err != nil {
		return Result{}, err
	}
	scale, err := source.scaleFor(sourceBounds, opts)
	if err != nil {
		return Result{}, err
	}
	d, err := normalizeToProgram(strokes, sourceBounds, scale, opts.Anchor)
	if err != nil {
		return Result{}, err
	}
	return Result{Drawing: d, SourceBounds: actualBounds, Scale: scale, FinalBounds: d.Bounds}, nil
}

// Flatten converts vector strokes into polyline strokes while applying each
// stroke's source transform.
func Flatten(d drawing.VectorDrawing, tolerance float64) ([]drawing.Stroke, error) {
	if tolerance <= 0 || math.IsNaN(tolerance) || math.IsInf(tolerance, 0) {
		return nil, errors.New("flattening tolerance must be finite and greater than zero")
	}
	if len(d.Strokes) == 0 {
		return nil, errors.New("drawing contains no strokes")
	}
	strokes := make([]drawing.Stroke, 0, len(d.Strokes))
	for i, stroke := range d.Strokes {
		points, err := flattenStroke(stroke, tolerance)
		if err != nil {
			return nil, fmt.Errorf("stroke %d: %w", i+1, err)
		}
		strokes = append(strokes, drawing.Stroke{Points: points, Closed: stroke.Closed})
	}
	return strokes, nil
}

// WorkBounds returns the normal upper-left-origin work area used by ta4-send.
func WorkBounds(width, height float64) drawing.Bounds {
	return drawing.Bounds{MinX: 0, MinY: -height, MaxX: width, MaxY: 0}
}

// Preflight verifies that a drawing is non-empty, finite, and inside limits.
func Preflight(d drawing.Drawing, limits drawing.Bounds) error {
	if len(d.Strokes) == 0 {
		return errors.New("drawing contains no strokes")
	}
	bounds, err := drawing.ComputeBounds(d.Strokes)
	if err != nil {
		return err
	}
	if !limits.Contains(bounds) {
		return fmt.Errorf("drawing bounds X%.3f..%.3f Y%.3f..%.3f exceed work bounds X%.3f..%.3f Y%.3f..%.3f",
			bounds.MinX, bounds.MaxX, bounds.MinY, bounds.MaxY,
			limits.MinX, limits.MaxX, limits.MinY, limits.MaxY)
	}
	return nil
}

func flattenStroke(stroke drawing.VectorStroke, tolerance float64) ([]drawing.Point, error) {
	if len(stroke.Segments) == 0 {
		return nil, errors.New("contains no segments")
	}
	transform := stroke.Transform
	points := []drawing.Point{transform.Apply(stroke.Start)}
	for _, segment := range stroke.Segments {
		segmentPoints, err := flattenSegment(segment, tolerance)
		if err != nil {
			return nil, err
		}
		for _, point := range segmentPoints {
			points = append(points, transform.Apply(point))
		}
	}
	if stroke.Closed && !samePoint(points[0], points[len(points)-1]) {
		points = append(points, points[0])
	}
	if len(points) < 2 {
		return nil, errors.New("contains fewer than two points")
	}
	return points, nil
}

func flattenSegment(segment drawing.Segment, tolerance float64) ([]drawing.Point, error) {
	switch segment.Kind {
	case drawing.SegmentLine:
		return []drawing.Point{segment.End}, nil
	case drawing.SegmentCubic:
		points, err := bezier.Flatten(bezier.Cubic{
			P0: bezier.Point{X: segment.Start.X, Y: segment.Start.Y},
			P1: bezier.Point{X: segment.Control1.X, Y: segment.Control1.Y},
			P2: bezier.Point{X: segment.Control2.X, Y: segment.Control2.Y},
			P3: bezier.Point{X: segment.End.X, Y: segment.End.Y},
		}, tolerance)
		if err != nil {
			return nil, fmt.Errorf("flatten cubic segment: %w", err)
		}
		return bezierPoints(points[1:]), nil
	case drawing.SegmentQuadratic:
		c1 := drawing.Point{
			X: segment.Start.X + (2.0/3.0)*(segment.Control1.X-segment.Start.X),
			Y: segment.Start.Y + (2.0/3.0)*(segment.Control1.Y-segment.Start.Y),
		}
		c2 := drawing.Point{
			X: segment.End.X + (2.0/3.0)*(segment.Control1.X-segment.End.X),
			Y: segment.End.Y + (2.0/3.0)*(segment.Control1.Y-segment.End.Y),
		}
		return flattenSegment(drawing.Segment{
			Kind:     drawing.SegmentCubic,
			Start:    segment.Start,
			Control1: c1,
			Control2: c2,
			End:      segment.End,
		}, tolerance)
	case drawing.SegmentEllipse:
		return ellipsePoints(segment.Center, segment.RadiusX, segment.RadiusY, tolerance), nil
	default:
		return nil, fmt.Errorf("unsupported segment kind %d", segment.Kind)
	}
}

func bezierPoints(points []bezier.Point) []drawing.Point {
	result := make([]drawing.Point, 0, len(points))
	for _, point := range points {
		result = append(result, drawing.Point{X: point.X, Y: point.Y})
	}
	return result
}

func ellipsePoints(center drawing.Point, rx, ry, tolerance float64) []drawing.Point {
	segments := segmentsForRadius(math.Max(rx, ry), tolerance)
	points := make([]drawing.Point, 0, segments)
	for i := 1; i <= segments; i++ {
		angle := 2 * math.Pi * float64(i) / float64(segments)
		points = append(points, drawing.Point{
			X: center.X + rx*math.Cos(angle),
			Y: center.Y + ry*math.Sin(angle),
		})
	}
	return points
}

func segmentsForRadius(radius, tolerance float64) int {
	if tolerance <= 0 || tolerance >= radius {
		return 24
	}
	angle := 2 * math.Acos(1-tolerance/radius)
	if angle <= 0 || math.IsNaN(angle) || math.IsInf(angle, 0) {
		return 24
	}
	segments := int(math.Ceil(2 * math.Pi / angle))
	if segments < 24 {
		return 24
	}
	if segments > 720 {
		return 720
	}
	return segments
}

func (source Source) bounds(strokes []drawing.Stroke) (Rect, error) {
	if source.ViewBox != nil {
		if source.ViewBox.Width <= 0 || source.ViewBox.Height <= 0 {
			return Rect{}, errors.New("source viewBox width and height must be greater than zero")
		}
		return *source.ViewBox, nil
	}
	d, err := drawing.New(strokes)
	if err != nil {
		return Rect{}, err
	}
	return Rect{MinX: d.Bounds.MinX, MinY: d.Bounds.MinY, Width: d.Bounds.Width(), Height: d.Bounds.Height()}, nil
}

func (source Source) scaleFor(bounds Rect, opts Options) (float64, error) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return 0, errors.New("SVG source bounds must have non-zero width and height")
	}
	scale := 1.0
	if source.Width != nil && source.Height != nil {
		scale = math.Min(*source.Width/bounds.Width, *source.Height/bounds.Height)
	}
	if opts.FitWidth > 0 && opts.FitHeight > 0 {
		scale = math.Min(opts.FitWidth/bounds.Width, opts.FitHeight/bounds.Height)
	} else if opts.FitWidth > 0 {
		scale = opts.FitWidth / bounds.Width
	} else if opts.FitHeight > 0 {
		scale = opts.FitHeight / bounds.Height
	}
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 0, errors.New("SVG scale must be finite and greater than zero")
	}
	return scale, nil
}

func normalizeToProgram(strokes []drawing.Stroke, source Rect, scale float64, anchor Anchor) (drawing.Drawing, error) {
	offsetX := 0.0
	offsetY := 0.0
	if anchor == AnchorCenter {
		offsetX = -source.Width * scale / 2
		offsetY = source.Height * scale / 2
	}

	transformed := make([]drawing.Stroke, 0, len(strokes))
	for _, stroke := range strokes {
		points := make([]drawing.Point, 0, len(stroke.Points))
		for _, point := range stroke.Points {
			points = append(points, drawing.Point{
				X: offsetX + (point.X-source.MinX)*scale,
				Y: offsetY - (point.Y-source.MinY)*scale,
			})
		}
		transformed = append(transformed, drawing.Stroke{Points: points, Closed: stroke.Closed})
	}
	return drawing.New(transformed)
}

func fitToWorkArea(strokes []drawing.Stroke, source drawing.Bounds, opts Options) (drawing.Drawing, float64, error) {
	sourceWidth := source.Width()
	sourceHeight := source.Height()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return drawing.Drawing{}, 0, errors.New("SVG source bounds must have non-zero width and height")
	}
	if !isFinite(sourceWidth) || !isFinite(sourceHeight) {
		return drawing.Drawing{}, 0, errors.New("SVG source bounds must be finite")
	}

	targetWidth := opts.WorkWidth
	targetHeight := opts.WorkHeight
	if opts.FitWidth > 0 {
		targetWidth = opts.FitWidth
	}
	if opts.FitHeight > 0 {
		targetHeight = opts.FitHeight
	}
	if targetWidth <= 0 || targetHeight <= 0 || !isFinite(targetWidth) || !isFinite(targetHeight) {
		return drawing.Drawing{}, 0, errors.New("SVG fit target must be finite and greater than zero")
	}

	scale := math.Min(targetWidth/sourceWidth, targetHeight/sourceHeight)
	if scale <= 0 || !isFinite(scale) {
		return drawing.Drawing{}, 0, errors.New("SVG scale must be finite and greater than zero")
	}

	scaledWidth := sourceWidth * scale
	scaledHeight := sourceHeight * scale
	offsetX := 0.0
	offsetY := 0.0
	if opts.Anchor == AnchorCenter {
		offsetX = (opts.WorkWidth - scaledWidth) / 2
		offsetY = -(opts.WorkHeight - scaledHeight) / 2
	}

	transformed := make([]drawing.Stroke, 0, len(strokes))
	for _, stroke := range strokes {
		points := make([]drawing.Point, 0, len(stroke.Points))
		for _, point := range stroke.Points {
			points = append(points, drawing.Point{
				X: offsetX + (point.X-source.MinX)*scale,
				Y: offsetY - (point.Y-source.MinY)*scale,
			})
		}
		transformed = append(transformed, drawing.Stroke{Points: points, Closed: stroke.Closed})
	}
	d, err := drawing.New(transformed)
	if err != nil {
		return drawing.Drawing{}, 0, err
	}
	return d, scale, nil
}

func (opts Options) withDefaults() Options {
	if opts.FlattenTolerance == 0 {
		opts.FlattenTolerance = DefaultOptions().FlattenTolerance
	}
	if opts.Anchor == "" {
		opts.Anchor = AnchorTopLeft
	}
	return opts
}

func validateOptions(opts Options) error {
	if opts.FlattenTolerance <= 0 || math.IsNaN(opts.FlattenTolerance) || math.IsInf(opts.FlattenTolerance, 0) {
		return errors.New("SVG flattening tolerance must be finite and greater than zero")
	}
	if opts.FitWidth < 0 || opts.FitHeight < 0 {
		return errors.New("SVG fit dimensions must not be negative")
	}
	if opts.FitToWorkArea && (opts.WorkWidth <= 0 || opts.WorkHeight <= 0 || !isFinite(opts.WorkWidth) || !isFinite(opts.WorkHeight)) {
		return errors.New("work width and height must be finite and greater than zero")
	}
	switch opts.Anchor {
	case AnchorTopLeft, AnchorCenter:
		return nil
	default:
		return fmt.Errorf("unsupported SVG anchor %q", opts.Anchor)
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func samePoint(a, b drawing.Point) bool {
	return math.Abs(a.X-b.X) < 0.0005 && math.Abs(a.Y-b.Y) < 0.0005
}
