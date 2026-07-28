package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultPenUp   = 0.50
	DefaultPenDown = 1.70
	DefaultStartX  = 0.0
	DefaultStartY  = 0.0
)

type Config struct {
	PenUp   float64 `json:"pen_up"`
	PenDown float64 `json:"pen_down"`
	StartX  float64 `json:"start_x"`
	StartY  float64 `json:"start_y"`
}

func Default() Config {
	return Config{
		PenUp:   DefaultPenUp,
		PenDown: DefaultPenDown,
		StartX:  DefaultStartX,
		StartY:  DefaultStartY,
	}
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "writerrobot", "config.json"), nil
}

func Load() (Config, string, error) {
	path, err := Path()
	if err != nil {
		return Config{}, "", fmt.Errorf("find config directory: %w", err)
	}
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, path, nil
	}
	if err != nil {
		return Config{}, path, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, path, fmt.Errorf("parse config: %w", err)
	}
	return cfg, path, nil
}

func Save(cfg Config) (string, error) {
	path, err := Path()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return path, fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return path, fmt.Errorf("write config: %w", err)
	}
	return path, nil
}
