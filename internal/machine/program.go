package machine

import (
	"fmt"

	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/plot"
)

// ProgramFromPlan converts planned plotting operations into absolute
// program-coordinate G-code.
func ProgramFromPlan(ops []plot.Operation) ([]gcode.Line, error) {
	commands := make([]string, 0, len(ops))
	for i, op := range ops {
		command, err := commandForOperation(op)
		if err != nil {
			return nil, fmt.Errorf("operation %d: %w", i+1, err)
		}
		commands = append(commands, command)
	}

	lines := make([]gcode.Line, 0, len(commands))
	for i, command := range commands {
		lines = append(lines, gcode.Line{Number: i + 1, Command: command})
	}
	return lines, nil
}

func commandForOperation(op plot.Operation) (string, error) {
	switch op.Kind {
	case plot.OperationPenUp, plot.OperationPenDown:
		return fmt.Sprintf("G1 Z%.3f F%s", op.Z, formatFeed(op.Feed)), nil
	case plot.OperationRapidMove:
		return fmt.Sprintf("G0 X%.3f Y%.3f", op.Point.X, op.Point.Y), nil
	case plot.OperationDrawMove:
		if op.Feed > 0 {
			return fmt.Sprintf("G1 X%.3f Y%.3f F%s", op.Point.X, op.Point.Y, formatFeed(op.Feed)), nil
		}
		return fmt.Sprintf("G1 X%.3f Y%.3f", op.Point.X, op.Point.Y), nil
	default:
		return "", fmt.Errorf("unsupported plot operation kind %d", op.Kind)
	}
}
