// Package svg imports a small, plotting-oriented subset of SVG.
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

// Anchor controls where transformed SVG geometry lands relative to the program
// origin.
type Anchor string

const (
	// AnchorTopLeft places the transformed SVG's top-left source corner at X0 Y0.
	AnchorTopLeft Anchor = "top-left"
	// AnchorCenter places the transformed SVG's center at X0 Y0.
	AnchorCenter Anchor = "center"
)

// Options controls SVG import.
type Options struct {
	FlattenTolerance float64
	FitWidth         float64
	FitHeight        float64
	Anchor           Anchor
}

// DefaultOptions returns conservative SVG import defaults.
func DefaultOptions() Options {
	return Options{FlattenTolerance: 0.10, Anchor: AnchorTopLeft}
}

// ParseFile loads and parses an SVG file.
func ParseFile(path string, opts Options) (drawing.Drawing, error) {
	file, err := os.Open(path)
	if err != nil {
		return drawing.Drawing{}, fmt.Errorf("open SVG: %w", err)
	}
	defer file.Close()
	return Parse(file, opts)
}

// Parse parses SVG geometry into WriterRobot program coordinates.
func Parse(r io.Reader, opts Options) (drawing.Drawing, error) {
	opts = opts.withDefaults()
	if err := validateOptions(opts); err != nil {
		return drawing.Drawing{}, err
	}

	root, err := decode(r)
	if err != nil {
		return drawing.Drawing{}, err
	}
	if root.name != "svg" {
		return drawing.Drawing{}, fmt.Errorf("root element is <%s>, want <svg>", root.name)
	}

	importer := &importer{tolerance: opts.FlattenTolerance}
	if err := importer.process(root, identityMatrix()); err != nil {
		return drawing.Drawing{}, err
	}
	if len(importer.strokes) == 0 {
		return drawing.Drawing{}, errors.New("SVG contains no supported drawable geometry")
	}

	source, err := root.sourceBounds(importer.strokes)
	if err != nil {
		return drawing.Drawing{}, err
	}
	scale, err := root.scaleFor(source, opts)
	if err != nil {
		return drawing.Drawing{}, err
	}
	return transformToProgram(importer.strokes, source, scale, opts.Anchor)
}

type element struct {
	name     string
	attrs    map[string]string
	children []element
}

type importer struct {
	tolerance float64
	strokes   []drawing.Stroke
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
		name:  start.Name.Local,
		attrs: make(map[string]string, len(start.Attr)),
	}
	for _, attr := range start.Attr {
		elem.attrs[attr.Name.Local] = attr.Value
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return element{}, fmt.Errorf("parse <%s>: %w", elem.name, err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			child, err := readElement(decoder, typed)
			if err != nil {
				return element{}, err
			}
			elem.children = append(elem.children, child)
		case xml.EndElement:
			if typed.Name.Local == elem.name {
				return elem, nil
			}
		}
	}
}

func (i *importer) process(elem element, parent matrix) error {
	if ignoredElement(elem.name) {
		return nil
	}
	if err := rejectUnsafeAttributes(elem); err != nil {
		return err
	}
	local, err := parseTransform(elem.attrs["transform"])
	if err != nil {
		return fmt.Errorf("<%s> transform: %w", elem.name, err)
	}
	transform := multiply(parent, local)

	switch elem.name {
	case "svg", "g":
		for _, child := range elem.children {
			if err := i.process(child, transform); err != nil {
				return err
			}
		}
	case "path":
		strokes, err := parsePath(elem.attrs["d"], i.tolerance, transform)
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
		stroke, err := parseCircle(elem, i.tolerance, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	case "ellipse":
		stroke, err := parseEllipse(elem, i.tolerance, transform)
		if err != nil {
			return err
		}
		i.strokes = append(i.strokes, stroke)
	default:
		return fmt.Errorf("unsupported SVG element <%s>", elem.name)
	}
	return nil
}

func parseLine(elem element, transform matrix) (drawing.Stroke, error) {
	x1, err := requiredLength(elem, "x1")
	if err != nil {
		return drawing.Stroke{}, err
	}
	y1, err := requiredLength(elem, "y1")
	if err != nil {
		return drawing.Stroke{}, err
	}
	x2, err := requiredLength(elem, "x2")
	if err != nil {
		return drawing.Stroke{}, err
	}
	y2, err := requiredLength(elem, "y2")
	if err != nil {
		return drawing.Stroke{}, err
	}
	return drawing.Stroke{Points: []drawing.Point{
		transform.apply(drawing.Point{X: x1, Y: y1}),
		transform.apply(drawing.Point{X: x2, Y: y2}),
	}}, nil
}

func parsePolyline(elem element, closed bool, transform matrix) (drawing.Stroke, error) {
	points, err := parsePoints(elem.attrs["points"], transform)
	if err != nil {
		return drawing.Stroke{}, fmt.Errorf("<%s>: %w", elem.name, err)
	}
	if len(points) < 2 {
		return drawing.Stroke{}, fmt.Errorf("<%s> contains fewer than two points", elem.name)
	}
	return drawing.Stroke{Points: points, Closed: closed}, nil
}

func parseRect(elem element, transform matrix) (drawing.Stroke, error) {
	rx, err := optionalLengthAttr(elem, "rx")
	if err != nil {
		return drawing.Stroke{}, err
	}
	ry, err := optionalLengthAttr(elem, "ry")
	if err != nil {
		return drawing.Stroke{}, err
	}
	if rx > 0 || ry > 0 {
		return drawing.Stroke{}, errors.New("<rect> with rounded corners is not supported")
	}
	x, err := optionalLengthAttr(elem, "x")
	if err != nil {
		return drawing.Stroke{}, err
	}
	y, err := optionalLengthAttr(elem, "y")
	if err != nil {
		return drawing.Stroke{}, err
	}
	width, err := requiredLength(elem, "width")
	if err != nil {
		return drawing.Stroke{}, err
	}
	height, err := requiredLength(elem, "height")
	if err != nil {
		return drawing.Stroke{}, err
	}
	if width <= 0 || height <= 0 {
		return drawing.Stroke{}, errors.New("<rect> width and height must be greater than zero")
	}
	points := []drawing.Point{
		{X: x, Y: y},
		{X: x + width, Y: y},
		{X: x + width, Y: y + height},
		{X: x, Y: y + height},
	}
	return drawing.Stroke{Points: transformPoints(points, transform), Closed: true}, nil
}

func parseCircle(elem element, tolerance float64, transform matrix) (drawing.Stroke, error) {
	cx, err := optionalLengthAttr(elem, "cx")
	if err != nil {
		return drawing.Stroke{}, err
	}
	cy, err := optionalLengthAttr(elem, "cy")
	if err != nil {
		return drawing.Stroke{}, err
	}
	r, err := requiredLength(elem, "r")
	if err != nil {
		return drawing.Stroke{}, err
	}
	if r <= 0 {
		return drawing.Stroke{}, errors.New("<circle> radius must be greater than zero")
	}
	return ellipseStroke(cx, cy, r, r, tolerance, transform), nil
}

func parseEllipse(elem element, tolerance float64, transform matrix) (drawing.Stroke, error) {
	cx, err := optionalLengthAttr(elem, "cx")
	if err != nil {
		return drawing.Stroke{}, err
	}
	cy, err := optionalLengthAttr(elem, "cy")
	if err != nil {
		return drawing.Stroke{}, err
	}
	rx, err := requiredLength(elem, "rx")
	if err != nil {
		return drawing.Stroke{}, err
	}
	ry, err := requiredLength(elem, "ry")
	if err != nil {
		return drawing.Stroke{}, err
	}
	if rx <= 0 || ry <= 0 {
		return drawing.Stroke{}, errors.New("<ellipse> radii must be greater than zero")
	}
	return ellipseStroke(cx, cy, rx, ry, tolerance, transform), nil
}

func ellipseStroke(cx, cy, rx, ry, tolerance float64, transform matrix) drawing.Stroke {
	segments := segmentsForRadius(math.Max(rx, ry), tolerance)
	points := make([]drawing.Point, 0, segments)
	for i := 0; i < segments; i++ {
		angle := 2 * math.Pi * float64(i) / float64(segments)
		points = append(points, transform.apply(drawing.Point{
			X: cx + rx*math.Cos(angle),
			Y: cy + ry*math.Sin(angle),
		}))
	}
	return drawing.Stroke{Points: points, Closed: true}
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

func transformPoints(points []drawing.Point, transform matrix) []drawing.Point {
	result := make([]drawing.Point, 0, len(points))
	for _, point := range points {
		result = append(result, transform.apply(point))
	}
	return result
}

func parsePoints(value string, transform matrix) ([]drawing.Point, error) {
	numbers, err := parseNumberList(value)
	if err != nil {
		return nil, err
	}
	if len(numbers)%2 != 0 {
		return nil, errors.New("points attribute contains an odd number of coordinates")
	}
	points := make([]drawing.Point, 0, len(numbers)/2)
	for i := 0; i < len(numbers); i += 2 {
		points = append(points, transform.apply(drawing.Point{X: numbers[i], Y: numbers[i+1]}))
	}
	return points, nil
}

func (elem element) sourceBounds(strokes []drawing.Stroke) (sourceRect, error) {
	if viewBox, ok, err := parseViewBox(elem.attrs["viewBox"]); err != nil {
		return sourceRect{}, err
	} else if ok {
		return viewBox, nil
	}
	d, err := drawing.New(strokes)
	if err != nil {
		return sourceRect{}, err
	}
	return sourceRect{MinX: d.Bounds.MinX, MinY: d.Bounds.MinY, Width: d.Bounds.Width(), Height: d.Bounds.Height()}, nil
}

func (elem element) scaleFor(source sourceRect, opts Options) (float64, error) {
	if source.Width <= 0 || source.Height <= 0 {
		return 0, errors.New("SVG source bounds must have non-zero width and height")
	}

	scale := 1.0
	width, hasWidth, err := optionalLength(elem.attrs["width"])
	if err != nil {
		return 0, fmt.Errorf("<svg> width: %w", err)
	}
	height, hasHeight, err := optionalLength(elem.attrs["height"])
	if err != nil {
		return 0, fmt.Errorf("<svg> height: %w", err)
	}
	if hasWidth && hasHeight {
		scale = math.Min(width/source.Width, height/source.Height)
	}
	if opts.FitWidth > 0 && opts.FitHeight > 0 {
		scale = math.Min(opts.FitWidth/source.Width, opts.FitHeight/source.Height)
	} else if opts.FitWidth > 0 {
		scale = opts.FitWidth / source.Width
	} else if opts.FitHeight > 0 {
		scale = opts.FitHeight / source.Height
	}
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 0, errors.New("SVG scale must be finite and greater than zero")
	}
	return scale, nil
}

func transformToProgram(strokes []drawing.Stroke, source sourceRect, scale float64, anchor Anchor) (drawing.Drawing, error) {
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

type sourceRect struct {
	MinX   float64
	MinY   float64
	Width  float64
	Height float64
}

func parseViewBox(value string) (sourceRect, bool, error) {
	if strings.TrimSpace(value) == "" {
		return sourceRect{}, false, nil
	}
	numbers, err := parseNumberList(value)
	if err != nil {
		return sourceRect{}, false, fmt.Errorf("parse viewBox: %w", err)
	}
	if len(numbers) != 4 {
		return sourceRect{}, false, errors.New("viewBox must contain four numbers")
	}
	if numbers[2] <= 0 || numbers[3] <= 0 {
		return sourceRect{}, false, errors.New("viewBox width and height must be greater than zero")
	}
	return sourceRect{MinX: numbers[0], MinY: numbers[1], Width: numbers[2], Height: numbers[3]}, true, nil
}

func requiredLength(elem element, name string) (float64, error) {
	value, ok := elem.attrs[name]
	if !ok || strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("<%s> missing required %s attribute", elem.name, name)
	}
	parsed, _, err := optionalLength(value)
	if err != nil {
		return 0, fmt.Errorf("<%s> %s: %w", elem.name, name, err)
	}
	return parsed, nil
}

func optionalLengthAttr(elem element, name string) (float64, error) {
	parsed, _, err := optionalLength(elem.attrs[name])
	if err != nil {
		return 0, fmt.Errorf("<%s> %s: %w", elem.name, name, err)
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

func ignoredElement(name string) bool {
	switch name {
	case "defs", "title", "desc", "metadata", "style":
		return true
	default:
		return false
	}
}

func rejectUnsafeAttributes(elem element) error {
	for _, name := range []string{"clip-path", "mask"} {
		if value := strings.TrimSpace(elem.attrs[name]); value != "" {
			return fmt.Errorf("<%s> uses unsupported %s attribute", elem.name, name)
		}
	}
	return nil
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
	switch opts.Anchor {
	case AnchorTopLeft, AnchorCenter:
		return nil
	default:
		return fmt.Errorf("unsupported SVG anchor %q", opts.Anchor)
	}
}
