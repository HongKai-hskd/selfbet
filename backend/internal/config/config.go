package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is loaded from data/config.json (relative to the working directory).
// The file is auto-created with defaults on first start.
type Config struct {
	Port     int    `json:"port"`
	Password string `json:"password"`
}

// DataDir returns ./data relative to the current working directory.
func DataDir() string {
	return filepath.Join(".", "data")
}

// Load reads (or creates) the config file and returns it.
func Load() (*Config, error) {
	dir := DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dir, "config.json")

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// first start: write defaults
		cfg := &Config{Port: 8080, Password: "selfbet123"}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		fmt.Println("[config] data/config.json created. 默认密码 selfbet123，请尽快修改该文件并重启")
		return cfg, nil
	}

	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Port <= 0 {
		cfg.Port = 8080
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("config.password 不能为空")
	}
	return cfg, nil
}
