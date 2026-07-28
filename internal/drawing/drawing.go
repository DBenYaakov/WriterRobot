// Package drawing defines neutral 2D drawing geometry.
//
// It contains no SVG, G-code, GRBL, machine, session, or CLI knowledge. SVG
// parsing, geometric processing, plot planning, and machine-command generation
// are separate layers that communicate through these geometry types.
package drawing

import (
	"errors"
	"fmt"
	"math"
)

// Point is a 2D point in WriterRobot program coordinates.
type Point struct {
	X float64
	Y float64
}

// Transform is a 2D affine transform matrix.
type Transform struct {
	A float64
	B float64
	C float64
	D float64
	E float64
	F float64
}

// IdentityTransform returns the identity affine transform.
func IdentityTransform() Transform {
	return Transform{A: 1, D: 1}
}

// Apply returns point transformed by t.
func (t Transform) Apply(point Point) Point {
	return Point{
		X: t.A*point.X + t.C*point.Y + t.E,
		Y: t.B*point.X + t.D*point.Y + t.F,
	}
}

// Then composes t with next using SVG-style inherited transform order.
func (t Transform) Then(next Transform) Transform {
	return Transform{
		A: t.A*next.A + t.C*next.B,
		B: t.B*next.A + t.D*next.B,
		C: t.A*next.C + t.C*next.D,
		D: t.B*next.C + t.D*next.D,
		E: t.A*next.E + t.C*next.F + t.E,
		F: t.B*next.E + t.D*next.F + t.F,
	}
}

// SegmentKind identifies a vector path segment.
type SegmentKind int

const (
	// SegmentLine is a straight line segment.
	SegmentLine SegmentKind = iota
	// SegmentCubic is a cubic Bezier segment.
	SegmentCubic
	// SegmentQuadratic is a quadratic Bezier segment.
	SegmentQuadratic
	// SegmentEllipse is a full ellipse segment.
	SegmentEllipse
)

// Segment is one vector segment in a source-coordinate stroke.
type Segment struct {
	Kind SegmentKind

	Start    Point
	Control1 Point
	Control2 Point
	End      Point

	Center  Point
	RadiusX float64
	RadiusY float64
}

// VectorStroke is one continuous source-coordinate path before geometric
// processing such as transforms, curve flattening, scaling, or Y inversion.
type VectorStroke struct {
	Start     Point
	Segments  []Segment
	Closed    bool
	Transform Transform
}

// VectorDrawing is an ordered set of source-coordinate vector strokes.
type VectorDrawing struct {
	Strokes []VectorStroke
}

// Stroke is one continuous pen-down polyline after geometric processing.
type Stroke struct {
	Points []Point
	Closed bool
}

// Drawing is an ordered set of strokes and its computed bounds.
type Drawing struct {
	Strokes []Stroke
	Bounds  Bounds
}

// Bounds is an axis-aligned rectangle in program coordinates.
type Bounds struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

// New validates strokes and computes their bounds.
func New(strokes []Stroke) (Drawing, error) {
	if len(strokes) == 0 {
		return Drawing{}, errors.New("drawing contains no strokes")
	}
	copied := make([]Stroke, 0, len(strokes))
	for i, stroke := range strokes {
		if len(stroke.Points) < 2 {
			return Drawing{}, fmt.Errorf("stroke %d contains fewer than two points", i+1)
		}
		points := make([]Point, len(stroke.Points))
		copy(points, stroke.Points)
		for j, point := range points {
			if !finitePoint(point) {
				return Drawing{}, fmt.Errorf("stroke %d point %d is not finite", i+1, j+1)
			}
		}
		if stroke.Closed && !samePoint(points[0], points[len(points)-1]) {
			points = append(points, points[0])
		}
		copied = append(copied, Stroke{Points: points, Closed: stroke.Closed})
	}
	bounds, err := ComputeBounds(copied)
	if err != nil {
		return Drawing{}, err
	}
	return Drawing{Strokes: copied, Bounds: bounds}, nil
}

// ComputeBounds returns the bounds for strokes.
func ComputeBounds(strokes []Stroke) (Bounds, error) {
	if len(strokes) == 0 {
		return Bounds{}, errors.New("drawing contains no strokes")
	}
	var bounds Bounds
	initialized := false
	for i, stroke := range strokes {
		if len(stroke.Points) < 2 {
			return Bounds{}, fmt.Errorf("stroke %d contains fewer than two points", i+1)
		}
		for j, point := range stroke.Points {
			if !finitePoint(point) {
				return Bounds{}, fmt.Errorf("stroke %d point %d is not finite", i+1, j+1)
			}
			if !initialized {
				bounds = Bounds{MinX: point.X, MinY: point.Y, MaxX: point.X, MaxY: point.Y}
				initialized = true
				continue
			}
			bounds.Include(point)
		}
	}
	return bounds, nil
}

// Include expands b to contain point.
func (b *Bounds) Include(point Point) {
	if point.X < b.MinX {
		b.MinX = point.X
	}
	if point.X > b.MaxX {
		b.MaxX = point.X
	}
	if point.Y < b.MinY {
		b.MinY = point.Y
	}
	if point.Y > b.MaxY {
		b.MaxY = point.Y
	}
}

// Width returns the bounds width.
func (b Bounds) Width() float64 {
	return b.MaxX - b.MinX
}

// Height returns the bounds height.
func (b Bounds) Height() float64 {
	return b.MaxY - b.MinY
}

// Valid reports whether b is finite and non-empty.
func (b Bounds) Valid() bool {
	return isFinite(b.MinX) && isFinite(b.MinY) && isFinite(b.MaxX) && isFinite(b.MaxY) && b.MaxX >= b.MinX && b.MaxY >= b.MinY
}

// Contains reports whether b fully contains other, allowing a small tolerance.
func (b Bounds) Contains(other Bounds) bool {
	const epsilon = 0.001
	return b.Valid() &&
		other.Valid() &&
		other.MinX >= b.MinX-epsilon &&
		other.MaxX <= b.MaxX+epsilon &&
		other.MinY >= b.MinY-epsilon &&
		other.MaxY <= b.MaxY+epsilon
}

func finitePoint(point Point) bool {
	return isFinite(point.X) && isFinite(point.Y)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func samePoint(a, b Point) bool {
	return math.Abs(a.X-b.X) < 0.0005 && math.Abs(a.Y-b.Y) < 0.0005
}
