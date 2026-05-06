package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type CaptureConfig struct {
	Interface string `yaml:"interface"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
}

type StoreConfig struct {
	FlushInterval time.Duration `yaml:"flush_interval"`
	DatabasePath  string        `yaml:"database_path"`
}

type MapperConfig struct {
	CacheRefreshInterval time.Duration `yaml:"cache_refresh_interval"`
}

type Config struct {
	Capture  CaptureConfig `yaml:"capture"`
	API      APIConfig     `yaml:"api"`
	Store    StoreConfig   `yaml:"store"`
	Mapper   MapperConfig  `yaml:"mapper"`
	LogLevel string        `yaml:"log_level"`
}

func DefaultConfig() Config {
	return Config{
		Capture: CaptureConfig{
			Interface: "eth0",
		},
		API: APIConfig{
			Listen: ":8080",
		},
		Store: StoreConfig{
			FlushInterval: 10 * time.Second,
			DatabasePath:  "./ybira.db",
		},
		Mapper: MapperConfig{
			CacheRefreshInterval: 2 * time.Second,
		},
		LogLevel: "info",
	}
}

type yamlConfig struct {
	Capture struct {
		Interface string `yaml:"interface"`
	} `yaml:"capture"`
	API struct {
		Listen string `yaml:"listen"`
	} `yaml:"api"`
	Store struct {
		FlushInterval *int   `yaml:"flush_interval"`
		DatabasePath  string `yaml:"database_path"`
	} `yaml:"store"`
	Mapper struct {
		CacheRefreshInterval *int `yaml:"cache_refresh_interval"`
	} `yaml:"mapper"`
	LogLevel string `yaml:"log_level"`
}

func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if err := loadFromFile(&cfg, path); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: config file: %v, using defaults\n", err)
	}

	applyEnvOverrides(&cfg)

	return cfg, nil
}

func loadFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}

	var yc yamlConfig
	if err := yaml.Unmarshal(data, &yc); err != nil {
		return fmt.Errorf("cannot parse %s: %w", path, err)
	}

	if yc.Capture.Interface != "" {
		cfg.Capture.Interface = yc.Capture.Interface
	}
	if yc.API.Listen != "" {
		cfg.API.Listen = yc.API.Listen
	}
	if yc.Store.FlushInterval != nil {
		cfg.Store.FlushInterval = time.Duration(*yc.Store.FlushInterval) * time.Second
	}
	if yc.Store.DatabasePath != "" {
		cfg.Store.DatabasePath = yc.Store.DatabasePath
	}
	if yc.Mapper.CacheRefreshInterval != nil {
		cfg.Mapper.CacheRefreshInterval = time.Duration(*yc.Mapper.CacheRefreshInterval) * time.Second
	}
	if yc.LogLevel != "" {
		cfg.LogLevel = yc.LogLevel
	}

	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("YBIRA_CAPTURE_INTERFACE"); v != "" {
		cfg.Capture.Interface = v
	}
	if v := os.Getenv("YBIRA_API_LISTEN"); v != "" {
		cfg.API.Listen = v
	}
	if v := os.Getenv("YBIRA_STORE_FLUSH_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			cfg.Store.FlushInterval = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("YBIRA_STORE_DATABASE_PATH"); v != "" {
		cfg.Store.DatabasePath = v
	}
	if v := os.Getenv("YBIRA_MAPPER_CACHE_REFRESH_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			cfg.Mapper.CacheRefreshInterval = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("YBIRA_LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
	}
}
