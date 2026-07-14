package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDefaultYAMLConfig_ReturnsValidDefaults(t *testing.T) {
	cfg := GetDefaultYAMLConfig()
	require.NotNil(t, cfg)

	assert.Equal(t, 9002, cfg.Server.Port)
	assert.Equal(t, "./data/llm-proxy.db", cfg.Storage.SQLitePath)
	assert.True(t, cfg.Admin.Enabled)
	assert.Equal(t, 30, cfg.Upstream.ProbeIntervalSeconds)
	assert.Equal(t, 5, cfg.Upstream.ProbeTimeoutSeconds)
	assert.True(t, cfg.Audit.Enabled)
	assert.Equal(t, 100, cfg.Audit.BatchSize)
	assert.Equal(t, 1000, cfg.Audit.FlushInterval)
	assert.Equal(t, 10000, cfg.Audit.ChannelBuffer)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "text", cfg.Logging.Format)
}

func TestValidate_FillsInDefaults_ForZeroValues(t *testing.T) {
	cfg := &YAMLConfig{} // all zero values

	err := cfg.Validate()
	require.NoError(t, err)

	assert.Equal(t, 9002, cfg.Server.Port)
	assert.Equal(t, "./data/llm-proxy.db", cfg.Storage.SQLitePath)
	assert.Equal(t, 30, cfg.Upstream.ProbeIntervalSeconds)
	assert.Equal(t, 5, cfg.Upstream.ProbeTimeoutSeconds)
	assert.Equal(t, 100, cfg.Audit.BatchSize)
	assert.Equal(t, 1000, cfg.Audit.FlushInterval)
	assert.Equal(t, 10000, cfg.Audit.ChannelBuffer)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "text", cfg.Logging.Format)
}

func TestValidate_PreservesNonZeroValues(t *testing.T) {
	cfg := &YAMLConfig{
		Server:  ServerConfig{Port: 8080},
		Storage: StorageConfig{SQLitePath: "/custom/path.db"},
		Upstream: UpstreamConfig{
			ProbeIntervalSeconds: 60,
			ProbeTimeoutSeconds:  10,
		},
		Audit: AuditConfig{
			BatchSize:     50,
			FlushInterval: 500,
			ChannelBuffer: 5000,
		},
		Logging: LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "/custom/path.db", cfg.Storage.SQLitePath)
	assert.Equal(t, 60, cfg.Upstream.ProbeIntervalSeconds)
	assert.Equal(t, 10, cfg.Upstream.ProbeTimeoutSeconds)
	assert.Equal(t, 50, cfg.Audit.BatchSize)
	assert.Equal(t, 500, cfg.Audit.FlushInterval)
	assert.Equal(t, 5000, cfg.Audit.ChannelBuffer)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestLoadYAMLConfig_NonExistentFile_ReturnsDefaults(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.yml")

	cfg, err := LoadYAMLConfig(nonExistent)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Should be equal to defaults
	defaults := GetDefaultYAMLConfig()
	assert.Equal(t, defaults.Server.Port, cfg.Server.Port)
	assert.Equal(t, defaults.Storage.SQLitePath, cfg.Storage.SQLitePath)
	assert.Equal(t, defaults.Upstream.ProbeIntervalSeconds, cfg.Upstream.ProbeIntervalSeconds)
	assert.Equal(t, defaults.Audit.BatchSize, cfg.Audit.BatchSize)
	assert.Equal(t, defaults.Logging.Level, cfg.Logging.Level)
	assert.Equal(t, defaults.Logging.Format, cfg.Logging.Format)
}

func TestLoadYAMLConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.yml")
	content := []byte(`
server:
  port: 8080
storage:
  sqlite_path: "/tmp/test.db"
logging:
  level: "debug"
  format: "json"
`)
	require.NoError(t, os.WriteFile(cfgFile, content, 0644))

	cfg, err := LoadYAMLConfig(cfgFile)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "/tmp/test.db", cfg.Storage.SQLitePath)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	// Defaults should fill in for unspecified fields
	assert.Equal(t, 30, cfg.Upstream.ProbeIntervalSeconds)
	assert.Equal(t, 100, cfg.Audit.BatchSize)
}

func TestLoadYAMLConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad.yml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(":::not valid yaml[[["), 0644))

	_, err := LoadYAMLConfig(cfgFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse YAML")
}

// ---------------------------------------------------------------------------
// DefaultTransportConfig
// ---------------------------------------------------------------------------

func TestDefaultTransportConfig_Values(t *testing.T) {
	tc := DefaultTransportConfig()
	require.NotNil(t, tc)

	assert.Equal(t, 30*time.Second, tc.DialTimeout)
	assert.Equal(t, 30*time.Second, tc.KeepAlive)
	assert.Equal(t, 90*time.Second, tc.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, tc.TLSHandshakeTimeout)
	assert.Equal(t, 200, tc.MaxIdleConns)
	assert.Equal(t, 64, tc.MaxIdleConnsPerHost)
}

func TestGetDefaultYAMLConfig_IncludesTransportDefaults(t *testing.T) {
	cfg := GetDefaultYAMLConfig()
	tc := DefaultTransportConfig()

	assert.Equal(t, tc.DialTimeout, cfg.Transport.DialTimeout)
	assert.Equal(t, tc.KeepAlive, cfg.Transport.KeepAlive)
	assert.Equal(t, tc.IdleConnTimeout, cfg.Transport.IdleConnTimeout)
	assert.Equal(t, tc.TLSHandshakeTimeout, cfg.Transport.TLSHandshakeTimeout)
	assert.Equal(t, tc.MaxIdleConns, cfg.Transport.MaxIdleConns)
	assert.Equal(t, tc.MaxIdleConnsPerHost, cfg.Transport.MaxIdleConnsPerHost)
}

// ---------------------------------------------------------------------------
// Validate — Transport defaults
// ---------------------------------------------------------------------------

func TestValidate_FillsTransportDefaults(t *testing.T) {
	cfg := &YAMLConfig{}
	require.NoError(t, cfg.Validate())

	assert.Equal(t, 30*time.Second, cfg.Transport.DialTimeout)
	assert.Equal(t, 30*time.Second, cfg.Transport.KeepAlive)
	assert.Equal(t, 90*time.Second, cfg.Transport.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, cfg.Transport.TLSHandshakeTimeout)
	assert.Equal(t, 200, cfg.Transport.MaxIdleConns)
	assert.Equal(t, 64, cfg.Transport.MaxIdleConnsPerHost)
}

func TestValidate_PreservesCustomTransport(t *testing.T) {
	cfg := &YAMLConfig{
		Transport: TransportConfig{
			DialTimeout:         5 * time.Second,
			KeepAlive:           10 * time.Second,
			IdleConnTimeout:     20 * time.Second,
			TLSHandshakeTimeout: 3 * time.Second,
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
		},
	}
	require.NoError(t, cfg.Validate())

	assert.Equal(t, 5*time.Second, cfg.Transport.DialTimeout)
	assert.Equal(t, 10*time.Second, cfg.Transport.KeepAlive)
	assert.Equal(t, 20*time.Second, cfg.Transport.IdleConnTimeout)
	assert.Equal(t, 3*time.Second, cfg.Transport.TLSHandshakeTimeout)
	assert.Equal(t, 50, cfg.Transport.MaxIdleConns)
	assert.Equal(t, 10, cfg.Transport.MaxIdleConnsPerHost)
}

func TestValidate_FillsAutoDisableThreshold(t *testing.T) {
	cfg := &YAMLConfig{}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 2, cfg.Upstream.AutoDisableThreshold)
}

// ---------------------------------------------------------------------------
// LoadEnvironmentConfig
// ---------------------------------------------------------------------------

func TestLoadEnvironmentConfig_BaseOnly(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))

	baseContent := []byte(`
server:
  port: 3000
logging:
  level: "warn"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yml"), baseContent, 0644))

	// Change to temp dir so "configs" resolves correctly
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	// Set ENVIRONMENT to something without an overlay file
	t.Setenv("ENVIRONMENT", "test-no-overlay")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)
	assert.Equal(t, 3000, cfg.Server.Port)
	assert.Equal(t, "warn", cfg.Logging.Level)
}

func TestLoadEnvironmentConfig_WithOverlay(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))

	baseContent := []byte(`
server:
  port: 9002
logging:
  level: "info"
  format: "text"
audit:
  batch_size: 100
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yml"), baseContent, 0644))

	overlayContent := []byte(`
logging:
  level: "debug"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "myenv.yml"), overlayContent, 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("ENVIRONMENT", "myenv")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)
	assert.Equal(t, 9002, cfg.Server.Port, "base value should be preserved")
	assert.Equal(t, "debug", cfg.Logging.Level, "overlay should override logging level")
	assert.Equal(t, 100, cfg.Audit.BatchSize, "unmentioned base values should be preserved")
}

func TestLoadEnvironmentConfig_DefaultsToDevEnv(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))

	baseContent := []byte(`
server:
  port: 9002
logging:
  level: "info"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yml"), baseContent, 0644))

	devContent := []byte(`
logging:
  level: "debug"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "dev.yml"), devContent, 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	// Unset ENVIRONMENT to trigger default "dev"
	t.Setenv("ENVIRONMENT", "")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Logging.Level, "should default to dev environment")
}

func TestLoadEnvironmentConfig_NoBaseFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))
	// No base.yml

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("ENVIRONMENT", "nonexistent")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)
	defaults := GetDefaultYAMLConfig()
	assert.Equal(t, defaults.Server.Port, cfg.Server.Port)
	assert.Equal(t, defaults.Logging.Level, cfg.Logging.Level)
}

// ===========================================================================
// EDGE-CASE / COVERAGE TESTS
// ===========================================================================

// ---------------------------------------------------------------------------
// LogConfiguration (0% coverage)
// ---------------------------------------------------------------------------

func TestLogConfiguration_DoesNotPanic(t *testing.T) {
	cfg := GetDefaultYAMLConfig()
	logger := slog.Default()

	// Should not panic on valid config
	assert.NotPanics(t, func() {
		cfg.LogConfiguration(logger)
	})
}

func TestLogConfiguration_CustomConfig(t *testing.T) {
	cfg := &YAMLConfig{
		Server:  ServerConfig{Port: 8080},
		Storage: StorageConfig{SQLitePath: "/custom/path.db"},
		Admin:   AdminConfig{Enabled: false},
		Upstream: UpstreamConfig{
			ProbeIntervalSeconds: 60,
		},
		Audit: AuditConfig{
			Enabled: false,
		},
		Logging: LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
	}
	logger := slog.Default()

	assert.NotPanics(t, func() {
		cfg.LogConfiguration(logger)
	})
}

// ---------------------------------------------------------------------------
// LoadEnvironmentConfig — env var overrides (tested at main level,
// but we verify the config paths that LoadEnvironmentConfig takes)
// ---------------------------------------------------------------------------

func TestLoadEnvironmentConfig_WithPORTEnvOverride(t *testing.T) {
	// PORT override is applied by main, not LoadEnvironmentConfig, but we can
	// verify that the base config port is correctly loaded and can be overridden
	// after the call.
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))

	baseContent := []byte(`
server:
  port: 9002
logging:
  level: "info"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yml"), baseContent, 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("ENVIRONMENT", "nonexistent-env")
	t.Setenv("PORT", "3333")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)
	// LoadEnvironmentConfig returns the YAML value; PORT override would be applied by main
	assert.Equal(t, 9002, cfg.Server.Port)
}

func TestLoadEnvironmentConfig_OverlayOnlyOverridesSpecifiedFields(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))

	baseContent := []byte(`
server:
  port: 9002
storage:
  sqlite_path: "/data/base.db"
logging:
  level: "info"
  format: "text"
audit:
  enabled: true
  batch_size: 100
  flush_interval_ms: 1000
  channel_buffer: 10000
upstream:
  probe_interval_seconds: 30
  probe_timeout_seconds: 5
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yml"), baseContent, 0644))

	// Overlay only changes logging level
	overlayContent := []byte(`
logging:
  level: "error"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "overlay.yml"), overlayContent, 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("ENVIRONMENT", "overlay")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)

	// Overridden
	assert.Equal(t, "error", cfg.Logging.Level)
	// Preserved from base
	assert.Equal(t, 9002, cfg.Server.Port)
	assert.Equal(t, "/data/base.db", cfg.Storage.SQLitePath)
	assert.Equal(t, "text", cfg.Logging.Format)
	assert.Equal(t, 100, cfg.Audit.BatchSize)
	assert.Equal(t, 30, cfg.Upstream.ProbeIntervalSeconds)
}

func TestLoadEnvironmentConfig_OverlayWithMultipleFields(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	require.NoError(t, os.Mkdir(configDir, 0755))

	baseContent := []byte(`
server:
  port: 9002
logging:
  level: "info"
  format: "text"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yml"), baseContent, 0644))

	overlayContent := []byte(`
server:
  port: 80
logging:
  level: "warn"
  format: "json"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "prod.yml"), overlayContent, 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { os.Chdir(origDir) })

	t.Setenv("ENVIRONMENT", "prod")

	cfg, err := LoadEnvironmentConfig()
	require.NoError(t, err)
	assert.Equal(t, 80, cfg.Server.Port)
	assert.Equal(t, "warn", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestValidate_ZeroAutoDisableThresholdGetsDefault(t *testing.T) {
	cfg := &YAMLConfig{
		Upstream: UpstreamConfig{AutoDisableThreshold: 0},
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 2, cfg.Upstream.AutoDisableThreshold, "zero should be filled with default 2")
}

func TestValidate_NegativeAutoDisableThresholdGetsDefault(t *testing.T) {
	cfg := &YAMLConfig{
		Upstream: UpstreamConfig{AutoDisableThreshold: -1},
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 2, cfg.Upstream.AutoDisableThreshold, "negative should be filled with default 2")
}
