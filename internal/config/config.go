package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// YAMLConfig represents the new simplified configuration.
type YAMLConfig struct {
	Server   ServerConfig   `yaml:"server"`
	Storage  StorageConfig  `yaml:"storage"`
	Admin    AdminConfig    `yaml:"admin"`
	Upstream UpstreamConfig `yaml:"upstream"`
	Audit    AuditConfig    `yaml:"audit"`
	Logging  LoggingConfig  `yaml:"logging"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

type AdminConfig struct {
	Enabled bool `yaml:"enabled"`
}

type UpstreamConfig struct {
	ProbeIntervalSeconds int `yaml:"probe_interval_seconds"`
	ProbeTimeoutSeconds  int `yaml:"probe_timeout_seconds"`
}

type AuditConfig struct {
	Enabled       bool `yaml:"enabled"`
	BatchSize     int  `yaml:"batch_size"`
	FlushInterval int  `yaml:"flush_interval_ms"`
	ChannelBuffer int  `yaml:"channel_buffer"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Validate validates the configuration and fills in defaults.
func (c *YAMLConfig) Validate() error {
	if c.Server.Port <= 0 {
		c.Server.Port = 9002
	}
	if c.Storage.SQLitePath == "" {
		c.Storage.SQLitePath = "./data/llm-proxy.db"
	}
	if c.Upstream.ProbeIntervalSeconds <= 0 {
		c.Upstream.ProbeIntervalSeconds = 30
	}
	if c.Upstream.ProbeTimeoutSeconds <= 0 {
		c.Upstream.ProbeTimeoutSeconds = 5
	}
	if c.Audit.BatchSize <= 0 {
		c.Audit.BatchSize = 100
	}
	if c.Audit.FlushInterval <= 0 {
		c.Audit.FlushInterval = 1000
	}
	if c.Audit.ChannelBuffer <= 0 {
		c.Audit.ChannelBuffer = 10000
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	return nil
}

// GetDefaultYAMLConfig returns a default configuration.
func GetDefaultYAMLConfig() *YAMLConfig {
	cfg := &YAMLConfig{
		Server:  ServerConfig{Port: 9002},
		Storage: StorageConfig{SQLitePath: "./data/llm-proxy.db"},
		Admin:   AdminConfig{Enabled: true},
		Upstream: UpstreamConfig{
			ProbeIntervalSeconds: 30,
			ProbeTimeoutSeconds:  5,
		},
		Audit: AuditConfig{
			Enabled:       true,
			BatchSize:     100,
			FlushInterval: 1000,
			ChannelBuffer: 10000,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
	return cfg
}

// LoadYAMLConfig loads configuration from a YAML file.
func LoadYAMLConfig(filename string) (*YAMLConfig, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return GetDefaultYAMLConfig(), nil
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	var config YAMLConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// LoadEnvironmentConfig loads base configuration and overlays environment-specific
// configuration based on the ENVIRONMENT variable (defaults to "dev").
func LoadEnvironmentConfig() (*YAMLConfig, error) {
	configDir := "configs"

	baseConfig, err := LoadYAMLConfig(filepath.Join(configDir, "base.yml"))
	if err != nil {
		return nil, fmt.Errorf("failed to load base configuration: %w", err)
	}

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "dev"
	}
	slog.Info("Loading environment configuration", "environment", env)

	envConfigPath := filepath.Join(configDir, fmt.Sprintf("%s.yml", env))
	if _, err := os.Stat(envConfigPath); os.IsNotExist(err) {
		return baseConfig, nil
	}

	data, err := os.ReadFile(envConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read env config %s: %w", envConfigPath, err)
	}

	// Unmarshal env overlay on top of base
	if err := yaml.Unmarshal(data, baseConfig); err != nil {
		return nil, fmt.Errorf("failed to parse env config: %w", err)
	}

	if err := baseConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid merged configuration: %w", err)
	}

	return baseConfig, nil
}

// LogConfiguration logs the configuration summary.
func (c *YAMLConfig) LogConfiguration(logger *slog.Logger) {
	logger.Info("Configuration",
		"port", c.Server.Port,
		"sqlite_path", c.Storage.SQLitePath,
		"admin_enabled", c.Admin.Enabled,
		"probe_interval", c.Upstream.ProbeIntervalSeconds,
		"audit_enabled", c.Audit.Enabled,
		"log_level", c.Logging.Level,
		"log_format", c.Logging.Format,
	)
}
