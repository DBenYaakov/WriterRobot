package svg_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/geometry"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
	"github.com/DBenYaakov/WriterRobot/internal/plot"
	svgimport "github.com/DBenYaakov/WriterRobot/internal/svg"
)

func TestParseInkscapeSignatureFixture(t *testing.T) {
	doc, err := svgimport.ParseFile(fixturePath("17-inkscape-signature.svg"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(doc.Drawing.Strokes) != 29 {
		t.Fatalf("strokes = %d, want 29 retained signature paths", len(doc.Drawing.Strokes))
	}
	for i, stroke := range doc.Drawing.Strokes {
		if len(stroke.Segments) == 0 {
			t.Fatalf("signature stroke %d has no segments", i+1)
		}
	}
	if doc.ViewBox == nil {
		t.Fatal("ViewBox = nil, want parsed Inkscape viewBox")
	}
	assertAlmost(t, doc.ViewBox.MinX, 51, "viewBox MinX")
	assertAlmost(t, doc.ViewBox.MinY, 141, "viewBox MinY")
	assertAlmost(t, doc.ViewBox.Width, 1808, "viewBox Width")
	assertAlmost(t, doc.ViewBox.Height, 563, "viewBox Height")

	geometryOpts := geometry.DefaultOptions()
	geometryOpts.FitWidth = 80
	d, err := geometry.Process(geometrySource(doc), geometryOpts)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if err := geometry.Preflight(d, geometry.WorkBounds(100, 100)); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	ops, err := plot.Plan(d, plot.DefaultOptions(0.5, 1.7))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	assertPlotPenSequencing(t, ops, 25)
	if _, err := machine.ProgramFromPlan(ops); err != nil {
		t.Fatalf("ProgramFromPlan: %v", err)
	}
}

func TestParseIgnoresSodipodiNamedviewByNamespace(t *testing.T) {
	doc := mustParse(t, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd">
  <sodipodi:namedview><path d="M 0 0 L 1 1"/></sodipodi:namedview>
  <line x1="0" y1="0" x2="10" y2="10"/>
</svg>`)
	assertStrokeCount(t, doc, 1)
}

func TestParseIgnoresDifferentlyPrefixedSodipodiNamedview(t *testing.T) {
	doc := mustParse(t, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:editor="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd">
  <editor:namedview><path d="M 0 0 L 1 1"/></editor:namedview>
  <line x1="0" y1="0" x2="10" y2="10"/>
</svg>`)
	assertStrokeCount(t, doc, 1)
}

func TestParseIgnoresMetadataSubtrees(t *testing.T) {
	doc := mustParse(t, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:cc="http://creativecommons.org/ns#" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <title><text x="0" y="0">ignored title text</text></title>
  <desc><image href="ignored.png"/></desc>
  <metadata><rdf:RDF><cc:Work><dc:title>ignored metadata</dc:title><path d="M0 0 L1 1"/></cc:Work></rdf:RDF></metadata>
  <defs><path d="M0 0 L1 1"/></defs>
  <line x1="0" y1="0" x2="10" y2="10"/>
</svg>`)
	assertStrokeCount(t, doc, 1)
}

func TestParseUnsupportedDrawableSVGElementsStillError(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "text", input: `<text x="0" y="0">hi</text>`, want: "unsupported SVG element <text>"},
		{name: "image", input: `<image href="example.png" x="0" y="0" width="10" height="10"/>`, want: "unsupported SVG element <image>"},
		{name: "use", input: `<use href="#p"/>`, want: "unsupported SVG element <use>"},
		{name: "clipPath", input: `<clipPath id="c"/>`, want: "unsupported SVG element <clipPath>"},
		{name: "mask", input: `<mask id="m"/>`, want: "unsupported SVG element <mask>"},
		{name: "pattern", input: `<pattern id="p"/>`, want: "unsupported SVG element <pattern>"},
		{name: "filter", input: `<filter id="f"/>`, want: "unsupported SVG element <filter>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svgimport.Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg">` + tt.input + `</svg>`))
			if err == nil {
				t.Fatal("Parse succeeded, want unsupported drawable error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseDrawableChildrenAfterIgnoredMetadata(t *testing.T) {
	doc := mustParse(t, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" sodipodi:docname="example.svg">
  <title>ignored</title>
  <metadata><ignored>ignored</ignored></metadata>
  <sodipodi:namedview inkscape:current-layer="layer1"/>
  <g id="layer1" inkscape:label="Layer 1" inkscape:groupmode="layer">
    <path d="M0 0 L10 0"/>
  </g>
</svg>`)
	assertStrokeCount(t, doc, 1)
}

func mustParse(t *testing.T, input string) svgimport.Document {
	t.Helper()
	doc, err := svgimport.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func assertStrokeCount(t *testing.T, doc svgimport.Document, want int) {
	t.Helper()
	if len(doc.Drawing.Strokes) != want {
		t.Fatalf("strokes = %d, want %d", len(doc.Drawing.Strokes), want)
	}
}

func geometrySource(doc svgimport.Document) geometry.Source {
	source := geometry.Source{
		Drawing: doc.Drawing,
		Width:   doc.Width,
		Height:  doc.Height,
	}
	if doc.ViewBox != nil {
		source.ViewBox = &geometry.Rect{
			MinX:   doc.ViewBox.MinX,
			MinY:   doc.ViewBox.MinY,
			Width:  doc.ViewBox.Width,
			Height: doc.ViewBox.Height,
		}
	}
	return source
}

func assertPlotPenSequencing(t *testing.T, ops []plot.Operation, wantPenDowns int) {
	t.Helper()
	penDown := false
	penDowns := 0
	for _, op := range ops {
		switch op.Kind {
		case plot.OperationPenDown:
			if penDown {
				t.Fatal("lowered pen while already down")
			}
			penDown = true
			penDowns++
		case plot.OperationPenUp:
			penDown = false
		case plot.OperationRapidMove:
			if penDown {
				t.Fatalf("rapid move while pen is down: %+v", op)
			}
		case plot.OperationDrawMove:
			if !penDown {
				t.Fatalf("draw move while pen is raised: %+v", op)
			}
		}
	}
	if penDown {
		t.Fatal("plot ended with pen down")
	}
	if penDowns != wantPenDowns {
		t.Fatalf("pen-down transitions = %d, want %d", penDowns, wantPenDowns)
	}
}

func fixturePath(file string) string {
	return filepath.Join("..", "..", "testdata", "svg", file)
}

func assertAlmost(t *testing.T, got, want float64, name string) {
	t.Helper()
	if got > want+0.01 || got < want-0.01 {
		t.Fatalf("%s = %.3f, want %.3f", name, got, want)
	}
}
