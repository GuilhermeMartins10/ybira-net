package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestPropertyEnvAlwaysOverridesYAML verifies Property 6 from the design:
// For any configuration key K with YAML value Y and environment variable
// YBIRA_K with value E, the loaded config value for K equals E
// (environment always wins).
//
// **Validates: Requirements 8.2**
func TestPropertyEnvAlwaysOverridesYAML(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random YAML values
		yamlInterface := rapid.StringMatching(`[a-z]{2,8}[0-9]`).Draw(rt, "yamlInterface")
		yamlListen := rapid.StringMatching(`:[0-9]{4,5}`).Draw(rt, "yamlListen")
		yamlLogLevel := rapid.SampledFrom([]string{"debug", "info", "warn", "error"}).Draw(rt, "yamlLogLevel")
		yamlFlushInterval := rapid.IntRange(1, 300).Draw(rt, "yamlFlushInterval")
		yamlDBPath := rapid.StringMatching(`/tmp/[a-z]{3,10}\.db`).Draw(rt, "yamlDBPath")
		yamlCacheRefresh := rapid.IntRange(1, 60).Draw(rt, "yamlCacheRefresh")

		// Generate random env values (different from YAML)
		envInterface := rapid.StringMatching(`[a-z]{2,8}[0-9]`).Draw(rt, "envInterface")
		envListen := rapid.StringMatching(`:[0-9]{4,5}`).Draw(rt, "envListen")
		envLogLevel := rapid.SampledFrom([]string{"debug", "info", "warn", "error"}).Draw(rt, "envLogLevel")
		envFlushInterval := rapid.IntRange(1, 300).Draw(rt, "envFlushInterval")
		envDBPath := rapid.StringMatching(`/tmp/[a-z]{3,10}\.db`).Draw(rt, "envDBPath")
		envCacheRefresh := rapid.IntRange(1, 60).Draw(rt, "envCacheRefresh")

		// Write YAML config file
		yamlContent := fmt.Sprintf(`capture:
  interface: %q
api:
  listen: %q
log_level: %q
store:
  flush_interval: %d
  database_path: %q
mapper:
  cache_refresh_interval: %d
`, yamlInterface, yamlListen, yamlLogLevel, yamlFlushInterval, yamlDBPath, yamlCacheRefresh)

		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
			rt.Fatal(err)
		}

		// Set environment variables (restore after test via cleanup)
		envVars := map[string]string{
			"YBIRA_CAPTURE_INTERFACE":            envInterface,
			"YBIRA_API_LISTEN":                   envListen,
			"YBIRA_LOG_LEVEL":                    envLogLevel,
			"YBIRA_STORE_FLUSH_INTERVAL":         strconv.Itoa(envFlushInterval),
			"YBIRA_STORE_DATABASE_PATH":          envDBPath,
			"YBIRA_MAPPER_CACHE_REFRESH_INTERVAL": strconv.Itoa(envCacheRefresh),
		}
		for k, v := range envVars {
			os.Setenv(k, v)
		}
		defer func() {
			for k := range envVars {
				os.Unsetenv(k)
			}
		}()

		// Load config
		cfg, err := Load(path)
		if err != nil {
			rt.Fatal(err)
		}

		// Property: environment always wins over YAML
		if cfg.Capture.Interface != envInterface {
			rt.Errorf("Capture.Interface: env %q should win over yaml %q, got %q",
				envInterface, yamlInterface, cfg.Capture.Interface)
		}
		if cfg.API.Listen != envListen {
			rt.Errorf("API.Listen: env %q should win over yaml %q, got %q",
				envListen, yamlListen, cfg.API.Listen)
		}
		if cfg.LogLevel != envLogLevel {
			rt.Errorf("LogLevel: env %q should win over yaml %q, got %q",
				envLogLevel, yamlLogLevel, cfg.LogLevel)
		}
		expectedFlush := time.Duration(envFlushInterval) * time.Second
		if cfg.Store.FlushInterval != expectedFlush {
			rt.Errorf("Store.FlushInterval: env %v should win over yaml %v, got %v",
				expectedFlush, time.Duration(yamlFlushInterval)*time.Second, cfg.Store.FlushInterval)
		}
		if cfg.Store.DatabasePath != envDBPath {
			rt.Errorf("Store.DatabasePath: env %q should win over yaml %q, got %q",
				envDBPath, yamlDBPath, cfg.Store.DatabasePath)
		}
		expectedCacheRefresh := time.Duration(envCacheRefresh) * time.Second
		if cfg.Mapper.CacheRefreshInterval != expectedCacheRefresh {
			rt.Errorf("Mapper.CacheRefreshInterval: env %v should win over yaml %v, got %v",
				expectedCacheRefresh, time.Duration(yamlCacheRefresh)*time.Second, cfg.Mapper.CacheRefreshInterval)
		}
	})
}
