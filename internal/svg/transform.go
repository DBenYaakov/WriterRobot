package svg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

type matrix struct {
	a float64
	b float64
	c float64
	d float64
	e float64
	f float64
}

func identityMatrix() matrix {
	return matrix{a: 1, d: 1}
}

func translateMatrix(tx, ty float64) matrix {
	return matrix{a: 1, d: 1, e: tx, f: ty}
}

func scaleMatrix(sx, sy float64) matrix {
	return matrix{a: sx, d: sy}
}

func multiply(parent, child matrix) matrix {
	return matrix{
		a: parent.a*child.a + parent.c*child.b,
		b: parent.b*child.a + parent.d*child.b,
		c: parent.a*child.c + parent.c*child.d,
		d: parent.b*child.c + parent.d*child.d,
		e: parent.a*child.e + parent.c*child.f + parent.e,
		f: parent.b*child.e + parent.d*child.f + parent.f,
	}
}

func (m matrix) apply(point drawing.Point) drawing.Point {
	return drawing.Point{
		X: m.a*point.X + m.c*point.Y + m.e,
		Y: m.b*point.X + m.d*point.Y + m.f,
	}
}

func parseTransform(value string) (matrix, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return identityMatrix(), nil
	}
	result := identityMatrix()
	for value != "" {
		value = strings.TrimLeftFunc(value, unicode.IsSpace)
		if value == "" {
			break
		}
		nameEnd := 0
		for nameEnd < len(value) && (unicode.IsLetter(rune(value[nameEnd])) || value[nameEnd] == '-') {
			nameEnd++
		}
		if nameEnd == 0 {
			return matrix{}, fmt.Errorf("expected transform function in %q", value)
		}
		name := value[:nameEnd]
		rest := strings.TrimLeftFunc(value[nameEnd:], unicode.IsSpace)
		if !strings.HasPrefix(rest, "(") {
			return matrix{}, fmt.Errorf("transform %s missing opening parenthesis", name)
		}
		closeIndex := strings.IndexByte(rest, ')')
		if closeIndex < 0 {
			return matrix{}, fmt.Errorf("transform %s missing closing parenthesis", name)
		}
		args, err := scanNumbers(rest[1:closeIndex])
		if err != nil {
			return matrix{}, fmt.Errorf("transform %s: %w", name, err)
		}
		local, err := transformFunction(name, args)
		if err != nil {
			return matrix{}, err
		}
		result = multiply(result, local)
		value = strings.TrimSpace(rest[closeIndex+1:])
	}
	return result, nil
}

func transformFunction(name string, args []float64) (matrix, error) {
	switch name {
	case "matrix":
		if len(args) != 6 {
			return matrix{}, errors.New("matrix transform requires six numbers")
		}
		return matrix{a: args[0], b: args[1], c: args[2], d: args[3], e: args[4], f: args[5]}, nil
	case "translate":
		if len(args) != 1 && len(args) != 2 {
			return matrix{}, errors.New("translate transform requires one or two numbers")
		}
		ty := 0.0
		if len(args) == 2 {
			ty = args[1]
		}
		return translateMatrix(args[0], ty), nil
	case "scale":
		if len(args) != 1 && len(args) != 2 {
			return matrix{}, errors.New("scale transform requires one or two numbers")
		}
		sy := args[0]
		if len(args) == 2 {
			sy = args[1]
		}
		return scaleMatrix(args[0], sy), nil
	default:
		return matrix{}, fmt.Errorf("unsupported transform %q", name)
	}
}

func scanNumbers(value string) ([]float64, error) {
	var numbers []float64
	for i := 0; i < len(value); {
		for i < len(value) && separator(value[i]) {
			i++
		}
		if i >= len(value) {
			break
		}
		start := i
		if value[i] == '+' || value[i] == '-' {
			i++
		}
		digits := 0
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
			digits++
		}
		if i < len(value) && value[i] == '.' {
			i++
			for i < len(value) && value[i] >= '0' && value[i] <= '9' {
				i++
				digits++
			}
		}
		if digits == 0 {
			return nil, fmt.Errorf("expected number near %q", value[start:])
		}
		if i < len(value) && (value[i] == 'e' || value[i] == 'E') {
			exponent := i
			i++
			if i < len(value) && (value[i] == '+' || value[i] == '-') {
				i++
			}
			exponentDigits := 0
			for i < len(value) && value[i] >= '0' && value[i] <= '9' {
				i++
				exponentDigits++
			}
			if exponentDigits == 0 {
				i = exponent
			}
		}
		parsed, err := strconv.ParseFloat(value[start:i], 64)
		if err != nil {
			return nil, fmt.Errorf("parse number %q: %w", value[start:i], err)
		}
		numbers = append(numbers, parsed)
	}
	return numbers, nil
}

func separator(b byte) bool {
	return b == ',' || b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
