package config

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
)

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:               8080,
			ReadTimeout:        10 * time.Second,
			WriteTimeout:       10 * time.Second,
			IdleTimeout:        120 * time.Second,
			ShutDownTimeout:    5 * time.Second,
			RequestTimeout:     1000 * time.Millisecond,
			CORSAllowedOrigins: "*",
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingEnabled:        true,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			GinMode:      "release",
			SchedulingTZ: "Local",
			RuntimeType:  "docker",
		},
		Ghostboard: DefaultGhostboardConfig(),
	}

	if err := cfg.validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestConfig_Validate_EmptyFilePath(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for empty file path")
	}
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero port", 0},
		{"negative port", -1},
		{"too high port", 65536},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:            tt.port,
					ReadTimeout:     10 * time.Second,
					WriteTimeout:    10 * time.Second,
					IdleTimeout:     120 * time.Second,
					ShutDownTimeout: 5 * time.Second,
					RequestTimeout:  1000 * time.Millisecond,
				}, Data: DataConfig{
					FilePath:                 "/tmp/config.json",
					PersistInterval:          5 * time.Second,
					SchedulingPoll:           30 * time.Second,
					RefreshIntervalSecs:      60,
					StatsRefreshIntervalSecs: 120,
					ActionsTimeoutSeconds:    30,
				},
				Misc: MiscConfig{
					SchedulingTZ: "Local",
				},
			}

			err := cfg.validate()
			if err == nil {
				t.Errorf("expected error for port %d", tt.port)
			}
		})
	}
}

func TestConfig_Validate_InvalidPersistInterval(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          0,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for zero persist interval")
	}
}

func TestConfig_Validate_InvalidTimeouts(t *testing.T) {
	tests := []struct {
		name            string
		readTimeout     time.Duration
		writeTimeout    time.Duration
		idleTimeout     time.Duration
		shutdownTimeout time.Duration
	}{
		{"zero read timeout", 0, 10 * time.Second, 120 * time.Second, 5 * time.Second},
		{"zero write timeout", 10 * time.Second, 0, 120 * time.Second, 5 * time.Second},
		{"zero idle timeout", 10 * time.Second, 10 * time.Second, 0, 5 * time.Second},
		{"zero shutdown timeout", 10 * time.Second, 10 * time.Second, 120 * time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:            8080,
					ReadTimeout:     tt.readTimeout,
					WriteTimeout:    tt.writeTimeout,
					IdleTimeout:     tt.idleTimeout,
					ShutDownTimeout: tt.shutdownTimeout,
					RequestTimeout:  1000 * time.Millisecond,
				},
				Data: DataConfig{
					FilePath:                 "/tmp/config.json",
					PersistInterval:          5 * time.Second,
					SchedulingPoll:           30 * time.Second,
					RefreshIntervalSecs:      60,
					StatsRefreshIntervalSecs: 120,
					ActionsTimeoutSeconds:    30,
				},
				Misc: MiscConfig{
					SchedulingTZ: "Local",
				},
			}

			err := cfg.validate()
			if err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}

func TestConfig_Validate_InvalidSchedulingPoll(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           0,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for zero scheduling poll")
	}
}

func TestConfig_Validate_InvalidTimezone(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Invalid/Timezone",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for invalid timezone")
	}
}

func TestConfig_Validate_ValidTimezones(t *testing.T) {
	timezones := []string{"Local", "UTC", "America/New_York", "Europe/Rome"}

	for _, tz := range timezones {
		t.Run(tz, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:            8080,
					ReadTimeout:     10 * time.Second,
					WriteTimeout:    10 * time.Second,
					IdleTimeout:     120 * time.Second,
					ShutDownTimeout: 5 * time.Second,
					RequestTimeout:  1000 * time.Millisecond,
				},
				Data: DataConfig{
					FilePath:                 "/tmp/config.json",
					PersistInterval:          5 * time.Second,
					SchedulingPoll:           30 * time.Second,
					RefreshIntervalSecs:      60,
					StatsRefreshIntervalSecs: 120,
					ActionsTimeoutSeconds:    30,
				},
				Misc: MiscConfig{
					SchedulingTZ: tz,
				},
				Ghostboard: DefaultGhostboardConfig(),
			}

			if err := cfg.validate(); err != nil {
				t.Errorf("expected valid timezone %s, got error: %v", tz, err)
			}
		})
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	// Test with env var set
	_ = os.Setenv("TEST_ENV_VAR", "custom_value")
	defer func() { _ = os.Unsetenv("TEST_ENV_VAR") }()

	result := getEnvOrDefault("TEST_ENV_VAR", "default_value")
	if result != "custom_value" {
		t.Errorf("expected 'custom_value', got '%s'", result)
	}

	// Test with env var not set
	result = getEnvOrDefault("NONEXISTENT_VAR", "default_value")
	if result != "default_value" {
		t.Errorf("expected 'default_value', got '%s'", result)
	}
}

func TestGetEnvOrViperPort_FromEnv(t *testing.T) {
	_ = os.Setenv("TEST_PORT", "9090")
	defer func() { _ = os.Unsetenv("TEST_PORT") }()

	port, err := getEnvOrViperPort("TEST_PORT", "server.port")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if port != 9090 {
		t.Errorf("expected 9090, got %d", port)
	}
}

func TestGetEnvOrViperPort_InvalidEnv(t *testing.T) {
	_ = os.Setenv("TEST_PORT_INVALID", "not_a_number")
	defer func() { _ = os.Unsetenv("TEST_PORT_INVALID") }()

	_, err := getEnvOrViperPort("TEST_PORT_INVALID", "server.port")
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestConfig_Validate_ZeroRefreshInterval(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      0,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for zero refresh interval")
	}
}

func TestConfig_Validate_ZeroStatsRefreshInterval(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 0,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for zero stats refresh interval")
	}
}

func TestConfig_Validate_ZeroRequestTimeout(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  0,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for zero request timeout")
	}
}

func TestConfig_Validate_InvalidActionsMode(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
			ActionsMode:              "invalid",
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for invalid actions_mode")
	}
}

func TestConfig_Validate_ValidActionsModes(t *testing.T) {
	modes := []string{"", "sync", "async"}

	for _, mode := range modes {
		t.Run("mode="+mode, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:            8080,
					ReadTimeout:     10 * time.Second,
					WriteTimeout:    10 * time.Second,
					IdleTimeout:     120 * time.Second,
					ShutDownTimeout: 5 * time.Second,
					RequestTimeout:  1000 * time.Millisecond,
				},
				Data: DataConfig{
					FilePath:                 "/tmp/config.json",
					PersistInterval:          5 * time.Second,
					SchedulingPoll:           30 * time.Second,
					RefreshIntervalSecs:      60,
					StatsRefreshIntervalSecs: 120,
					ActionsTimeoutSeconds:    30,
					ActionsMode:              mode,
				},
				Misc: MiscConfig{
					SchedulingTZ: "Local",
				},
				Ghostboard: DefaultGhostboardConfig(),
			}

			if err := cfg.validate(); err != nil {
				t.Errorf("expected valid actions_mode %q, got error: %v", mode, err)
			}
		})
	}
}

func TestConfig_Validate_ZeroActionsTimeout(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    0,
		},
		Misc: MiscConfig{
			SchedulingTZ: "Local",
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("expected error for zero actions_timeout_seconds")
	}
}

func TestConfig_Validate_EmptyTimezone(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{
			SchedulingTZ: "",
		},
		Ghostboard: DefaultGhostboardConfig(),
	}

	// Empty timezone should be valid (defaults to Local)
	err := cfg.validate()
	if err != nil {
		t.Errorf("expected no error for empty timezone, got: %v", err)
	}
}

func TestGetEnvOrViperPort_FromViper(t *testing.T) {
	// Test with no env var set - should use viper value (which may be 0 if not configured)
	port, err := getEnvOrViperPort("NONEXISTENT_PORT_VAR_12345", "server.port")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// When env var is not set, getEnvOrViperPort returns viper value
	// In test context, viper may not be configured, so port can be 0 or the default
	// We just verify no error was returned - the value depends on viper state
	_ = port
}

func TestGetEnvOrDefault_EmptyValue(t *testing.T) {
	// Test with env var set to empty
	_ = os.Setenv("TEST_EMPTY_VAR", "")
	defer func() { _ = os.Unsetenv("TEST_EMPTY_VAR") }()

	result := getEnvOrDefault("TEST_EMPTY_VAR", "default_value")
	// Empty string should return default
	if result != "default_value" {
		t.Errorf("expected 'default_value' for empty env, got '%s'", result)
	}
}

func TestLoadConfig_WithValidDefaults(t *testing.T) {
	// Create a temp dir for config
	tempDir := t.TempDir()
	dataDir := tempDir + "/data"

	// Set environment variables to use temp directory
	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataDir+"/config.json")
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected no error loading config, got: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	// Verify default values
	if cfg.Server.Port <= 0 {
		t.Errorf("expected positive port, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout <= 0 {
		t.Error("expected positive read timeout")
	}
	if cfg.Server.WriteTimeout <= 0 {
		t.Error("expected positive write timeout")
	}
	if cfg.Server.IdleTimeout <= 0 {
		t.Error("expected positive idle timeout")
	}
	if cfg.Data.PersistInterval <= 0 {
		t.Error("expected positive persist interval")
	}
	if cfg.Data.SchedulingPoll <= 0 {
		t.Error("expected positive scheduling poll interval")
	}
}

func TestLoadConfig_WithCustomPort(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := tempDir + "/data"

	// Set custom port via env var
	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataDir+"/config.json")
	_ = os.Setenv("PORT", "9999")
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
		_ = os.Unsetenv("PORT")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected no error loading config, got: %v", err)
	}

	if cfg.Server.Port != 9999 {
		t.Errorf("expected port 9999, got %d", cfg.Server.Port)
	}
}

func TestLoadConfig_WithInvalidPort(t *testing.T) {
	tempDir := t.TempDir()

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("PORT", "not_a_port")
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("PORT")
	}()

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid port, got nil")
	}
}

func TestLoadConfig_WithInvalidWaitingServerPort(t *testing.T) {
	tempDir := t.TempDir()

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("WAITING_SERVER_PORT", "invalid")
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("WAITING_SERVER_PORT")
	}()

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid waiting server port, got nil")
	}
}

func TestLoadConfig_CreatesDataFile(t *testing.T) {
	tempDir := t.TempDir()
	dataFilePath := tempDir + "/data/test_config.json"

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataFilePath)
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
	}()

	// Verify file doesn't exist
	if _, statErr := os.Stat(dataFilePath); !os.IsNotExist(statErr) {
		t.Fatal("expected data file to not exist initially")
	}

	_, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(dataFilePath); os.IsNotExist(err) {
		t.Error("expected data file to be created")
	}

	// Verify file has empty JSON object
	content, err := os.ReadFile(dataFilePath)
	if err != nil {
		t.Fatalf("failed to read data file: %v", err)
	}
	if string(content) != "{}" {
		t.Errorf("expected '{}', got '%s'", string(content))
	}
}

func TestLoadConfig_UsesExistingDataFile(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := tempDir + "/data"
	dataFilePath := dataDir + "/config.json"

	// Create the data directory and file before LoadConfig
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	existingContent := `{"containers":[]}`
	if err := os.WriteFile(dataFilePath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("failed to write data file: %v", err)
	}

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataFilePath)
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
	}()

	_, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify file content wasn't overwritten
	content, err := os.ReadFile(dataFilePath)
	if err != nil {
		t.Fatalf("failed to read data file: %v", err)
	}
	if string(content) != existingContent {
		t.Errorf("expected '%s', got '%s'", existingContent, string(content))
	}
}

func TestCORSAllowedOrigins(t *testing.T) {
	tests := []struct {
		name     string
		misc     string
		server   string
		expected string
	}{
		{name: "canonical misc key", misc: "https://ui.example", expected: "https://ui.example"},
		{name: "canonical key wins over deprecated alias", misc: "https://ui.example", server: "https://legacy.example", expected: "https://ui.example"},
		{name: "deprecated server key is honoured", server: "https://legacy.example", expected: "https://legacy.example"},
		{name: "default when neither key is set", expected: defaultCORSAllowedOrigins},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			if tt.misc != "" {
				viper.Set("misc.cors_allowed_origins", tt.misc)
			}
			if tt.server != "" {
				viper.Set("server.cors_allowed_origins", tt.server)
			}

			if got := corsAllowedOrigins(); got != tt.expected {
				t.Errorf("corsAllowedOrigins() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestCORSAllowedOrigins_LogsConflictingKeys covers the upgrade hazard: an
// installation still overriding the deprecated key through the environment while
// the configuration file carries the canonical one would otherwise change its
// allow-list without any trace in the logs.
func TestCORSAllowedOrigins_LogsConflictingKeys(t *testing.T) {
	tests := []struct {
		name     string
		misc     string
		server   string
		expected string
		wantWarn bool
	}{
		{name: "different values warn and keep the canonical key", misc: "https://ui.example", server: "*", expected: "https://ui.example", wantWarn: true},
		{name: "identical values do not warn", misc: "https://ui.example", server: "https://ui.example", expected: "https://ui.example"},
		{name: "deprecated key alone warns", server: "https://legacy.example", expected: "https://legacy.example", wantWarn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			original := logger.Logger.Out
			logger.Logger.SetOutput(&buf)
			t.Cleanup(func() { logger.Logger.SetOutput(original) })

			viper.Reset()
			t.Cleanup(viper.Reset)
			if tt.misc != "" {
				viper.Set("misc.cors_allowed_origins", tt.misc)
			}
			if tt.server != "" {
				viper.Set("server.cors_allowed_origins", tt.server)
			}

			if got := corsAllowedOrigins(); got != tt.expected {
				t.Errorf("corsAllowedOrigins() = %q, want %q", got, tt.expected)
			}

			logged := buf.String()
			if !tt.wantWarn {
				if strings.Contains(logged, logrus.WarnLevel.String()) {
					t.Errorf("expected no warning, got %q", logged)
				}
				return
			}

			if !strings.Contains(logged, logrus.WarnLevel.String()) {
				t.Fatalf("expected a warning, got %q", logged)
			}
			if !strings.Contains(logged, "server.cors_allowed_origins") {
				t.Errorf("expected the warning to name the deprecated key, got %q", logged)
			}
		})
	}
}

func TestLoadConfig_CORSAllowedOriginsFromEnv(t *testing.T) {
	tempDir := t.TempDir()
	dataFilePath := tempDir + "/data/config.json"

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataFilePath)
	_ = os.Setenv("GO_SPIN_MISC_CORS_ALLOWED_ORIGINS", "https://ui.example")
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
		_ = os.Unsetenv("GO_SPIN_MISC_CORS_ALLOWED_ORIGINS")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Server.CORSAllowedOrigins != "https://ui.example" {
		t.Errorf("expected CORS origins from env, got %q", cfg.Server.CORSAllowedOrigins)
	}
}

func TestGhostboardConfig_WithDefaults(t *testing.T) {
	defaults := DefaultGhostboardConfig()

	empty := GhostboardConfig{}.WithDefaults()
	if empty.ClearDelay != defaults.ClearDelay {
		t.Errorf("ClearDelay = %v, want %v", empty.ClearDelay, defaults.ClearDelay)
	}
	if empty.MaxTextBytes != defaults.MaxTextBytes {
		t.Errorf("MaxTextBytes = %d, want %d", empty.MaxTextBytes, defaults.MaxTextBytes)
	}
	if empty.PersistInterval != defaults.PersistInterval {
		t.Errorf("PersistInterval = %v, want %v", empty.PersistInterval, defaults.PersistInterval)
	}
	if empty.HeartbeatInterval != defaults.HeartbeatInterval {
		t.Errorf("HeartbeatInterval = %v, want %v", empty.HeartbeatInterval, defaults.HeartbeatInterval)
	}
	if empty.SessionTTL != defaults.SessionTTL {
		t.Errorf("SessionTTL = %v, want %v", empty.SessionTTL, defaults.SessionTTL)
	}
	if empty.PINMaxAttempts != defaults.PINMaxAttempts {
		t.Errorf("PINMaxAttempts = %d, want %d", empty.PINMaxAttempts, defaults.PINMaxAttempts)
	}
	if empty.PINLockout != defaults.PINLockout {
		t.Errorf("PINLockout = %v, want %v", empty.PINLockout, defaults.PINLockout)
	}
	if empty.FilePath != "" {
		t.Errorf("FilePath = %q, want empty (persistence disabled)", empty.FilePath)
	}
	if len(empty.PrivateBoards) != 0 {
		t.Errorf("PrivateBoards = %v, want none (every board is public by default)", empty.PrivateBoards)
	}

	custom := GhostboardConfig{
		FilePath:          "/tmp/boards.json",
		ClearDelay:        time.Minute,
		MaxTextBytes:      2048,
		PersistInterval:   time.Second,
		HeartbeatInterval: 5 * time.Second,
		SessionTTL:        time.Hour,
		PINMaxAttempts:    2,
		PINLockout:        30 * time.Second,
	}
	if got := custom.WithDefaults(); !reflect.DeepEqual(got, custom) {
		t.Errorf("WithDefaults() changed a fully configured value: %+v", got)
	}
}

func TestConfig_Validate_InvalidGhostboard(t *testing.T) {
	base := func() *Config {
		return &Config{
			Server: ServerConfig{
				Port:            8080,
				ReadTimeout:     10 * time.Second,
				WriteTimeout:    10 * time.Second,
				IdleTimeout:     120 * time.Second,
				ShutDownTimeout: 5 * time.Second,
				RequestTimeout:  1000 * time.Millisecond,
			},
			Data: DataConfig{
				FilePath:                 "/tmp/config.json",
				PersistInterval:          5 * time.Second,
				SchedulingPoll:           30 * time.Second,
				RefreshIntervalSecs:      60,
				StatsRefreshIntervalSecs: 120,
				ActionsTimeoutSeconds:    30,
			},
			Misc: MiscConfig{
				SchedulingTZ: "Local",
			},
			Ghostboard: DefaultGhostboardConfig(),
		}
	}

	tests := []struct {
		mutate func(*GhostboardConfig)
		name   string
	}{
		{name: "clear delay", mutate: func(g *GhostboardConfig) { g.ClearDelay = 0 }},
		{name: "max text bytes", mutate: func(g *GhostboardConfig) { g.MaxTextBytes = 0 }},
		{name: "persist interval", mutate: func(g *GhostboardConfig) { g.PersistInterval = 0 }},
		{name: "heartbeat interval", mutate: func(g *GhostboardConfig) { g.HeartbeatInterval = 0 }},
		{name: "session ttl", mutate: func(g *GhostboardConfig) { g.SessionTTL = 0 }},
		{name: "pin max attempts", mutate: func(g *GhostboardConfig) { g.PINMaxAttempts = 0 }},
		{name: "pin lockout", mutate: func(g *GhostboardConfig) { g.PINLockout = 0 }},
		{name: "invalid private board name", mutate: func(g *GhostboardConfig) { g.PrivateBoards = []string{"not a board"} }},
		{name: "private boards without a pin", mutate: func(g *GhostboardConfig) { g.PrivateBoards = []string{"lavoro"} }},
		{name: "pin without a secret", mutate: func(g *GhostboardConfig) { g.PINHMAC = strings.Repeat("a", 64) }},
		{name: "pin with a short secret", mutate: func(g *GhostboardConfig) {
			g.PINHMAC = strings.Repeat("a", 64)
			g.PINSecret = "short"
		}},
		{name: "pin hmac not hex", mutate: func(g *GhostboardConfig) {
			g.PINHMAC = "not-hex"
			g.PINSecret = "0123456789abcdef"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(&cfg.Ghostboard)
			if err := cfg.validate(); err == nil {
				t.Fatal("expected a validation error, got nil")
			}
		})
	}
}

func TestConfig_Validate_ValidPIN(t *testing.T) {
	digest, err := ghostboard.PINDigest("0123456789abcdef", "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}

	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutDownTimeout: 5 * time.Second,
			RequestTimeout:  1000 * time.Millisecond,
		},
		Data: DataConfig{
			FilePath:                 "/tmp/config.json",
			PersistInterval:          5 * time.Second,
			SchedulingPoll:           30 * time.Second,
			RefreshIntervalSecs:      60,
			StatsRefreshIntervalSecs: 120,
			ActionsTimeoutSeconds:    30,
		},
		Misc: MiscConfig{SchedulingTZ: "Local"},
		Ghostboard: GhostboardConfig{
			ClearDelay:        time.Minute,
			MaxTextBytes:      1024,
			PersistInterval:   time.Second,
			HeartbeatInterval: 10 * time.Second,
			PINHMAC:           digest,
			PINSecret:         "0123456789abcdef",
			PrivateBoards:     []string{"lavoro", "note"},
			SessionTTL:        time.Hour,
			PINMaxAttempts:    3,
			PINLockout:        time.Minute,
		},
	}

	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
}

func TestLoadConfig_GhostboardDefaults(t *testing.T) {
	tempDir := t.TempDir()
	dataFilePath := tempDir + "/data/config.json"

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataFilePath)
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	defaults := DefaultGhostboardConfig()
	if cfg.Ghostboard.ClearDelay != defaults.ClearDelay {
		t.Errorf("ClearDelay = %v, want %v", cfg.Ghostboard.ClearDelay, defaults.ClearDelay)
	}
	if cfg.Ghostboard.MaxTextBytes != defaults.MaxTextBytes {
		t.Errorf("MaxTextBytes = %d, want %d", cfg.Ghostboard.MaxTextBytes, defaults.MaxTextBytes)
	}
	if cfg.Ghostboard.PersistInterval != defaults.PersistInterval {
		t.Errorf("PersistInterval = %v, want %v", cfg.Ghostboard.PersistInterval, defaults.PersistInterval)
	}
	if cfg.Ghostboard.HeartbeatInterval != defaults.HeartbeatInterval {
		t.Errorf("HeartbeatInterval = %v, want %v", cfg.Ghostboard.HeartbeatInterval, defaults.HeartbeatInterval)
	}
	if want := tempDir + "/data/ghostboard.json"; cfg.Ghostboard.FilePath != want {
		t.Errorf("FilePath = %q, want %q", cfg.Ghostboard.FilePath, want)
	}
	if cfg.Ghostboard.SessionTTL != defaults.SessionTTL {
		t.Errorf("SessionTTL = %v, want %v", cfg.Ghostboard.SessionTTL, defaults.SessionTTL)
	}
	if cfg.Ghostboard.PINMaxAttempts != defaults.PINMaxAttempts {
		t.Errorf("PINMaxAttempts = %d, want %d", cfg.Ghostboard.PINMaxAttempts, defaults.PINMaxAttempts)
	}
	if cfg.Ghostboard.PINLockout != defaults.PINLockout {
		t.Errorf("PINLockout = %v, want %v", cfg.Ghostboard.PINLockout, defaults.PINLockout)
	}
	if len(cfg.Ghostboard.PrivateBoards) != 0 {
		t.Errorf("PrivateBoards = %v, want none", cfg.Ghostboard.PrivateBoards)
	}
}

func TestLoadConfig_GhostboardEnvOverrides(t *testing.T) {
	tempDir := t.TempDir()
	dataFilePath := tempDir + "/data/config.json"
	boardFilePath := tempDir + "/boards/custom.json"

	_ = os.Setenv("GO_SPIN_CONFIG_PATH", tempDir)
	_ = os.Setenv("GO_SPIN_DATA_FILE_PATH", dataFilePath)
	_ = os.Setenv("GO_SPIN_GHOSTBOARD_FILE_PATH", boardFilePath)
	_ = os.Setenv("GO_SPIN_GHOSTBOARD_MAX_TEXT_BYTES", "2048")
	defer func() {
		_ = os.Unsetenv("GO_SPIN_CONFIG_PATH")
		_ = os.Unsetenv("GO_SPIN_DATA_FILE_PATH")
		_ = os.Unsetenv("GO_SPIN_GHOSTBOARD_FILE_PATH")
		_ = os.Unsetenv("GO_SPIN_GHOSTBOARD_MAX_TEXT_BYTES")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Ghostboard.FilePath != boardFilePath {
		t.Errorf("FilePath = %q, want %q", cfg.Ghostboard.FilePath, boardFilePath)
	}
	if cfg.Ghostboard.MaxTextBytes != 2048 {
		t.Errorf("MaxTextBytes = %d, want 2048", cfg.Ghostboard.MaxTextBytes)
	}
}
