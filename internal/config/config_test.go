package config

import (
	"path/filepath"
	"testing"

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
