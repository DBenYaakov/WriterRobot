package main

import (
	"strings"
	"testing"
)

func TestReadKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  key
	}{
		{"enter", "\r", keyEnter},
		{"ctrl-c", string([]byte{3}), keyCancel},
		{"up", "\x1b[A", keyUp},
		{"down", "\x1b[B", keyDown},
		{"right", "\x1b[C", keyRight},
		{"left", "\x1b[D", keyLeft},
		{"pen up lower", "u", keyPenUp},
		{"pen up upper", "U", keyPenUp},
		{"pen down lower", "d", keyPenDown},
		{"pen down upper", "D", keyPenDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readKey(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("readKey: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
