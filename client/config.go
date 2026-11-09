package client

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultConfigPath places the settings file next to the executable,
// so the whole install is one portable folder (or just the exe in a
// folder of its own).
func DefaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "bitchord-notch.json"
	}
	return filepath.Join(filepath.Dir(exe), "bitchord-notch.json")
}

func LoadConfig(path string) Config {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
