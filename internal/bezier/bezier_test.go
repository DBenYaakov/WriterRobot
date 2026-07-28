package bezier

import (
	"math"
	"testing"
)

func TestFlattenPreservesEndpoints(t *testing.T) {
	curve := Cubic{P0: Point{1, 2}, P1: Point{3, 8}, P2: Point{7, -4}, P3: Point{10, 5}}
	points, err := Flatten(curve, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if points[0] != curve.P0 {
		t.Fatalf("first point = %#v, want %#v", points[0], curve.P0)
	}
	if points[len(points)-1] != curve.P3 {
		t.Fatalf("last point = %#v, want %#v", points[len(points)-1], curve.P3)
	}
}

func TestFlattenUsesMoreSegmentsForSmallerTolerance(t *testing.T) {
	curve := Cubic{P0: Point{0, 0}, P1: Point{0, 30}, P2: Point{30, 30}, P3: Point{30, 0}}
	coarse, err := Flatten(curve, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	fine, err := Flatten(curve, 0.05)
	if err != nil {
		t.Fatal(err)
	}
	if len(fine) <= len(coarse) {
		t.Fatalf("fine point count %d must exceed coarse point count %d", len(fine), len(coarse))
	}
}

func TestFlattenStraightLine(t *testing.T) {
	curve := Cubic{P0: Point{0, 0}, P1: Point{3, 0}, P2: Point{7, 0}, P3: Point{10, 0}}
	points, err := Flatten(curve, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}
}

func TestFlattenRejectsInvalidTolerance(t *testing.T) {
	for _, tolerance := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := Flatten(Cubic{}, tolerance); err == nil {
			t.Fatalf("Flatten tolerance %v succeeded, want error", tolerance)
		}
	}
}
