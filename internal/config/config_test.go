package config

import (
	"encoding/json"
	"testing"
)

func TestDefaultReturnsHomeOnCompletion(t *testing.T) {
	cfg := Default()

	if !cfg.ReturnHomeOnCompletion {
		t.Fatal("ReturnHomeOnCompletion = false, want true")
	}
	if cfg.MachineHomeX != DefaultMachineHomeX || cfg.MachineHomeY != DefaultMachineHomeY {
		t.Fatalf("machine home = X%.3f Y%.3f, want default X%.3f Y%.3f", cfg.MachineHomeX, cfg.MachineHomeY, DefaultMachineHomeX, DefaultMachineHomeY)
	}
}

func TestMissingReturnHomeFieldKeepsDefault(t *testing.T) {
	cfg := Default()
	if err := json.Unmarshal([]byte(`{"pen_up":0.5,"pen_down":1.7,"start_x":10,"start_y":-20}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !cfg.ReturnHomeOnCompletion {
		t.Fatal("ReturnHomeOnCompletion = false for old config, want default true")
	}
	if cfg.MachineHomeX != DefaultMachineHomeX || cfg.MachineHomeY != DefaultMachineHomeY {
		t.Fatalf("machine home = X%.3f Y%.3f, want default X%.3f Y%.3f", cfg.MachineHomeX, cfg.MachineHomeY, DefaultMachineHomeX, DefaultMachineHomeY)
	}
}

func TestReturnHomeCanBeDisabledByConfig(t *testing.T) {
	cfg := Default()
	if err := json.Unmarshal([]byte(`{"return_home_on_completion":false}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if cfg.ReturnHomeOnCompletion {
		t.Fatal("ReturnHomeOnCompletion = true, want explicit false")
	}
}
