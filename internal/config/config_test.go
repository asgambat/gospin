package config

import (
	"os"
	"testing"
	"time"
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
		},
		Misc: MiscConfig{
			GinMode:      "release",
			SchedulingTZ: "Local",
			RuntimeType:  "docker",
		},
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
				},
				Data: DataConfig{
					FilePath:                 "/tmp/config.json",
					PersistInterval:          5 * time.Second,
					SchedulingPoll:           30 * time.Second,
					RefreshIntervalSecs:      60,
					StatsRefreshIntervalSecs: 120,
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
				},
				Misc: MiscConfig{
					SchedulingTZ: tz,
				},
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
		},
		Misc: MiscConfig{
			SchedulingTZ: "",
		},
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
