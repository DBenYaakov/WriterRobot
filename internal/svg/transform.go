package svg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

func translateTransform(tx, ty float64) drawing.Transform {
	return drawing.Transform{A: 1, D: 1, E: tx, F: ty}
}

func scaleTransform(sx, sy float64) drawing.Transform {
	return drawing.Transform{A: sx, D: sy}
}

func parseTransform(value string) (drawing.Transform, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return drawing.IdentityTransform(), nil
	}
	result := drawing.IdentityTransform()
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
			return drawing.Transform{}, fmt.Errorf("expected transform function in %q", value)
		}
		name := value[:nameEnd]
		rest := strings.TrimLeftFunc(value[nameEnd:], unicode.IsSpace)
		if !strings.HasPrefix(rest, "(") {
			return drawing.Transform{}, fmt.Errorf("transform %s missing opening parenthesis", name)
		}
		closeIndex := strings.IndexByte(rest, ')')
		if closeIndex < 0 {
			return drawing.Transform{}, fmt.Errorf("transform %s missing closing parenthesis", name)
		}
		args, err := scanNumbers(rest[1:closeIndex])
		if err != nil {
			return drawing.Transform{}, fmt.Errorf("transform %s: %w", name, err)
		}
		local, err := transformFunction(name, args)
		if err != nil {
			return drawing.Transform{}, err
		}
		result = result.Then(local)
		value = strings.TrimSpace(rest[closeIndex+1:])
	}
	return result, nil
}

func transformFunction(name string, args []float64) (drawing.Transform, error) {
	switch name {
	case "matrix":
		if len(args) != 6 {
			return drawing.Transform{}, errors.New("matrix transform requires six numbers")
		}
		return drawing.Transform{A: args[0], B: args[1], C: args[2], D: args[3], E: args[4], F: args[5]}, nil
	case "translate":
		if len(args) != 1 && len(args) != 2 {
			return drawing.Transform{}, errors.New("translate transform requires one or two numbers")
		}
		ty := 0.0
		if len(args) == 2 {
			ty = args[1]
		}
		return translateTransform(args[0], ty), nil
	case "scale":
		if len(args) != 1 && len(args) != 2 {
			return drawing.Transform{}, errors.New("scale transform requires one or two numbers")
		}
		sy := args[0]
		if len(args) == 2 {
			sy = args[1]
		}
		return scaleTransform(args[0], sy), nil
	default:
		return drawing.Transform{}, fmt.Errorf("unsupported transform %q", name)
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
