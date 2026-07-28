// Package drawing defines neutral 2D drawing geometry.
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

// Stroke is one continuous pen-down drawing path.
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

// WorkBounds returns the normal upper-left-origin work area used by ta4-send.
func WorkBounds(width, height float64) Bounds {
	return Bounds{MinX: 0, MinY: -height, MaxX: width, MaxY: 0}
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

// Preflight verifies that a drawing is non-empty, finite, and inside limits.
func Preflight(d Drawing, limits Bounds) error {
	if len(d.Strokes) == 0 {
		return errors.New("drawing contains no strokes")
	}
	bounds, err := ComputeBounds(d.Strokes)
	if err != nil {
		return err
	}
	if !sameBounds(bounds, d.Bounds) {
		d.Bounds = bounds
	}
	if !limits.Contains(d.Bounds) {
		return fmt.Errorf("drawing bounds X%.3f..%.3f Y%.3f..%.3f exceed work bounds X%.3f..%.3f Y%.3f..%.3f",
			d.Bounds.MinX, d.Bounds.MaxX, d.Bounds.MinY, d.Bounds.MaxY,
			limits.MinX, limits.MaxX, limits.MinY, limits.MaxY)
	}
	return nil
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

func sameBounds(a, b Bounds) bool {
	return samePoint(Point{X: a.MinX, Y: a.MinY}, Point{X: b.MinX, Y: b.MinY}) &&
		samePoint(Point{X: a.MaxX, Y: a.MaxY}, Point{X: b.MaxX, Y: b.MaxY})
}
