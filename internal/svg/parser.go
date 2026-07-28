// Package svg imports a small, plotting-oriented subset of SVG into neutral
// vector drawing geometry.
//
// The parser is limited to XML, supported SVG elements, path syntax, and SVG
// document metadata. Coordinate normalization, viewBox scaling, curve
// flattening, work-area fitting, and plot planning happen in later layers.
package svg

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

const (
	svgNamespace      = "http://www.w3.org/2000/svg"
	sodipodiNamespace = "http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd"
	rdfNamespace      = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	ccNamespace       = "http://creativecommons.org/ns#"
	dcNamespace       = "http://purl.org/dc/elements/1.1/"
)

// Document is parsed SVG vector geometry plus source-viewport metadata.
type Document struct {
	Drawing drawing.VectorDrawing
	ViewBox *ViewBox
	Width   *float64
	Height  *float64
}

// ViewBox is the parsed SVG viewBox rectangle.
type ViewBox struct {
	MinX   float64
	MinY   float64
	Width  float64
	Height float64
}

// ParseFile loads and parses an SVG file.
func ParseFile(path string) (Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return Document{}, fmt.Errorf("open SVG: %w", err)
	}
	defer file.Close()
	return Parse(file)
}

// Parse parses supported SVG geometry into source-coordinate vector strokes.
func Parse(r io.Reader) (Document, error) {
	root, err := decode(r)
	if err != nil {
		return Document{}, err
	}
	if root.local != "svg" || !root.inSVGNamespace() {
		return Document{}, fmt.Errorf("root element is <%s>, want <svg>", root.displayName())
	}

	viewBox, err := parseViewBox(root.attrs["viewBox"])
	if err != nil {
		return Document{}, err
	}
	width, err := optionalRootLength(root, "width")
	if err != nil {
		return Document{}, err
	}
	height, err := optionalRootLength(root, "height")
	if err != nil {
		return Document{}, err
	}

	importer := &importer{}
	if err := importer.process(root, drawing.IdentityTransform()); err != nil {
		return Document{}, err
	}
	if len(importer.strokes) == 0 {
		return Document{}, errors.New("SVG contains no supported drawable geometry")
	}
	return Document{
		Drawing: drawing.VectorDrawing{Strokes: importer.strokes},
		ViewBox: viewBox,
		Width:   width,
		Height:  height,
	}, nil
}

type element struct {
	namespace string
	local     string
	attrs     map[string]string
	children  []element
}

type importer struct {
	strokes []drawing.VectorStroke
}

func decode(r io.Reader) (element, error) {
	decoder := xml.NewDecoder(r)
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return element{}, errors.New("SVG is empty")
			}
			return element{}, fmt.Errorf("parse SVG XML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		return readElement(decoder, start)
	}
}

func readElement(decoder *xml.Decoder, start xml.StartElement) (element, error) {
	elem := element{
		namespace: start.Name.Space,
		local:     start.Name.Local,
		attrs:     make(map[string]string, len(start.Attr)),
	}
	for _, attr := range start.Attr {
		elem.attrs[attr.Name.Local] = attr.Value
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return element{}, fmt.Errorf("parse <%s>: %w", elem.displayName(), err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			child, err := readElement(decoder, typed)
			if err != nil {
				return element{}, err
			}
			elem.children = append(elem.children, child)
		case xml.EndElement:
			if typed.Name.Space == elem.namespace && typed.Name.Local == elem.local {
				return elem, nil
			}
		}
	}
}

func (i *importer) process(elem element, parent drawing.Transform) error {
	if ignoredMetadataElement(elem) {
		return nil
	}
	if !elem.inSVGNamespace() {
		return fmt.Errorf("unsupported SVG element <%s>", elem.displayName())
	}
	if err := rejectUnsafeAttributes(elem); err != nil {
		return err
	}
	local, err := parseTransform(elem.attrs["transform"])
	if err != nil {
		return fmt.Errorf("<%s> transform: %w", elem.displayName(), err)
	}
	transform := parent.Then(local)

	switch elem.local {
	case "svg", "g":
		for _, child := range elem.children {
			if err := i.process(child, transform); err != nil {
				return err
			}
		}
	case "path":
		strokes, err := parsePath(elem.attrs["d"], transform)
		if err != nil {
			return fmt.Errorf("<path>: %w", err)
		}
		i.strokes = append(i.strokes, strokes...)
	case "line":
		stroke, err := parseLine(elem, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	case "polyline":
		stroke, err := parsePolyline(elem, false, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	case "polygon":
		stroke, err := parsePolyline(elem, true, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	case "rect":
		stroke, err := parseRect(elem, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	case "circle":
		stroke, err := parseCircle(elem, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	case "ellipse":
		stroke, err := parseEllipse(elem, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	default:
		return fmt.Errorf("unsupported SVG element <%s>", elem.displayName())
	}
	return nil
}

func parseLine(elem element, transform drawing.Transform) (drawing.VectorStroke, error) {
	x1, err := requiredLength(elem, "x1")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	y1, err := requiredLength(elem, "y1")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	x2, err := requiredLength(elem, "x2")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	y2, err := requiredLength(elem, "y2")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	return strokeFromPoints([]drawing.Point{{X: x1, Y: y1}, {X: x2, Y: y2}}, false, transform), nil
}

func parsePolyline(elem element, closed bool, transform drawing.Transform) (drawing.VectorStroke, error) {
	points, err := parsePoints(elem.attrs["points"])
	if err != nil {
		return drawing.VectorStroke{}, fmt.Errorf("<%s>: %w", elem.displayName(), err)
	}
	if len(points) < 2 {
		return drawing.VectorStroke{}, fmt.Errorf("<%s> contains fewer than two points", elem.displayName())
	}
	return strokeFromPoints(points, closed, transform), nil
}

func parseRect(elem element, transform drawing.Transform) (drawing.VectorStroke, error) {
	rx, err := optionalLengthAttr(elem, "rx")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	ry, err := optionalLengthAttr(elem, "ry")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	if rx > 0 || ry > 0 {
		return drawing.VectorStroke{}, errors.New("<rect> with rounded corners is not supported")
	}
	x, err := optionalLengthAttr(elem, "x")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	y, err := optionalLengthAttr(elem, "y")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	width, err := requiredLength(elem, "width")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	height, err := requiredLength(elem, "height")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	if width <= 0 || height <= 0 {
		return drawing.VectorStroke{}, errors.New("<rect> width and height must be greater than zero")
	}
	points := []drawing.Point{
		{X: x, Y: y},
		{X: x + width, Y: y},
		{X: x + width, Y: y + height},
		{X: x, Y: y + height},
	}
	return strokeFromPoints(points, true, transform), nil
}

func parseCircle(elem element, transform drawing.Transform) (drawing.VectorStroke, error) {
	cx, err := optionalLengthAttr(elem, "cx")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	cy, err := optionalLengthAttr(elem, "cy")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	r, err := requiredLength(elem, "r")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	if r <= 0 {
		return drawing.VectorStroke{}, errors.New("<circle> radius must be greater than zero")
	}
	return ellipseStroke(cx, cy, r, r, transform), nil
}

func parseEllipse(elem element, transform drawing.Transform) (drawing.VectorStroke, error) {
	cx, err := optionalLengthAttr(elem, "cx")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	cy, err := optionalLengthAttr(elem, "cy")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	rx, err := requiredLength(elem, "rx")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	ry, err := requiredLength(elem, "ry")
	if err != nil {
		return drawing.VectorStroke{}, err
	}
	if rx <= 0 || ry <= 0 {
		return drawing.VectorStroke{}, errors.New("<ellipse> radii must be greater than zero")
	}
	return ellipseStroke(cx, cy, rx, ry, transform), nil
}

func ellipseStroke(cx, cy, rx, ry float64, transform drawing.Transform) drawing.VectorStroke {
	start := drawing.Point{X: cx + rx, Y: cy}
	return drawing.VectorStroke{
		Start: start,
		Segments: []drawing.Segment{{
			Kind:    drawing.SegmentEllipse,
			Start:   start,
			End:     start,
			Center:  drawing.Point{X: cx, Y: cy},
			RadiusX: rx,
			RadiusY: ry,
		}},
		Closed:    true,
		Transform: transform,
	}
}

func strokeFromPoints(points []drawing.Point, closed bool, transform drawing.Transform) drawing.VectorStroke {
	segments := make([]drawing.Segment, 0, len(points)-1)
	for i := 1; i < len(points); i++ {
		segments = append(segments, drawing.Segment{
			Kind:  drawing.SegmentLine,
			Start: points[i-1],
			End:   points[i],
		})
	}
	return drawing.VectorStroke{
		Start:     points[0],
		Segments:  segments,
		Closed:    closed,
		Transform: transform,
	}
}

func parsePoints(value string) ([]drawing.Point, error) {
	numbers, err := parseNumberList(value)
	if err != nil {
		return nil, err
	}
	if len(numbers)%2 != 0 {
		return nil, errors.New("points attribute contains an odd number of coordinates")
	}
	points := make([]drawing.Point, 0, len(numbers)/2)
	for i := 0; i < len(numbers); i += 2 {
		points = append(points, drawing.Point{X: numbers[i], Y: numbers[i+1]})
	}
	return points, nil
}

func parseViewBox(value string) (*ViewBox, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	numbers, err := parseNumberList(value)
	if err != nil {
		return nil, fmt.Errorf("parse viewBox: %w", err)
	}
	if len(numbers) != 4 {
		return nil, errors.New("viewBox must contain four numbers")
	}
	if numbers[2] <= 0 || numbers[3] <= 0 {
		return nil, errors.New("viewBox width and height must be greater than zero")
	}
	return &ViewBox{MinX: numbers[0], MinY: numbers[1], Width: numbers[2], Height: numbers[3]}, nil
}

func optionalRootLength(elem element, name string) (*float64, error) {
	parsed, ok, err := optionalLength(elem.attrs[name])
	if err != nil {
		return nil, fmt.Errorf("<svg> %s: %w", name, err)
	}
	if !ok {
		return nil, nil
	}
	return &parsed, nil
}

func requiredLength(elem element, name string) (float64, error) {
	value, ok := elem.attrs[name]
	if !ok || strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("<%s> missing required %s attribute", elem.displayName(), name)
	}
	parsed, _, err := optionalLength(value)
	if err != nil {
		return 0, fmt.Errorf("<%s> %s: %w", elem.displayName(), name, err)
	}
	return parsed, nil
}

func optionalLengthAttr(elem element, name string) (float64, error) {
	parsed, _, err := optionalLength(elem.attrs[name])
	if err != nil {
		return 0, fmt.Errorf("<%s> %s: %w", elem.displayName(), name, err)
	}
	return parsed, nil
}

func optionalLength(value string) (float64, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	if strings.HasSuffix(value, "%") {
		return 0, false, errors.New("percent lengths are not supported")
	}
	multiplier := 1.0
	for _, unit := range []struct {
		suffix string
		scale  float64
	}{
		{suffix: "mm", scale: 1},
		{suffix: "cm", scale: 10},
		{suffix: "in", scale: 25.4},
		{suffix: "pt", scale: 25.4 / 72},
		{suffix: "px", scale: 1},
	} {
		if strings.HasSuffix(value, unit.suffix) {
			multiplier = unit.scale
			value = strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
			break
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid length %q", value)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false, errors.New("length must be finite")
	}
	return number * multiplier, true, nil
}

func parseNumberList(value string) ([]float64, error) {
	tokens, err := scanNumbers(value)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("expected at least one number")
	}
	return tokens, nil
}

func (elem element) inSVGNamespace() bool {
	return elem.namespace == "" || elem.namespace == svgNamespace
}

func (elem element) displayName() string {
	return elem.local
}

func ignoredMetadataElement(elem element) bool {
	if elem.inSVGNamespace() {
		switch elem.local {
		case "defs", "title", "desc", "metadata", "style":
			return true
		default:
			return false
		}
	}
	switch elem.namespace {
	case sodipodiNamespace:
		return elem.local == "namedview"
	case rdfNamespace, ccNamespace, dcNamespace:
		return true
	default:
		return false
	}
}

func rejectUnsafeAttributes(elem element) error {
	for _, name := range []string{"clip-path", "mask", "filter"} {
		if value := strings.TrimSpace(elem.attrs[name]); value != "" {
			return fmt.Errorf("<%s> uses unsupported %s attribute", elem.displayName(), name)
		}
	}
	return nil
}
