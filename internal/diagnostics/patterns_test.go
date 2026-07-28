package diagnostics

import (
	"strconv"
	"strings"
	"testing"

	"github.com/DBenYaakov/WriterRobot/internal/gcode"
)

func TestBuiltInPatternsGenerateSafeMotion(t *testing.T) {
	patterns := []Pattern{
		CirclePattern{},
		SquarePattern{},
		TrianglePattern{},
		SinePattern{},
		GridPattern{},
		CrosshairPattern{},
	}
	opts := DefaultOptions(0.5, 1.7)

	for _, pattern := range patterns {
		t.Run(pattern.Name(), func(t *testing.T) {
			lines, err := pattern.Generate(opts)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if len(lines) == 0 {
				t.Fatal("pattern generated no lines")
			}
			if lines[0].Command != "G1 Z0.500 F300" {
				t.Fatalf("first command = %q, want pen raised", lines[0].Command)
			}
			if lines[len(lines)-1].Command != "G1 Z0.500 F300" {
				t.Fatalf("last command = %q, want pen raised", lines[len(lines)-1].Command)
			}
			if lines[len(lines)-2].Command != "G0 X0.000 Y0.000" {
				t.Fatalf("next-to-last command = %q, want return to origin", lines[len(lines)-2].Command)
			}
			verifySafeMotion(t, lines, opts)
		})
	}
}

func verifySafeMotion(t *testing.T, lines []gcode.Line, opts Options) {
	t.Helper()

	penDown := false
	drewSinceLower := false
	lowerCount := 0
	drawCount := 0

	for i, line := range lines {
		if line.Number != i+1 {
			t.Fatalf("line %d has source number %d", i+1, line.Number)
		}
		command := line.Command
		switch {
		case strings.HasPrefix(command, "G0 "):
			if penDown {
				t.Fatalf("line %d rapid move while pen is down: %q", i+1, command)
			}
			assertSafeXY(t, command)
		case strings.HasPrefix(command, "G1 Z"):
			z := parseAxis(t, command, "Z")
			switch {
			case almostEqual(z, opts.PenUpZ):
				if penDown && !drewSinceLower {
					t.Fatalf("line %d raised pen without drawing: %q", i+1, command)
				}
				penDown = false
				drewSinceLower = false
			case almostEqual(z, opts.PenDownZ):
				if penDown {
					t.Fatalf("line %d lowered pen while already down: %q", i+1, command)
				}
				penDown = true
				drewSinceLower = false
				lowerCount++
			default:
				t.Fatalf("line %d has unexpected Z command: %q", i+1, command)
			}
		case strings.HasPrefix(command, "G1 X"):
			if !penDown {
				t.Fatalf("line %d draw move while pen is up: %q", i+1, command)
			}
			assertSafeXY(t, command)
			drewSinceLower = true
			drawCount++
		default:
			t.Fatalf("line %d has unexpected command: %q", i+1, command)
		}
	}

	if penDown {
		t.Fatal("pattern ended with pen down")
	}
	if lowerCount == 0 || drawCount == 0 {
		t.Fatalf("pattern did not lower and draw: lowers=%d draws=%d", lowerCount, drawCount)
	}
}

func parseAxis(t *testing.T, command, axis string) float64 {
	t.Helper()
	prefix := axis
	for _, field := range strings.Fields(command) {
		if strings.HasPrefix(field, prefix) {
			value, err := strconv.ParseFloat(strings.TrimPrefix(field, prefix), 64)
			if err != nil {
				t.Fatalf("parse %s in %q: %v", axis, command, err)
			}
			return value
		}
	}
	t.Fatalf("axis %s not found in %q", axis, command)
	return 0
}

func assertSafeXY(t *testing.T, command string) {
	t.Helper()
	x := parseAxis(t, command, "X")
	y := parseAxis(t, command, "Y")
	if x < -0.001 || x > 100 || y > 0.001 || y < -100 {
		t.Fatalf("command has invalid diagnostic coordinate: %q", command)
	}
}

func almostEqual(a, b float64) bool {
	if a > b {
		return a-b < 0.001
	}
	return b-a < 0.001
}
