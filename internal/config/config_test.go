package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Capture.Interface != "eth0" {
		t.Errorf("expected capture interface 'eth0', got %q", cfg.Capture.Interface)
	}
	if cfg.API.Listen != ":8080" {
		t.Errorf("expected API listen ':8080', got %q", cfg.API.Listen)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log level 'info', got %q", cfg.LogLevel)
	}
	if cfg.Store.FlushInterval != 10*time.Second {
		t.Errorf("expected store flush interval 10s, got %v", cfg.Store.FlushInterval)
	}
	if cfg.Store.DatabasePath != "./ybira.db" {
		t.Errorf("expected database path './ybira.db', got %q", cfg.Store.DatabasePath)
	}
	if cfg.Mapper.CacheRefreshInterval != 2*time.Second {
		t.Errorf("expected mapper cache refresh interval 2s, got %v", cfg.Mapper.CacheRefreshInterval)
	}
}

func TestLoadFromYAML(t *testing.T) {
	yamlContent := `
capture:
  interface: "wlan0"
api:
  listen: ":9090"
log_level: "debug"
store:
  flush_interval: 30
  database_path: "/tmp/test.db"
mapper:
  cache_refresh_interval: 5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Capture.Interface != "wlan0" {
		t.Errorf("expected 'wlan0', got %q", cfg.Capture.Interface)
	}
	if cfg.API.Listen != ":9090" {
		t.Errorf("expected ':9090', got %q", cfg.API.Listen)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected 'debug', got %q", cfg.LogLevel)
	}
	if cfg.Store.FlushInterval != 30*time.Second {
		t.Errorf("expected 30s, got %v", cfg.Store.FlushInterval)
	}
	if cfg.Store.DatabasePath != "/tmp/test.db" {
		t.Errorf("expected '/tmp/test.db', got %q", cfg.Store.DatabasePath)
	}
	if cfg.Mapper.CacheRefreshInterval != 5*time.Second {
		t.Errorf("expected 5s, got %v", cfg.Mapper.CacheRefreshInterval)
	}
}

func TestEnvOverridePrecedence(t *testing.T) {
	// Create a YAML file with specific values
	yamlContent := `
capture:
  interface: "wlan0"
api:
  listen: ":9090"
log_level: "debug"
store:
  flush_interval: 30
  database_path: "/tmp/yaml.db"
mapper:
  cache_refresh_interval: 5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Set environment variables that should override YAML values
	t.Setenv("YBIRA_CAPTURE_INTERFACE", "br0")
	t.Setenv("YBIRA_API_LISTEN", ":7070")
	t.Setenv("YBIRA_LOG_LEVEL", "error")
	t.Setenv("YBIRA_STORE_FLUSH_INTERVAL", "60")
	t.Setenv("YBIRA_STORE_DATABASE_PATH", "/var/data/env.db")
	t.Setenv("YBIRA_MAPPER_CACHE_REFRESH_INTERVAL", "10")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// Env should win over YAML
	if cfg.Capture.Interface != "br0" {
		t.Errorf("expected env override 'br0', got %q", cfg.Capture.Interface)
	}
	if cfg.API.Listen != ":7070" {
		t.Errorf("expected env override ':7070', got %q", cfg.API.Listen)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("expected env override 'error', got %q", cfg.LogLevel)
	}
	if cfg.Store.FlushInterval != 60*time.Second {
		t.Errorf("expected env override 60s, got %v", cfg.Store.FlushInterval)
	}
	if cfg.Store.DatabasePath != "/var/data/env.db" {
		t.Errorf("expected env override '/var/data/env.db', got %q", cfg.Store.DatabasePath)
	}
	if cfg.Mapper.CacheRefreshInterval != 10*time.Second {
		t.Errorf("expected env override 10s, got %v", cfg.Mapper.CacheRefreshInterval)
	}
}

func TestMissingFileFallback(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	// Should fall back to all defaults
	defaults := DefaultConfig()
	if cfg.Capture.Interface != defaults.Capture.Interface {
		t.Errorf("expected default interface %q, got %q", defaults.Capture.Interface, cfg.Capture.Interface)
	}
	if cfg.API.Listen != defaults.API.Listen {
		t.Errorf("expected default listen %q, got %q", defaults.API.Listen, cfg.API.Listen)
	}
	if cfg.LogLevel != defaults.LogLevel {
		t.Errorf("expected default log level %q, got %q", defaults.LogLevel, cfg.LogLevel)
	}
	if cfg.Store.FlushInterval != defaults.Store.FlushInterval {
		t.Errorf("expected default flush interval %v, got %v", defaults.Store.FlushInterval, cfg.Store.FlushInterval)
	}
	if cfg.Store.DatabasePath != defaults.Store.DatabasePath {
		t.Errorf("expected default database path %q, got %q", defaults.Store.DatabasePath, cfg.Store.DatabasePath)
	}
	if cfg.Mapper.CacheRefreshInterval != defaults.Mapper.CacheRefreshInterval {
		t.Errorf("expected default cache refresh %v, got %v", defaults.Mapper.CacheRefreshInterval, cfg.Mapper.CacheRefreshInterval)
	}
}

func TestMalformedYAMLFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Write invalid YAML
	if err := os.WriteFile(path, []byte("{{{{not yaml at all!!!!"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// Should fall back to all defaults
	defaults := DefaultConfig()
	if cfg.Capture.Interface != defaults.Capture.Interface {
		t.Errorf("expected default interface %q, got %q", defaults.Capture.Interface, cfg.Capture.Interface)
	}
	if cfg.LogLevel != defaults.LogLevel {
		t.Errorf("expected default log level %q, got %q", defaults.LogLevel, cfg.LogLevel)
	}
}
