package svg

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

type pathToken struct {
	command byte
	number  float64
}

func parsePath(data string, transform drawing.Transform) ([]drawing.VectorStroke, error) {
	tokens, err := tokenizePath(data)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("path data is empty")
	}

	parser := &pathParser{
		tokens:    tokens,
		transform: transform,
	}
	return parser.parse()
}

type pathParser struct {
	tokens    []pathToken
	index     int
	transform drawing.Transform

	current       drawing.Point
	subpathStart  drawing.Point
	currentStroke *drawing.VectorStroke
	strokes       []drawing.VectorStroke
	hasCurrent    bool

	lastCubicControl drawing.Point
	hasCubicControl  bool
	lastQuadControl  drawing.Point
	hasQuadControl   bool
}

func (p *pathParser) parse() ([]drawing.VectorStroke, error) {
	command := byte(0)
	for p.index < len(p.tokens) {
		if p.nextIsCommand() {
			command = p.tokens[p.index].command
			p.index++
		} else if command == 0 {
			return nil, errors.New("path must begin with a command")
		}
		if err := p.runCommand(command); err != nil {
			return nil, err
		}
	}
	p.finishOpenStroke(false)
	if len(p.strokes) == 0 {
		return nil, errors.New("path contains no drawable segments")
	}
	return p.strokes, nil
}

func (p *pathParser) runCommand(command byte) error {
	relative := command >= 'a' && command <= 'z'
	upper := command
	if relative {
		upper -= 'a' - 'A'
	}

	switch upper {
	case 'M':
		if !p.hasNumber() {
			return errors.New("M command requires coordinates")
		}
		first := true
		for p.hasNumber() {
			point, err := p.readPoint(relative)
			if err != nil {
				return fmt.Errorf("M command: %w", err)
			}
			if first {
				p.moveTo(point)
				first = false
			} else {
				p.lineTo(point)
			}
		}
	case 'L':
		if !p.hasNumber() {
			return errors.New("L command requires coordinates")
		}
		for p.hasNumber() {
			point, err := p.readPoint(relative)
			if err != nil {
				return fmt.Errorf("L command: %w", err)
			}
			p.lineTo(point)
		}
	case 'H':
		if !p.hasNumber() {
			return errors.New("H command requires coordinates")
		}
		for p.hasNumber() {
			x, err := p.readNumber()
			if err != nil {
				return fmt.Errorf("H command: %w", err)
			}
			if relative {
				x += p.current.X
			}
			p.lineTo(drawing.Point{X: x, Y: p.current.Y})
		}
	case 'V':
		if !p.hasNumber() {
			return errors.New("V command requires coordinates")
		}
		for p.hasNumber() {
			y, err := p.readNumber()
			if err != nil {
				return fmt.Errorf("V command: %w", err)
			}
			if relative {
				y += p.current.Y
			}
			p.lineTo(drawing.Point{X: p.current.X, Y: y})
		}
	case 'C':
		if !p.hasNumber() {
			return errors.New("C command requires coordinates")
		}
		for p.hasNumber() {
			c1, c2, end, err := p.readCubic(relative)
			if err != nil {
				return err
			}
			if err := p.cubicTo(c1, c2, end); err != nil {
				return err
			}
		}
	case 'S':
		if !p.hasNumber() {
			return errors.New("S command requires coordinates")
		}
		for p.hasNumber() {
			c1 := p.current
			if p.hasCubicControl {
				c1 = reflectPoint(p.lastCubicControl, p.current)
			}
			c2, end, err := p.readSmoothCubic(relative)
			if err != nil {
				return err
			}
			if err := p.cubicTo(c1, c2, end); err != nil {
				return err
			}
		}
	case 'Q':
		if !p.hasNumber() {
			return errors.New("Q command requires coordinates")
		}
		for p.hasNumber() {
			control, end, err := p.readQuadratic(relative)
			if err != nil {
				return err
			}
			if err := p.quadraticTo(control, end); err != nil {
				return err
			}
		}
	case 'T':
		if !p.hasNumber() {
			return errors.New("T command requires coordinates")
		}
		for p.hasNumber() {
			control := p.current
			if p.hasQuadControl {
				control = reflectPoint(p.lastQuadControl, p.current)
			}
			end, err := p.readPoint(relative)
			if err != nil {
				return fmt.Errorf("T command: %w", err)
			}
			if err := p.quadraticTo(control, end); err != nil {
				return err
			}
		}
	case 'Z':
		if !p.hasCurrent {
			return errors.New("Z command before current point")
		}
		p.current = p.subpathStart
		p.finishOpenStroke(true)
	default:
		return fmt.Errorf("unsupported path command %q", string(command))
	}

	if upper != 'C' && upper != 'S' {
		p.hasCubicControl = false
	}
	if upper != 'Q' && upper != 'T' {
		p.hasQuadControl = false
	}
	return nil
}

func (p *pathParser) moveTo(point drawing.Point) {
	p.finishOpenStroke(false)
	p.current = point
	p.subpathStart = point
	p.currentStroke = &drawing.VectorStroke{Start: point, Transform: p.transform}
	p.hasCurrent = true
	p.hasCubicControl = false
	p.hasQuadControl = false
}

func (p *pathParser) lineTo(point drawing.Point) {
	if !p.hasCurrent || p.currentStroke == nil {
		return
	}
	p.currentStroke.Segments = append(p.currentStroke.Segments, drawing.Segment{
		Kind:  drawing.SegmentLine,
		Start: p.current,
		End:   point,
	})
	p.current = point
}

func (p *pathParser) cubicTo(c1, c2, end drawing.Point) error {
	if !p.hasCurrent {
		return errors.New("curve command before current point")
	}
	if p.currentStroke == nil {
		return errors.New("curve command without active subpath")
	}
	p.currentStroke.Segments = append(p.currentStroke.Segments, drawing.Segment{
		Kind:     drawing.SegmentCubic,
		Start:    p.current,
		Control1: c1,
		Control2: c2,
		End:      end,
	})
	p.current = end
	p.lastCubicControl = c2
	p.hasCubicControl = true
	p.hasQuadControl = false
	return nil
}

func (p *pathParser) quadraticTo(control, end drawing.Point) error {
	if !p.hasCurrent {
		return errors.New("curve command before current point")
	}
	if p.currentStroke == nil {
		return errors.New("curve command without active subpath")
	}
	p.currentStroke.Segments = append(p.currentStroke.Segments, drawing.Segment{
		Kind:     drawing.SegmentQuadratic,
		Start:    p.current,
		Control1: control,
		End:      end,
	})
	p.current = end
	p.lastQuadControl = control
	p.hasQuadControl = true
	p.hasCubicControl = false
	return nil
}

func (p *pathParser) finishOpenStroke(closed bool) {
	if p.currentStroke != nil && len(p.currentStroke.Segments) > 0 {
		stroke := *p.currentStroke
		stroke.Closed = closed
		p.strokes = append(p.strokes, stroke)
	}
	p.currentStroke = nil
}

func (p *pathParser) readCubic(relative bool) (drawing.Point, drawing.Point, drawing.Point, error) {
	c1, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, drawing.Point{}, fmt.Errorf("C command: %w", err)
	}
	c2, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, drawing.Point{}, fmt.Errorf("C command: %w", err)
	}
	end, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, drawing.Point{}, fmt.Errorf("C command: %w", err)
	}
	return c1, c2, end, nil
}

func (p *pathParser) readSmoothCubic(relative bool) (drawing.Point, drawing.Point, error) {
	c2, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, fmt.Errorf("S command: %w", err)
	}
	end, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, fmt.Errorf("S command: %w", err)
	}
	return c2, end, nil
}

func (p *pathParser) readQuadratic(relative bool) (drawing.Point, drawing.Point, error) {
	control, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, fmt.Errorf("Q command: %w", err)
	}
	end, err := p.readPoint(relative)
	if err != nil {
		return drawing.Point{}, drawing.Point{}, fmt.Errorf("Q command: %w", err)
	}
	return control, end, nil
}

func (p *pathParser) readPoint(relative bool) (drawing.Point, error) {
	x, err := p.readNumber()
	if err != nil {
		return drawing.Point{}, err
	}
	y, err := p.readNumber()
	if err != nil {
		return drawing.Point{}, err
	}
	if relative {
		x += p.current.X
		y += p.current.Y
	}
	return drawing.Point{X: x, Y: y}, nil
}

func (p *pathParser) readNumber() (float64, error) {
	if p.index >= len(p.tokens) || p.tokens[p.index].command != 0 {
		return 0, errors.New("missing coordinate")
	}
	number := p.tokens[p.index].number
	p.index++
	return number, nil
}

func (p *pathParser) hasNumber() bool {
	return p.index < len(p.tokens) && p.tokens[p.index].command == 0
}

func (p *pathParser) nextIsCommand() bool {
	return p.index < len(p.tokens) && p.tokens[p.index].command != 0
}

func tokenizePath(data string) ([]pathToken, error) {
	var tokens []pathToken
	for i := 0; i < len(data); {
		for i < len(data) && separator(data[i]) {
			i++
		}
		if i >= len(data) {
			break
		}
		if isPathCommand(data[i]) {
			tokens = append(tokens, pathToken{command: data[i]})
			i++
			continue
		}
		start := i
		if data[i] == '+' || data[i] == '-' {
			i++
		}
		digits := 0
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
			digits++
		}
		if i < len(data) && data[i] == '.' {
			i++
			for i < len(data) && data[i] >= '0' && data[i] <= '9' {
				i++
				digits++
			}
		}
		if digits == 0 {
			return nil, fmt.Errorf("expected path number near %q", data[start:])
		}
		if i < len(data) && (data[i] == 'e' || data[i] == 'E') {
			exponent := i
			i++
			if i < len(data) && (data[i] == '+' || data[i] == '-') {
				i++
			}
			exponentDigits := 0
			for i < len(data) && data[i] >= '0' && data[i] <= '9' {
				i++
				exponentDigits++
			}
			if exponentDigits == 0 {
				i = exponent
			}
		}
		number, err := strconv.ParseFloat(data[start:i], 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("invalid path number %q", data[start:i])
		}
		tokens = append(tokens, pathToken{number: number})
	}
	return tokens, nil
}

func isPathCommand(b byte) bool {
	switch unicode.ToUpper(rune(b)) {
	case 'M', 'L', 'H', 'V', 'C', 'S', 'Q', 'T', 'Z', 'A':
		return true
	default:
		return false
	}
}

func reflectPoint(point, about drawing.Point) drawing.Point {
	return drawing.Point{X: 2*about.X - point.X, Y: 2*about.Y - point.Y}
}
