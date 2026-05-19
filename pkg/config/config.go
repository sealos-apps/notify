// Package config provides configuration loading and management for sealos-notify
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"
	"github.com/caarlos0/env/v9"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// GlobalConfig contains the complete application configuration
type GlobalConfig struct {
	// Configuration files
	ConfigPath string `yaml:"-" short:"c" help:"Path to configuration file (YAML format)" type:"path"`
	EnvFile    string `yaml:"-" help:"Path to .env file for environment variables" type:"path" default:".env"`

	// Server configuration
	Server ServerConfig `yaml:"server" embed:"" prefix:"server-" envPrefix:"SERVER_"`

	// Database configuration
	Database DatabaseConfig `yaml:"database" embed:"" prefix:"database-" envPrefix:"DATABASE_"`

	// Logging configuration
	Logging LoggingConfig `yaml:"logging" embed:"" prefix:"log-" envPrefix:"LOGGING_"`

	// Dispatcher configuration
	Dispatcher DispatcherConfig `yaml:"dispatcher" embed:"" prefix:"dispatcher-" envPrefix:"DISPATCHER_"`

	// API authentication configuration
	Auth AuthConfig `yaml:"auth" embed:"" prefix:"auth-" envPrefix:"AUTH_"`

	// Default notification settings
	Defaults DefaultsConfig `yaml:"defaults" kong:"-"`

	// Notification channels
	Channels map[string]ChannelConfig `yaml:"channels" kong:"-"`

	// Channel providers
	Providers map[string]ProviderConfig `yaml:"providers" kong:"-"`

	// Notification templates
	Templates map[string]TemplateConfig `yaml:"templates" kong:"-"`
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	Address      string        `yaml:"address" name:"address" env:"ADDRESS" default:":8080" help:"Server listen address"`
	ReadTimeout  time.Duration `yaml:"readTimeout" name:"read-timeout" env:"READ_TIMEOUT" default:"30s" help:"HTTP read timeout"`
	WriteTimeout time.Duration `yaml:"writeTimeout" name:"write-timeout" env:"WRITE_TIMEOUT" default:"30s" help:"HTTP write timeout"`
	IdleTimeout  time.Duration `yaml:"idleTimeout" name:"idle-timeout" env:"IDLE_TIMEOUT" default:"60s" help:"HTTP idle timeout"`
}

// DatabaseConfig contains database configuration
type DatabaseConfig struct {
	Host            string        `yaml:"host" name:"host" env:"HOST" default:"localhost" help:"Database host"`
	Port            int           `yaml:"port" name:"port" env:"PORT" default:"5432" help:"Database port"`
	User            string        `yaml:"user" name:"user" env:"USER" default:"postgres" help:"Database user"`
	Password        string        `yaml:"password" name:"password" env:"PASSWORD" default:"" help:"Database password"`
	DBName          string        `yaml:"dbname" name:"dbname" env:"DBNAME" default:"sealos_notify" help:"Database name"`
	SSLMode         string        `yaml:"sslMode" name:"ssl-mode" env:"SSL_MODE" default:"disable" help:"SSL mode"`
	MaxOpenConns    int           `yaml:"maxOpenConns" name:"max-open-conns" env:"MAX_OPEN_CONNS" default:"25" help:"Maximum open connections"`
	MaxIdleConns    int           `yaml:"maxIdleConns" name:"max-idle-conns" env:"MAX_IDLE_CONNS" default:"5" help:"Maximum idle connections"`
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime" name:"conn-max-lifetime" env:"CONN_MAX_LIFETIME" default:"5m" help:"Connection max lifetime"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level" name:"level" env:"LEVEL" default:"info" help:"Log level (debug, info, warn, error)"`
	Format string `yaml:"format" name:"format" env:"FORMAT" default:"json" help:"Log format (json, text)"`
	Debug  bool   `yaml:"debug" name:"debug" env:"DEBUG" default:"false" help:"Enable debug mode"`
}

// DispatcherConfig contains dispatcher configuration
type DispatcherConfig struct {
	Enabled      bool          `yaml:"enabled" name:"enabled" env:"ENABLED" default:"true" help:"Enable task dispatcher"`
	Interval     time.Duration `yaml:"interval" name:"interval" env:"INTERVAL" default:"10s" help:"Task polling interval"`
	BatchSize    int           `yaml:"batchSize" name:"batch-size" env:"BATCH_SIZE" default:"100" help:"Number of tasks to fetch per batch"`
	LeaseTimeout time.Duration `yaml:"leaseTimeout" name:"lease-timeout" env:"LEASE_TIMEOUT" default:"5m" help:"Task lease timeout"`
}

// AuthConfig contains app credential authentication configuration.
type AuthConfig struct {
	Enabled             bool   `yaml:"enabled" name:"enabled" env:"ENABLED" default:"true" help:"Enable API authentication"`
	CredentialsFilePath string `yaml:"credentialsFilePath" name:"credentials-file-path" env:"CREDENTIALS_FILE_PATH" default:"" help:"Path to mounted app credential Secret file"`
}

// DefaultsConfig contains default notification settings
type DefaultsConfig struct {
	TimeoutSeconds      int   `yaml:"timeoutSeconds"`
	MaxRetry            int   `yaml:"maxRetry"`
	RetryBackoffSeconds []int `yaml:"retryBackoffSeconds"`
}

// ChannelConfig defines a notification channel
type ChannelConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
}

// ProviderConfig defines a channel provider
type ProviderConfig struct {
	Type string                 `yaml:"type"`
	Data map[string]interface{} `yaml:",inline"`
}

// TemplateConfig defines a notification template
type TemplateConfig struct {
	Channel      string `yaml:"channel"`
	Subject      string `yaml:"subject,omitempty"`
	Body         string `yaml:"body,omitempty"`
	TemplateCode string `yaml:"templateCode,omitempty"`
	MsgType      string `yaml:"msgType,omitempty"`
}

// SecretRef references a Kubernetes Secret
type SecretRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

// LoadOptions contains options for loading configuration
type LoadOptions struct {
	Args          []string // CLI arguments
	ConfigContent []byte   // Optional: direct config content
	DisableExit   bool     // Disable os.Exit on parse errors
}

// LoadGlobalConfig loads configuration from CLI args, YAML file, and env vars
func LoadGlobalConfig(opts LoadOptions) (*GlobalConfig, error) {
	// Load .env file if exists (ignore errors)
	_ = godotenv.Load()

	cfg := &GlobalConfig{}

	// Parse CLI arguments
	parser, err := kong.New(cfg,
		kong.Name("sealos-notify"),
		kong.Description("Sealos notification service"),
		kong.UsageOnError(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CLI parser: %w", err)
	}

	// Parse arguments (DisableExit is handled via kong option at construction time)
	_ = opts.DisableExit

	if _, err := parser.Parse(opts.Args); err != nil {
		return nil, fmt.Errorf("failed to parse CLI arguments: %w", err)
	}

	// Load YAML configuration if provided
	var configContent []byte
	if opts.ConfigContent != nil {
		configContent = opts.ConfigContent
	} else if cfg.ConfigPath != "" {
		content, err := os.ReadFile(cfg.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		configContent = content
	}

	if configContent != nil {
		if err := yaml.Unmarshal(configContent, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	}

	// Load environment variables (override YAML and CLI)
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}
	expandProviderEnv(cfg.Providers)

	return cfg, nil
}

func expandProviderEnv(providers map[string]ProviderConfig) {
	for providerName, provider := range providers {
		provider.Data = expandMapEnv(provider.Data)
		providers[providerName] = provider
	}
}

func expandMapEnv(data map[string]interface{}) map[string]interface{} {
	for key, value := range data {
		switch v := value.(type) {
		case string:
			data[key] = os.ExpandEnv(v)
		case map[string]interface{}:
			data[key] = expandMapEnv(v)
		case []interface{}:
			for i, item := range v {
				if s, ok := item.(string); ok {
					v[i] = os.ExpandEnv(s)
				}
			}
			data[key] = v
		}
	}
	return data
}

// Validate validates the configuration
func (c *GlobalConfig) Validate() error {
	// Validate database configuration
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Database.DBName == "" {
		return fmt.Errorf("database name is required")
	}
	if c.Auth.Enabled && c.Auth.CredentialsFilePath == "" {
		return fmt.Errorf("auth.credentialsFilePath is required when auth is enabled")
	}

	// Validate channels
	for name, channel := range c.Channels {
		if !channel.Enabled {
			continue
		}
		if channel.Provider == "" {
			return fmt.Errorf("channel %s: provider is required", name)
		}
		if _, ok := c.Providers[channel.Provider]; !ok {
			return fmt.Errorf("channel %s: provider %s not found", name, channel.Provider)
		}
	}

	// Validate templates
	for name, template := range c.Templates {
		if template.Channel == "" {
			return fmt.Errorf("template %s: channel is required", name)
		}
		if _, ok := c.Channels[template.Channel]; !ok {
			return fmt.Errorf("template %s: channel %s not found", name, template.Channel)
		}
	}

	return nil
}

// ApplyHotReload applies hot-reloadable fields from newConfig
func (c *GlobalConfig) ApplyHotReload(newConfig *GlobalConfig) {
	c.Logging = newConfig.Logging
	c.Dispatcher = newConfig.Dispatcher
	c.Auth = newConfig.Auth
	c.Defaults = newConfig.Defaults
	c.Channels = newConfig.Channels
	c.Providers = newConfig.Providers
	c.Templates = newConfig.Templates
}

// GetDSN returns the database connection string
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode)
}

// LogConfig logs the current configuration (with sensitive data masked)
func (c *GlobalConfig) LogConfig(logger *log.Entry) {
	logger.WithFields(log.Fields{
		"server_address":     c.Server.Address,
		"database_host":      c.Database.Host,
		"database_name":      c.Database.DBName,
		"dispatcher_enabled": c.Dispatcher.Enabled,
		"log_level":          c.Logging.Level,
	}).Info("Configuration loaded")
}
