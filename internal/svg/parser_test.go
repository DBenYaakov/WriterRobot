package svg

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

func TestParseValidFixtureSuite(t *testing.T) {
	tests := []struct {
		file    string
		strokes int
		closed  []bool
	}{
		{file: "01-line.svg", strokes: 1, closed: []bool{false}},
		{file: "02-rectangle.svg", strokes: 1, closed: []bool{true}},
		{file: "03-circle.svg", strokes: 1, closed: []bool{true}},
		{file: "04-ellipse.svg", strokes: 1, closed: []bool{true}},
		{file: "05-polyline.svg", strokes: 1, closed: []bool{false}},
		{file: "06-polygon.svg", strokes: 1, closed: []bool{true}},
		{file: "07-triangle.svg", strokes: 1, closed: []bool{true}},
		{file: "08-cubic-bezier.svg", strokes: 1, closed: []bool{false}},
		{file: "09-quadratic-bezier.svg", strokes: 1, closed: []bool{false}},
		{file: "10-relative-path.svg", strokes: 1, closed: []bool{true}},
		{file: "11-closed-path.svg", strokes: 1, closed: []bool{true}},
		{file: "12-multiple-strokes.svg", strokes: 2, closed: []bool{false, false}},
		{file: "13-transforms.svg", strokes: 1, closed: []bool{false}},
		{file: "14-viewbox-scale.svg", strokes: 1, closed: []bool{false}},
		{file: "15-nested-transforms.svg", strokes: 1, closed: []bool{false}},
		{file: "16-simple-signature.svg", strokes: 2, closed: []bool{false, false}},
		{file: "hardware-check.svg", strokes: 6, closed: []bool{true, true, true, false, false, false}},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			doc, err := ParseFile(fixturePath(tt.file))
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if len(doc.Drawing.Strokes) != tt.strokes {
				t.Fatalf("strokes = %d, want %d", len(doc.Drawing.Strokes), tt.strokes)
			}
			for i, stroke := range doc.Drawing.Strokes {
				if stroke.Closed != tt.closed[i] {
					t.Fatalf("stroke %d closed = %v, want %v", i+1, stroke.Closed, tt.closed[i])
				}
				if len(stroke.Segments) == 0 {
					t.Fatalf("stroke %d has no segments", i+1)
				}
			}
		})
	}
}

func TestParseLineAndPolylinePointOrder(t *testing.T) {
	doc := mustParseFixture(t, "05-polyline.svg")
	stroke := doc.Drawing.Strokes[0]

	assertPoint(t, stroke.Start, drawing.Point{X: 10, Y: 20}, "start")
	assertSegment(t, stroke.Segments[0], drawing.SegmentLine, drawing.Point{X: 10, Y: 20}, drawing.Point{X: 20, Y: 30})
	assertSegment(t, stroke.Segments[1], drawing.SegmentLine, drawing.Point{X: 20, Y: 30}, drawing.Point{X: 30, Y: 20})
}

func TestParseRectCircleEllipseAndPolygonGeometry(t *testing.T) {
	rect := mustParseFixture(t, "02-rectangle.svg").Drawing.Strokes[0]
	if !rect.Closed {
		t.Fatal("rect is open, want closed")
	}
	assertPoint(t, rect.Start, drawing.Point{X: 10, Y: 20}, "rect start")
	assertSegment(t, rect.Segments[2], drawing.SegmentLine, drawing.Point{X: 40, Y: 60}, drawing.Point{X: 10, Y: 60})

	circle := mustParseFixture(t, "03-circle.svg").Drawing.Strokes[0]
	if len(circle.Segments) != 1 || circle.Segments[0].Kind != drawing.SegmentEllipse {
		t.Fatalf("circle segment = %+v, want one ellipse segment", circle.Segments)
	}
	assertPoint(t, circle.Start, drawing.Point{X: 40, Y: 30}, "circle start")
	assertPoint(t, circle.Segments[0].Center, drawing.Point{X: 30, Y: 30}, "circle center")
	assertAlmost(t, circle.Segments[0].RadiusX, 10, "circle radius X")
	assertAlmost(t, circle.Segments[0].RadiusY, 10, "circle radius Y")

	ellipse := mustParseFixture(t, "04-ellipse.svg").Drawing.Strokes[0]
	assertPoint(t, ellipse.Start, drawing.Point{X: 60, Y: 35}, "ellipse start")
	assertPoint(t, ellipse.Segments[0].Center, drawing.Point{X: 40, Y: 35}, "ellipse center")
	assertAlmost(t, ellipse.Segments[0].RadiusX, 20, "ellipse radius X")
	assertAlmost(t, ellipse.Segments[0].RadiusY, 10, "ellipse radius Y")

	polygon := mustParseFixture(t, "06-polygon.svg").Drawing.Strokes[0]
	if !polygon.Closed {
		t.Fatal("polygon is open, want closed")
	}
	assertSegment(t, polygon.Segments[1], drawing.SegmentLine, drawing.Point{X: 20, Y: 30}, drawing.Point{X: 30, Y: 20})
}

func TestParsePathCommands(t *testing.T) {
	triangle := mustParseFixture(t, "07-triangle.svg").Drawing.Strokes[0]
	if !triangle.Closed {
		t.Fatal("triangle path is open, want closed")
	}
	assertPoint(t, triangle.Start, drawing.Point{X: 10, Y: 40}, "triangle start")
	assertSegment(t, triangle.Segments[0], drawing.SegmentLine, drawing.Point{X: 10, Y: 40}, drawing.Point{X: 30, Y: 40})
	assertSegment(t, triangle.Segments[1], drawing.SegmentLine, drawing.Point{X: 30, Y: 40}, drawing.Point{X: 20, Y: 20})

	relative := mustParseFixture(t, "10-relative-path.svg").Drawing.Strokes[0]
	if len(relative.Segments) != 4 {
		t.Fatalf("relative segments = %d, want 4", len(relative.Segments))
	}
	assertSegment(t, relative.Segments[0], drawing.SegmentLine, drawing.Point{X: 10, Y: 20}, drawing.Point{X: 30, Y: 20})
	assertSegment(t, relative.Segments[1], drawing.SegmentLine, drawing.Point{X: 30, Y: 20}, drawing.Point{X: 40, Y: 20})
	assertSegment(t, relative.Segments[2], drawing.SegmentLine, drawing.Point{X: 40, Y: 20}, drawing.Point{X: 40, Y: 40})
	assertSegment(t, relative.Segments[3], drawing.SegmentLine, drawing.Point{X: 40, Y: 40}, drawing.Point{X: 10, Y: 40})

	multiple := mustParseFixture(t, "12-multiple-strokes.svg")
	assertPoint(t, multiple.Drawing.Strokes[0].Start, drawing.Point{X: 10, Y: 10}, "first subpath start")
	assertPoint(t, multiple.Drawing.Strokes[1].Start, drawing.Point{X: 50, Y: 20}, "second subpath start")
}

func TestParseCurveCommandsPreservesCurveSegments(t *testing.T) {
	cubic := mustParseFixture(t, "08-cubic-bezier.svg").Drawing.Strokes[0]
	if len(cubic.Segments) != 1 || cubic.Segments[0].Kind != drawing.SegmentCubic {
		t.Fatalf("cubic segments = %+v, want one cubic segment", cubic.Segments)
	}
	assertPoint(t, cubic.Segments[0].Control1, drawing.Point{X: 20, Y: 20}, "cubic c1")
	assertPoint(t, cubic.Segments[0].Control2, drawing.Point{X: 40, Y: 20}, "cubic c2")
	assertPoint(t, cubic.Segments[0].End, drawing.Point{X: 50, Y: 50}, "cubic end")

	quadratic := mustParseFixture(t, "09-quadratic-bezier.svg").Drawing.Strokes[0]
	if len(quadratic.Segments) != 2 {
		t.Fatalf("quadratic segments = %d, want 2", len(quadratic.Segments))
	}
	assertSegment(t, quadratic.Segments[0], drawing.SegmentQuadratic, drawing.Point{X: 10, Y: 50}, drawing.Point{X: 50, Y: 50})
	assertPoint(t, quadratic.Segments[0].Control1, drawing.Point{X: 30, Y: 20}, "quadratic control")
	assertSegment(t, quadratic.Segments[1], drawing.SegmentQuadratic, drawing.Point{X: 50, Y: 50}, drawing.Point{X: 90, Y: 50})
	assertPoint(t, quadratic.Segments[1].Control1, drawing.Point{X: 70, Y: 80}, "smooth quadratic reflected control")
}

func TestParseDocumentMetadataAndTransformAttributes(t *testing.T) {
	doc := mustParseFixture(t, "14-viewbox-scale.svg")
	if doc.ViewBox == nil {
		t.Fatal("ViewBox = nil, want parsed viewBox")
	}
	assertAlmost(t, doc.ViewBox.MinX, 0, "viewBox MinX")
	assertAlmost(t, doc.ViewBox.MinY, 0, "viewBox MinY")
	assertAlmost(t, doc.ViewBox.Width, 100, "viewBox Width")
	assertAlmost(t, doc.ViewBox.Height, 50, "viewBox Height")
	if doc.Width == nil || doc.Height == nil {
		t.Fatal("width/height metadata missing")
	}
	assertAlmost(t, *doc.Width, 50, "width")
	assertAlmost(t, *doc.Height, 25, "height")

	transformed := mustParseFixture(t, "13-transforms.svg").Drawing.Strokes[0]
	assertPoint(t, transformed.Transform.Apply(transformed.Start), drawing.Point{X: 10, Y: 20}, "transformed start")
	assertPoint(t, transformed.Transform.Apply(transformed.Segments[0].End), drawing.Point{X: 20, Y: 20}, "transformed end")

	nested := mustParseFixture(t, "15-nested-transforms.svg").Drawing.Strokes[0]
	assertPoint(t, nested.Transform.Apply(nested.Start), drawing.Point{X: 20, Y: 20}, "nested transformed start")
	assertPoint(t, nested.Transform.Apply(nested.Segments[0].End), drawing.Point{X: 40, Y: 20}, "nested transformed end")
}

func TestParseInvalidFixtureSuite(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{file: "invalid-malformed.xml", want: "parse"},
		{file: "invalid-path-data.svg", want: "L command requires coordinates"},
		{file: "invalid-nan.svg", want: "length must be finite"},
		{file: "invalid-empty.svg", want: "no supported drawable geometry"},
		{file: "unsupported-text.svg", want: "unsupported SVG element <text>"},
		{file: "unsupported-image.svg", want: "unsupported SVG element <image>"},
		{file: "unsupported-clip-path.svg", want: "unsupported clip-path"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			_, err := ParseFile(fixturePath(tt.file))
			if err == nil {
				t.Fatal("ParseFile succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsNarrowInlineParserErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unsupported arc command", input: `<svg><path d="M0 0 A10 10 0 0 1 20 20"/></svg>`, want: "unsupported path command"},
		{name: "percent length", input: `<svg width="100%" height="10" viewBox="0 0 10 10"><line x1="0" y1="0" x2="1" y2="1"/></svg>`, want: "percent lengths"},
		{name: "bad optional coordinate", input: `<svg><rect x="bad" y="0" width="10" height="10"/></svg>`, want: "<rect> x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Parse succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func mustParseFixture(t *testing.T, file string) Document {
	t.Helper()
	doc, err := ParseFile(fixturePath(file))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return doc
}

func assertSegment(t *testing.T, got drawing.Segment, wantKind drawing.SegmentKind, wantStart, wantEnd drawing.Point) {
	t.Helper()
	if got.Kind != wantKind {
		t.Fatalf("segment kind = %v, want %v", got.Kind, wantKind)
	}
	assertPoint(t, got.Start, wantStart, "segment start")
	assertPoint(t, got.End, wantEnd, "segment end")
}

func fixturePath(file string) string {
	return filepath.Join("..", "..", "testdata", "svg", file)
}

func assertPoint(t *testing.T, got, want drawing.Point, name string) {
	t.Helper()
	assertAlmost(t, got.X, want.X, name+" X")
	assertAlmost(t, got.Y, want.Y, name+" Y")
}

func assertAlmost(t *testing.T, got, want float64, name string) {
	t.Helper()
	if got > want+0.01 || got < want-0.01 {
		t.Fatalf("%s = %.3f, want %.3f", name, got, want)
	}
}
