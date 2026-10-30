package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
)

const ENV_PREFIX = "GO_SPIN"

// Config holds all application configuration (immutable after load)
type Config struct {
	Misc       MiscConfig
	Data       DataConfig
	Server     ServerConfig
	Ghostboard GhostboardConfig
}

type ServerConfig struct {
	CORSAllowedOrigins string
	SecurityHeaders    SecurityHeadersConfig
	Port               int
	WaitingServerPort  int
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutDownTimeout    time.Duration
	RequestTimeout     time.Duration
}

// SecurityHeadersConfig controls the headers every response carries. An empty
// CSP means the built-in policy that fits the shipped UI, and an HSTS max age
// of zero leaves Strict-Transport-Security out.
type SecurityHeadersConfig struct {
	CSP        string
	HSTSMaxAge time.Duration
	Enabled    bool
}

type DataConfig struct {
	FilePath                 string
	BaseUrl                  string
	SpinUpUrl                string
	SystemMonitorMountPoint  string
	ActionsMode              string
	PersistInterval          time.Duration
	SchedulingPoll           time.Duration
	RefreshIntervalSecs      int
	StatsRefreshIntervalSecs int
	ActionsTimeoutSeconds    int
	SchedulingEnabled        bool
}

// GhostboardConfig tunes the shared-text boards served at /ui/ghostboard.
type GhostboardConfig struct {
	FilePath            string
	PINHMAC             string
	PINSecret           string
	PrivateBoards       []string
	ClearDelay          time.Duration
	MaxTextBytes        int
	PersistInterval     time.Duration
	HeartbeatInterval   time.Duration
	SessionTTL          time.Duration
	PINMaxAttempts      int
	PINLockout          time.Duration
	FilesEnabled        bool
	SealedEnabled       bool
	MaxFileBytes        int64
	MaxFileCount        int
	MaxBoardFileBytes   int64
	MaxTotalFileBytes   int64
	FileTTL             time.Duration
	FileTransferTimeout time.Duration
	SealedBurnTombstone time.Duration
}

// FilesRoot returns the directory holding the attachments, one sub-directory
// per board. It sits next to the persistence file, so the whole ghostboard
// state stays together and no separate path has to be configured.
func (g GhostboardConfig) FilesRoot() string {
	if g.FilePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(g.FilePath), "files")
}

const (
	defaultGhostboardClearDelaySecs      = 300
	defaultGhostboardMaxTextBytes        = 16777216
	defaultGhostboardPersistIntervalSecs = 2
	defaultGhostboardHeartbeatSecs       = 25
	defaultGhostboardSessionTTLSecs      = 28800
	defaultGhostboardPINMaxAttempts      = 5
	defaultGhostboardPINLockoutSecs      = 60

	defaultGhostboardMaxFileBytes            = 838860800 // 800 MiB
	defaultGhostboardMaxFileCount            = 10
	defaultGhostboardMaxBoardFileBytes       = 2147483648 // 2 GiB
	defaultGhostboardMaxTotalFileBytes       = 5368709120 // 5 GiB
	defaultGhostboardFileTTLSecs             = 86400      // 24 hours
	defaultGhostboardFileTransferTimeoutSecs = 600        // 10 minutes

	defaultGhostboardSealedBurnTombstoneSecs = 86400 // 24 hours
)

// DefaultGhostboardConfig returns the defaults documented in config.yaml.
func DefaultGhostboardConfig() GhostboardConfig {
	return GhostboardConfig{
		ClearDelay:        defaultGhostboardClearDelaySecs * time.Second,
		MaxTextBytes:      defaultGhostboardMaxTextBytes,
		PersistInterval:   defaultGhostboardPersistIntervalSecs * time.Second,
		HeartbeatInterval: defaultGhostboardHeartbeatSecs * time.Second,
		SessionTTL:        defaultGhostboardSessionTTLSecs * time.Second,
		PINMaxAttempts:    defaultGhostboardPINMaxAttempts,
		PINLockout:        defaultGhostboardPINLockoutSecs * time.Second,

		FilesEnabled:        true,
		MaxFileBytes:        defaultGhostboardMaxFileBytes,
		MaxFileCount:        defaultGhostboardMaxFileCount,
		MaxBoardFileBytes:   defaultGhostboardMaxBoardFileBytes,
		MaxTotalFileBytes:   defaultGhostboardMaxTotalFileBytes,
		FileTTL:             defaultGhostboardFileTTLSecs * time.Second,
		FileTransferTimeout: defaultGhostboardFileTransferTimeoutSecs * time.Second,

		SealedEnabled:       true,
		SealedBurnTombstone: defaultGhostboardSealedBurnTombstoneSecs * time.Second,
	}
}

// WithDefaults fills unset fields with their defaults. It keeps an empty
// FilePath as-is, which disables persistence.
func (g GhostboardConfig) WithDefaults() GhostboardConfig {
	defaults := DefaultGhostboardConfig()
	if g.ClearDelay <= 0 {
		g.ClearDelay = defaults.ClearDelay
	}
	if g.MaxTextBytes <= 0 {
		g.MaxTextBytes = defaults.MaxTextBytes
	}
	if g.PersistInterval <= 0 {
		g.PersistInterval = defaults.PersistInterval
	}
	if g.HeartbeatInterval <= 0 {
		g.HeartbeatInterval = defaults.HeartbeatInterval
	}
	if g.SessionTTL <= 0 {
		g.SessionTTL = defaults.SessionTTL
	}
	if g.PINMaxAttempts <= 0 {
		g.PINMaxAttempts = defaults.PINMaxAttempts
	}
	if g.PINLockout <= 0 {
		g.PINLockout = defaults.PINLockout
	}
	if g.MaxFileBytes <= 0 {
		g.MaxFileBytes = defaults.MaxFileBytes
	}
	if g.MaxFileCount <= 0 {
		g.MaxFileCount = defaults.MaxFileCount
	}
	if g.MaxBoardFileBytes <= 0 {
		g.MaxBoardFileBytes = defaults.MaxBoardFileBytes
	}
	if g.MaxTotalFileBytes <= 0 {
		g.MaxTotalFileBytes = defaults.MaxTotalFileBytes
	}
	if g.FileTTL <= 0 {
		g.FileTTL = defaults.FileTTL
	}
	if g.FileTransferTimeout <= 0 {
		g.FileTransferTimeout = defaults.FileTransferTimeout
	}
	if g.SealedBurnTombstone <= 0 {
		g.SealedBurnTombstone = defaults.SealedBurnTombstone
	}
	return g
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
	viper.SetDefault("server.security_headers.enabled", true)
	viper.SetDefault("server.security_headers.csp", "")
	viper.SetDefault("server.security_headers.hsts_max_age_secs", 0)

	viper.SetDefault("data.file_path", confPath+"/data/config.json")
	viper.SetDefault("data.persist_interval_secs", 5)
	viper.SetDefault("data.scheduling_enabled", true)
	viper.SetDefault("data.scheduling_poll_interval_secs", 30)
	viper.SetDefault("data.base_url", "http://localhost/")
	viper.SetDefault("data.spin_up_url", "http://localhost/")
	viper.SetDefault("data.refresh_interval_secs", 60)
	viper.SetDefault("data.stats_refresh_interval_secs", 120)
	viper.SetDefault("data.system_monitor_mount_point", "/")
	viper.SetDefault("data.actions_mode", "async")
	viper.SetDefault("data.actions_timeout_seconds", 30)
	viper.SetDefault("misc.gin_mode", "release")
	viper.SetDefault("misc.scheduling_timezone", "Local")
	viper.SetDefault("misc.runtime_type", "docker")
	viper.SetDefault("misc.log_level", "info")
	viper.SetDefault("misc.cosmos_base_url", "")
	viper.SetDefault("misc.cosmos_token", "")
	viper.SetDefault("ghostboard.file_path", "")
	viper.SetDefault("ghostboard.clear_delay_secs", defaultGhostboardClearDelaySecs)
	viper.SetDefault("ghostboard.max_text_bytes", defaultGhostboardMaxTextBytes)
	viper.SetDefault("ghostboard.persist_interval_secs", defaultGhostboardPersistIntervalSecs)
	viper.SetDefault("ghostboard.heartbeat_interval_secs", defaultGhostboardHeartbeatSecs)
	viper.SetDefault("ghostboard.pin_hmac", "")
	viper.SetDefault("ghostboard.pin_secret", "")
	viper.SetDefault("ghostboard.private_boards", []string{})
	viper.SetDefault("ghostboard.session_ttl_secs", defaultGhostboardSessionTTLSecs)
	viper.SetDefault("ghostboard.pin_max_attempts", defaultGhostboardPINMaxAttempts)
	viper.SetDefault("ghostboard.pin_lockout_secs", defaultGhostboardPINLockoutSecs)
	viper.SetDefault("ghostboard.files_enabled", true)
	viper.SetDefault("ghostboard.max_file_bytes", defaultGhostboardMaxFileBytes)
	viper.SetDefault("ghostboard.max_file_count", defaultGhostboardMaxFileCount)
	viper.SetDefault("ghostboard.max_board_file_bytes", defaultGhostboardMaxBoardFileBytes)
	viper.SetDefault("ghostboard.max_total_file_bytes", defaultGhostboardMaxTotalFileBytes)
	viper.SetDefault("ghostboard.file_ttl_secs", defaultGhostboardFileTTLSecs)
	viper.SetDefault("ghostboard.file_transfer_timeout_secs", defaultGhostboardFileTransferTimeoutSecs)
	viper.SetDefault("ghostboard.sealed_enabled", true)
	viper.SetDefault("ghostboard.sealed_burn_tombstone_secs", defaultGhostboardSealedBurnTombstoneSecs)

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

	ghostboardFilePath := viper.GetString("ghostboard.file_path")
	if ghostboardFilePath == "" {
		// Keep board data next to the main data file unless a dedicated path
		// is configured, so the whole application state stays together.
		ghostboardFilePath = filepath.Join(filepath.Dir(viper.GetString("data.file_path")), "ghostboard.json")
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
			CORSAllowedOrigins: corsAllowedOrigins(),
			SecurityHeaders: SecurityHeadersConfig{
				Enabled:    viper.GetBool("server.security_headers.enabled"),
				CSP:        strings.TrimSpace(viper.GetString("server.security_headers.csp")),
				HSTSMaxAge: time.Duration(viper.GetInt("server.security_headers.hsts_max_age_secs")) * time.Second,
			},
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
			SystemMonitorMountPoint:  viper.GetString("data.system_monitor_mount_point"),
			ActionsMode:              viper.GetString("data.actions_mode"),
			ActionsTimeoutSeconds:    viper.GetInt("data.actions_timeout_seconds"),
		},

		Ghostboard: GhostboardConfig{
			FilePath:          ghostboardFilePath,
			ClearDelay:        time.Duration(viper.GetInt("ghostboard.clear_delay_secs")) * time.Second,
			MaxTextBytes:      viper.GetInt("ghostboard.max_text_bytes"),
			PersistInterval:   time.Duration(viper.GetInt("ghostboard.persist_interval_secs")) * time.Second,
			HeartbeatInterval: time.Duration(viper.GetInt("ghostboard.heartbeat_interval_secs")) * time.Second,
			PINHMAC:           strings.TrimSpace(viper.GetString("ghostboard.pin_hmac")),
			PINSecret:         strings.TrimSpace(viper.GetString("ghostboard.pin_secret")),
			PrivateBoards:     ghostboardPrivateBoards(),
			SessionTTL:        time.Duration(viper.GetInt("ghostboard.session_ttl_secs")) * time.Second,
			PINMaxAttempts:    viper.GetInt("ghostboard.pin_max_attempts"),
			PINLockout:        time.Duration(viper.GetInt("ghostboard.pin_lockout_secs")) * time.Second,

			FilesEnabled:        viper.GetBool("ghostboard.files_enabled"),
			MaxFileBytes:        viper.GetInt64("ghostboard.max_file_bytes"),
			MaxFileCount:        viper.GetInt("ghostboard.max_file_count"),
			MaxBoardFileBytes:   viper.GetInt64("ghostboard.max_board_file_bytes"),
			MaxTotalFileBytes:   viper.GetInt64("ghostboard.max_total_file_bytes"),
			FileTTL:             time.Duration(viper.GetInt("ghostboard.file_ttl_secs")) * time.Second,
			FileTransferTimeout: time.Duration(viper.GetInt("ghostboard.file_transfer_timeout_secs")) * time.Second,
			SealedEnabled:       viper.GetBool("ghostboard.sealed_enabled"),
			SealedBurnTombstone: time.Duration(viper.GetInt("ghostboard.sealed_burn_tombstone_secs")) * time.Second,
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

// defaultCORSAllowedOrigins is the permissive allow-list used when the CORS key
// is absent from both the configuration file and the environment.
const defaultCORSAllowedOrigins = "*"

// corsAllowedOrigins resolves the CORS allow-list. The canonical key is
// misc.cors_allowed_origins (the one documented in config/config.yaml and
// README.md); server.cors_allowed_origins is still
// honoured as a deprecated alias so upgrades never silently widen the
// allow-list to the permissive default. When both keys are set to different
// values the canonical one wins, with a warning naming both: an installation that
// still overrides the deprecated key through the environment would otherwise
// switch allow-lists without any trace in the logs.
func corsAllowedOrigins() string {
	miscOrigins := viper.GetString("misc.cors_allowed_origins")
	serverOrigins := viper.GetString("server.cors_allowed_origins")

	switch {
	case miscOrigins != "" && serverOrigins != "" && miscOrigins != serverOrigins:
		logger.WithComponent("config").Warnf("both misc.cors_allowed_origins (%q) and the deprecated server.cors_allowed_origins (%q) are set, using misc.cors_allowed_origins", miscOrigins, serverOrigins)
		return miscOrigins
	case miscOrigins != "":
		return miscOrigins
	case serverOrigins != "":
		logger.WithComponent("config").Warn("server.cors_allowed_origins is deprecated, use misc.cors_allowed_origins")
		return serverOrigins
	default:
		return defaultCORSAllowedOrigins
	}
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
	if c.Server.SecurityHeaders.HSTSMaxAge < 0 {
		return fmt.Errorf("server.security_headers.hsts_max_age_secs must not be negative")
	}
	// A policy ends up in a header: a line break in it would let a value from
	// the configuration file forge the rest of the response.
	if strings.ContainsAny(c.Server.SecurityHeaders.CSP, "\r\n") {
		return fmt.Errorf("server.security_headers.csp must be a single line")
	}
	if c.Misc.SchedulingTZ != "" && c.Misc.SchedulingTZ != "Local" {
		if _, err := time.LoadLocation(c.Misc.SchedulingTZ); err != nil {
			return fmt.Errorf("misc.scheduling_timezone is invalid: %w", err)
		}
	}
	if c.Data.ActionsMode != "" && c.Data.ActionsMode != "sync" && c.Data.ActionsMode != "async" {
		return fmt.Errorf("data.actions_mode must be 'sync' or 'async', got %q", c.Data.ActionsMode)
	}
	if c.Data.ActionsTimeoutSeconds <= 0 {
		return fmt.Errorf("data.actions_timeout_seconds must be positive")
	}
	if c.Ghostboard.ClearDelay <= 0 {
		return fmt.Errorf("ghostboard.clear_delay_secs must be positive")
	}
	if c.Ghostboard.MaxTextBytes <= 0 {
		return fmt.Errorf("ghostboard.max_text_bytes must be positive")
	}
	if c.Ghostboard.PersistInterval <= 0 {
		return fmt.Errorf("ghostboard.persist_interval_secs must be positive")
	}
	if c.Ghostboard.HeartbeatInterval <= 0 {
		return fmt.Errorf("ghostboard.heartbeat_interval_secs must be positive")
	}
	if c.Ghostboard.SealedBurnTombstone <= 0 {
		return fmt.Errorf("ghostboard.sealed_burn_tombstone_secs must be positive")
	}
	if err := c.Ghostboard.validatePIN(); err != nil {
		return err
	}
	if err := c.Ghostboard.validateFiles(); err != nil {
		return err
	}

	return nil
}

// validateFiles checks the attachment limits. They are validated even when
// files_enabled is false, so turning the feature on can never fail at runtime
// on a value nobody looked at.
func (g GhostboardConfig) validateFiles() error {
	if g.MaxFileBytes <= 0 {
		return fmt.Errorf("ghostboard.max_file_bytes must be positive")
	}
	if g.MaxFileCount <= 0 {
		return fmt.Errorf("ghostboard.max_file_count must be positive")
	}
	if g.MaxBoardFileBytes <= 0 {
		return fmt.Errorf("ghostboard.max_board_file_bytes must be positive")
	}
	if g.MaxTotalFileBytes <= 0 {
		return fmt.Errorf("ghostboard.max_total_file_bytes must be positive")
	}
	if g.MaxFileBytes > g.MaxBoardFileBytes || g.MaxBoardFileBytes > g.MaxTotalFileBytes {
		return fmt.Errorf("ghostboard file limits must not decrease: max_file_bytes <= max_board_file_bytes <= max_total_file_bytes")
	}
	if g.FileTTL <= 0 {
		return fmt.Errorf("ghostboard.file_ttl_secs must be positive")
	}
	if g.FileTransferTimeout <= 0 {
		return fmt.Errorf("ghostboard.file_transfer_timeout_secs must be positive")
	}
	return nil
}

// validatePIN checks the settings of the PIN that protects the private boards.
// A configuration that names a private board without a PIN would leave the
// board open while looking protected, so it is rejected at startup.
func (g GhostboardConfig) validatePIN() error {
	if g.SessionTTL <= 0 {
		return fmt.Errorf("ghostboard.session_ttl_secs must be positive")
	}
	if g.PINMaxAttempts <= 0 {
		return fmt.Errorf("ghostboard.pin_max_attempts must be positive")
	}
	if g.PINLockout <= 0 {
		return fmt.Errorf("ghostboard.pin_lockout_secs must be positive")
	}

	for _, name := range g.PrivateBoards {
		if err := ghostboard.ValidateBoardName(name); err != nil {
			return fmt.Errorf("ghostboard.private_boards contains an invalid board name %q: %w", name, err)
		}
	}

	if len(g.PrivateBoards) > 0 && g.PINHMAC == "" {
		return fmt.Errorf("ghostboard.pin_hmac is required when ghostboard.private_boards is set: generate it with gb-cli pin-hash")
	}
	if g.PINHMAC == "" {
		return nil
	}

	if err := ghostboard.ValidatePINSecret(g.PINSecret); err != nil {
		return fmt.Errorf("%w (set it in GO_SPIN_GHOSTBOARD_PIN_SECRET, never in the configuration file)", err)
	}
	if _, err := ghostboard.NewAuthenticator(ghostboard.AuthConfig{
		PINDigest:   g.PINHMAC,
		Secret:      g.PINSecret,
		SessionTTL:  g.SessionTTL,
		MaxAttempts: g.PINMaxAttempts,
		Lockout:     g.PINLockout,
	}); err != nil {
		return err
	}

	return nil
}

// ghostboardPrivateBoards returns the board names that need the PIN, trimmed
// and without the empty entries a YAML list often carries.
func ghostboardPrivateBoards() []string {
	raw := viper.GetStringSlice("ghostboard.private_boards")
	boards := make([]string, 0, len(raw))
	for _, name := range raw {
		if name = strings.TrimSpace(name); name != "" {
			boards = append(boards, name)
		}
	}
	return boards
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
