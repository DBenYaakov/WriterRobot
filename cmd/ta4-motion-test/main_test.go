package main

import (
	"math"
	"reflect"
	"testing"
)

func TestParseFeeds(t *testing.T) {
	got, err := parseFeeds("200, 400,600")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{200, 400, 600}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseFeeds() = %v, want %v", got, want)
	}
}

func TestParseFeedsRejectsDuplicate(t *testing.T) {
	if _, err := parseFeeds("600,600"); err == nil {
		t.Fatal("parseFeeds accepted duplicate feed")
	}
}

func TestCircleIsClosed(t *testing.T) {
	points := circle(10, 20, 3, 24)
	if len(points) != 25 {
		t.Fatalf("len(circle) = %d, want 25", len(points))
	}
	first := points[0]
	last := points[len(points)-1]
	if math.Abs(first.X-last.X) > 1e-9 || math.Abs(first.Y-last.Y) > 1e-9 {
		t.Fatalf("circle not closed: first=%v last=%v", first, last)
	}
}

func TestCalibrationPatternFitsExpectedWidth(t *testing.T) {
	pattern, err := calibrationPattern(0.10)
	if err != nil {
		t.Fatal(err)
	}
	for _, stroke := range pattern {
		for _, p := range stroke {
			if p.X < 0 || p.X > 177.001 {
				t.Fatalf("point X %.3f outside expected pattern width", p.X)
			}
		}
	}
}
