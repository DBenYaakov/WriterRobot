// Package bezier provides cubic Bezier curve flattening for GRBL G-code.
package bezier

import (
	"errors"
	"math"
)

type Point struct {
	X float64
	Y float64
}

type Cubic struct {
	P0 Point
	P1 Point
	P2 Point
	P3 Point
}

const maxSubdivisionDepth = 32

func Flatten(curve Cubic, tolerance float64) ([]Point, error) {
	if tolerance <= 0 || math.IsNaN(tolerance) || math.IsInf(tolerance, 0) {
		return nil, errors.New("tolerance must be a finite number greater than zero")
	}
	if !finiteCurve(curve) {
		return nil, errors.New("curve coordinates must be finite")
	}
	points := []Point{curve.P0}
	flatten(&points, curve, tolerance, 0)
	return points, nil
}

func flatten(points *[]Point, curve Cubic, tolerance float64, depth int) {
	if depth >= maxSubdivisionDepth || flatEnough(curve, tolerance) {
		*points = append(*points, curve.P3)
		return
	}
	left, right := split(curve)
	flatten(points, left, tolerance, depth+1)
	flatten(points, right, tolerance, depth+1)
}

func flatEnough(curve Cubic, tolerance float64) bool {
	return pointLineDistance(curve.P1, curve.P0, curve.P3) <= tolerance && pointLineDistance(curve.P2, curve.P0, curve.P3) <= tolerance
}

func pointLineDistance(point, start, end Point) float64 {
	dx := end.X - start.X
	dy := end.Y - start.Y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return math.Hypot(point.X-start.X, point.Y-start.Y)
	}
	return math.Abs(dy*point.X-dx*point.Y+end.X*start.Y-end.Y*start.X) / length
}

func split(curve Cubic) (Cubic, Cubic) {
	p01 := midpoint(curve.P0, curve.P1)
	p12 := midpoint(curve.P1, curve.P2)
	p23 := midpoint(curve.P2, curve.P3)
	p012 := midpoint(p01, p12)
	p123 := midpoint(p12, p23)
	p0123 := midpoint(p012, p123)
	return Cubic{P0: curve.P0, P1: p01, P2: p012, P3: p0123}, Cubic{P0: p0123, P1: p123, P2: p23, P3: curve.P3}
}

func midpoint(a, b Point) Point {
	return Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
}

func finiteCurve(curve Cubic) bool {
	for _, point := range [...]Point{curve.P0, curve.P1, curve.P2, curve.P3} {
		if math.IsNaN(point.X) || math.IsNaN(point.Y) || math.IsInf(point.X, 0) || math.IsInf(point.Y, 0) {
			return false
		}
	}
	return true
}
