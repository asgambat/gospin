package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bassista/go_spin/internal/logger"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

const ENV_PREFIX = "GO_SPIN"

// Config holds all application configuration (immutable after load)
type Config struct {
	Server ServerConfig
	Data   DataConfig
	Misc   MiscConfig
}

type ServerConfig struct {
	Port               int
	WaitingServerPort  int
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutDownTimeout    time.Duration
	RequestTimeout     time.Duration
	CORSAllowedOrigins string // CORS allowed origins, default "*"
}

type DataConfig struct {
	FilePath                 string
	PersistInterval          time.Duration
	SchedulingEnabled        bool
	SchedulingPoll           time.Duration
	BaseUrl                  string
	SpinUpUrl                string
	RefreshIntervalSecs      int
	StatsRefreshIntervalSecs int
}

type MiscConfig struct {
	GinMode       string
	SchedulingTZ  string
	RuntimeType   string // "docker" o "memory"
	LogLevel      string // "debug", "info", "warn", "error", default "info"
	CosmosBaseUrl string // Base URL for Cosmos API (e.g., "https://cosmos.example.com")
	CosmosToken   string // Bearer token for Cosmos API authentication
}

// LoadConfig loads configuration from file, env vars and validates required fields.
// Returns error if validation fails (fail-fast).
func LoadConfig() (*Config, error) {
	logger.WithComponent("config").Debugf("loading configuration, config path env var: %s_CONFIG_PATH", ENV_PREFIX)
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		logger.WithComponent("config").Info("No .env file found (that's okay in production)")
	}

	confPath := getEnvOrDefault(ENV_PREFIX+"_CONFIG_PATH", "./config")
	viper.AddConfigPath(confPath)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Set defaults
	viper.SetDefault("server.port", 8084)
	viper.SetDefault("server.waiting_server_port", 8085)
	viper.SetDefault("server.read_timeout_secs", 10)
	viper.SetDefault("server.write_timeout_secs", 10)
	viper.SetDefault("server.idle_timeout_secs", 120)
	viper.SetDefault("server.shutdown_timeout_secs", 5)
	viper.SetDefault("server.request_timeout_millis", 1000)
	viper.SetDefault("server.cors_allowed_origins", "*")

	viper.SetDefault("data.file_path", confPath+"/data/config.json")
	viper.SetDefault("data.persist_interval_secs", 5)
	viper.SetDefault("data.scheduling_enabled", true)
	viper.SetDefault("data.scheduling_poll_interval_secs", 30)
	viper.SetDefault("data.base_url", "http://localhost/")
	viper.SetDefault("data.spin_up_url", "http://localhost/")
	viper.SetDefault("data.refresh_interval_secs", 60)
	viper.SetDefault("data.stats_refresh_interval_secs", 120)
	viper.SetDefault("misc.gin_mode", "release")
	viper.SetDefault("misc.scheduling_timezone", "Local")
	viper.SetDefault("misc.runtime_type", "docker")
	viper.SetDefault("misc.log_level", "info")
	viper.SetDefault("misc.cosmos_base_url", "")
	viper.SetDefault("misc.cosmos_token", "")

	// Environment variables automatically override config file values
	viper.AutomaticEnv()
	viper.SetEnvPrefix(ENV_PREFIX)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			logger.WithComponent("config").Info("No config file found, using defaults and env vars")
		} else {
			return nil, fmt.Errorf("config file error: %w", err)
		}
	}

	if err := dataFileExistenceCheck(); err != nil {
		return nil, err
	}

	port, err := getEnvOrViperPort("PORT", "server.port")
	if err != nil {
		return nil, err
	}

	portWaitingServer, err := getEnvOrViperPort("WAITING_SERVER_PORT", "server.waiting_server_port")
	if err != nil {
		return nil, err
	}

	// Build immutable config struct
	cfg := &Config{
		Server: ServerConfig{
			Port:               port,
			WaitingServerPort:  portWaitingServer,
			ReadTimeout:        time.Duration(viper.GetInt("server.read_timeout_secs")) * time.Second,
			WriteTimeout:       time.Duration(viper.GetInt("server.write_timeout_secs")) * time.Second,
			IdleTimeout:        time.Duration(viper.GetInt("server.idle_timeout_secs")) * time.Second,
			ShutDownTimeout:    time.Duration(viper.GetInt("server.shutdown_timeout_secs")) * time.Second,
			RequestTimeout:     time.Duration(viper.GetInt("server.request_timeout_millis")) * time.Millisecond,
			CORSAllowedOrigins: viper.GetString("server.cors_allowed_origins"),
		},
		Data: DataConfig{
			FilePath:                 viper.GetString("data.file_path"),
			PersistInterval:          time.Duration(viper.GetInt("data.persist_interval_secs")) * time.Second,
			SchedulingEnabled:        viper.GetBool("data.scheduling_enabled"),
			SchedulingPoll:           time.Duration(viper.GetInt("data.scheduling_poll_interval_secs")) * time.Second,
			BaseUrl:                  viper.GetString("data.base_url"),
			SpinUpUrl:                viper.GetString("data.spin_up_url"),
			RefreshIntervalSecs:      viper.GetInt("data.refresh_interval_secs"),
			StatsRefreshIntervalSecs: viper.GetInt("data.stats_refresh_interval_secs"),
		},
		Misc: MiscConfig{
			GinMode:       viper.GetString("misc.gin_mode"),
			SchedulingTZ:  viper.GetString("misc.scheduling_timezone"),
			RuntimeType:   viper.GetString("misc.runtime_type"),
			LogLevel:      viper.GetString("misc.log_level"),
			CosmosBaseUrl: viper.GetString("misc.cosmos_base_url"),
			CosmosToken:   viper.GetString("misc.cosmos_token"),
		},
	}

	logger.WithComponent("config").Debugf("configuration loaded: port=%d, gin_mode=%s, runtime_type=%s, scheduling_enabled=%v, scheduling_tz=%s, cosmos_base_url=%s, cosmos_token=%s",
		cfg.Server.Port, cfg.Misc.GinMode, cfg.Misc.RuntimeType, cfg.Data.SchedulingEnabled, cfg.Misc.SchedulingTZ, cfg.Misc.CosmosBaseUrl, maskToken(cfg.Misc.CosmosToken))

	// Fail-fast validation
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	fmt.Println("All configuration loaded successfully")

	return cfg, nil
}

func dataFileExistenceCheck() error {
	fileStorePath := viper.GetString("data.file_path")
	logger.WithComponent("config").Infof("Using data file: %s", fileStorePath)

	// Ensure the directory for the data file exists
	dataDir := filepath.Dir(fileStorePath)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	//if the file does not exist, create an empty one with empty json object
	if _, err := os.Stat(fileStorePath); os.IsNotExist(err) {
		emptyFile, err := os.Create(fileStorePath)
		if err != nil {
			return fmt.Errorf("failed to create data file %s: %w", fileStorePath, err)
		}
		defer func() {
			if cerr := emptyFile.Close(); cerr != nil {
				logger.WithComponent("config").Errorf("failed to close data file %s: %v", fileStorePath, cerr)
			}
		}()
		if _, err := emptyFile.WriteString("{}"); err != nil {
			return fmt.Errorf("failed to write to data file %s: %w", fileStorePath, err)
		}
		logger.WithComponent("config").Infof("Created new EMPTY data file: %s", fileStorePath)
	}
	return nil
}

// validate checks required configuration fields
func (c *Config) validate() error {
	if c.Data.RefreshIntervalSecs <= 0 {
		return fmt.Errorf("data.refresh_interval_secs must be positive")
	}
	if c.Data.StatsRefreshIntervalSecs <= 0 {
		return fmt.Errorf("data.stats_refresh_interval_secs must be positive")
	}
	if c.Data.FilePath == "" {
		return fmt.Errorf("data.file_path configuration is required")
	}
	if c.Data.PersistInterval <= 0 {
		return fmt.Errorf("data.persist_interval_secs must be positive")
	}
	if c.Data.SchedulingPoll <= 0 {
		return fmt.Errorf("data.scheduling_poll_interval_secs must be positive")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be a valid TCP port (1-65535)")
	}
	if c.Server.ShutDownTimeout <= 0 {
		return fmt.Errorf("server.shutdown_timeout_secs must be positive")
	}
	if c.Server.IdleTimeout <= 0 {
		return fmt.Errorf("server.idle_timeout_secs must be positive")
	}
	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("server.write_timeout_secs must be positive")
	}
	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("server.read_timeout_secs must be positive")
	}
	if c.Server.RequestTimeout <= 0 {
		return fmt.Errorf("server.request_timeout_millis must be positive")
	}
	if c.Misc.SchedulingTZ != "" && c.Misc.SchedulingTZ != "Local" {
		if _, err := time.LoadLocation(c.Misc.SchedulingTZ); err != nil {
			return fmt.Errorf("misc.scheduling_timezone is invalid: %w", err)
		}
	}

	return nil
}

// getEnvOrDefault returns env var value or default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvOrViperPort(envKey, viperKey string) (int, error) {
	if value := os.Getenv(envKey); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("%s must be a valid integer TCP port: %w", envKey, err)
		}
		return port, nil
	}
	return viper.GetInt(viperKey), nil
}

func maskToken(token string) string {
	if len(token) <= 8 {
		if len(token) == 0 {
			return "(not set)"
		}
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
