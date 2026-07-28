package gcode

import (
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	input := `
; heading
G21
G90 (absolute positioning)
G0 X10 Y-20 ; move

`
	got, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"G21", "G90", "G0 X10 Y-20"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Command != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, got[i].Command, want[i])
		}
	}
}
