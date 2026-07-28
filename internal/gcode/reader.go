package gcode

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Line is a normalized G-code command and its original source line number.
type Line struct {
	Number  int
	Command string
}

// Read parses G-code from r, removing blank lines and comments.
func Read(r io.Reader) ([]Line, error) {
	scanner := bufio.NewScanner(r)
	// Allow unusually long generated G-code lines while still bounding memory use.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var lines []Line
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		command := normalize(scanner.Text())
		if command == "" {
			continue
		}
		lines = append(lines, Line{Number: lineNumber, Command: command})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read G-code: %w", err)
	}
	return lines, nil
}

func normalize(s string) string {
	var b strings.Builder
	inParen := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			inParen = true
		case ')':
			inParen = false
		case ';':
			if !inParen {
				return strings.TrimSpace(b.String())
			}
		default:
			if !inParen {
				b.WriteByte(s[i])
			}
		}
	}
	return strings.TrimSpace(b.String())
}
