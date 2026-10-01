package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is loaded from data/config.json (relative to the working directory).
// The file is auto-created with defaults on first start.
// db_driver: "mysql"（推荐）或 "sqlite"；db_dsn 按 driver 给出连接串。
// 登录密码不在本文件——存于数据库 settings 表（setting_key = auth_password）。
type Config struct {
	Port     int    `json:"port"`
	DbDriver string `json:"db_driver"`
	DbDsn    string `json:"db_dsn"`
}

// DataDir returns ./data relative to the current working directory.
// sqlite 模式的库文件目录（mysql 模式不使用）。
func DataDir() string {
	return filepath.Join(".", "data")
}

// ConfigPath returns ./config.json relative to the current working directory.
func ConfigPath() string {
	return filepath.Join(".", "config.json")
}

// Load reads (or creates) the config file and returns it.
func Load() (*Config, error) {
	path := ConfigPath()

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// first start: write defaults (sqlite mode; 登录密码存于 settings 表，默认 kaytodo)
		cfg := &Config{Port: 8080, DbDriver: "sqlite", DbDsn: ""}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		fmt.Println("[config] config.json created. 默认 sqlite 模式，密码 kaytodo（存于 settings 表）")
		return cfg, nil
	}

	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Port <= 0 {
		cfg.Port = 8080
	}
	if cfg.DbDriver == "" {
		cfg.DbDriver = "sqlite"
	}
	if cfg.DbDriver != "mysql" && cfg.DbDriver != "sqlite" {
		return nil, fmt.Errorf("config.db_driver 无效（mysql/sqlite）")
	}
	if cfg.DbDriver == "mysql" && cfg.DbDsn == "" {
		return nil, fmt.Errorf("config.db_dsn 不能为空（mysql 模式）")
	}
	return cfg, nil
}
