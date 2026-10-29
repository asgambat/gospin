package boardclient

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/asgambat/gospin/internal/ghostboard"
)

const (
	// DefaultURL is the address of the local GoSpin server used when neither
	// the configuration file nor the environment overrides it.
	DefaultURL = "http://localhost:8084"

	// DefaultTimeout bounds a single CLI request, leaving room for a board at
	// the maximum configured text size.
	DefaultTimeout = 30 * time.Second

	// ConfigFileName is the configuration file the CLI looks up next to its own
	// executable and inside the per-user configuration directory.
	ConfigFileName = "config.yaml"

	// userConfigDirName is the directory holding ConfigFileName inside the
	// per-user configuration directory ($XDG_CONFIG_HOME or <home>/.config).
	userConfigDirName = "ghostboard"

	// homeConfigSubdir is the per-user configuration directory used when
	// XDG_CONFIG_HOME is not set.
	homeConfigSubdir = ".config"

	// EnvURL, EnvTimeout and EnvPIN override the configuration file. They share
	// the GO_SPIN prefix of the server settings.
	EnvURL     = "GO_SPIN_BOARD_URL"
	EnvTimeout = "GO_SPIN_BOARD_TIMEOUT_SECS"
	EnvPIN     = "GO_SPIN_BOARD_PIN"
)

// Config is the resolved configuration of the CLI, before the command-line
// flags are applied on top of it.
type Config struct {
	// URL is the base address of the GoSpin server.
	URL string
	// Board is the board used when a command does not name one.
	Board string
	// FilePath is the configuration file that was read, empty when none was.
	FilePath string
	// PIN unlocks a board protected by the shared ghostboard PIN.
	PIN string
	// Warnings lists what the configuration file contains but the CLI does
	// not understand, such as a mistyped key. Reporting them is the whole
	// point of the config command: a silently ignored key looks like a
	// default that was never overridden.
	Warnings []string
	// EnvOverrides names the environment variables that replaced a file value.
	EnvOverrides []string
	// Timeout bounds a single request.
	Timeout time.Duration
}

// LoadOptions tells Load where to look. Every field is optional: an empty
// ConfigPath searches the standard locations, an empty HomeDir or
// ExecutablePath skips that location.
type LoadOptions struct {
	// Getenv reads the environment; os.Getenv is used when nil.
	Getenv func(string) string
	// ConfigPath is an explicitly requested configuration file.
	ConfigPath string
	// HomeDir is the user's home directory.
	HomeDir string
	// ExecutablePath is the CLI executable, whose directory is searched first.
	ExecutablePath string
	// XDGConfigHome is $XDG_CONFIG_HOME; when set it replaces <home>/.config.
	XDGConfigHome string
	// SkipFile ignores every configuration file, including ConfigPath.
	SkipFile bool
}

// Load resolves the CLI configuration. Precedence, lowest first: built-in
// defaults, configuration file, GO_SPIN_BOARD_* environment variables. The
// command-line flags are applied on top of the result by the caller, so the
// values are validated afterwards by Validate: a file that names a wrong URL
// must not break a command that overrides it with a flag.
func Load(opts LoadOptions) (Config, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		URL:     DefaultURL,
		Board:   ghostboard.DefaultBoard,
		Timeout: DefaultTimeout,
	}

	path, err := resolveConfigPath(opts)
	if err != nil {
		return Config{}, err
	}
	if path != "" {
		warnings, err := loadFile(path, &cfg)
		if err != nil {
			return Config{}, err
		}
		cfg.FilePath = path
		cfg.Warnings = warnings
	}

	if value := strings.TrimSpace(getenv(EnvURL)); value != "" {
		cfg.URL = value
		cfg.EnvOverrides = append(cfg.EnvOverrides, EnvURL)
	}
	if value := strings.TrimSpace(getenv(EnvPIN)); value != "" {
		cfg.PIN = value
		cfg.EnvOverrides = append(cfg.EnvOverrides, EnvPIN)
	}
	if value := strings.TrimSpace(getenv(EnvTimeout)); value != "" {
		secs, convErr := strconv.Atoi(value)
		if convErr != nil || secs <= 0 {
			return Config{}, fmt.Errorf("%s must be a positive number of seconds, got %q", EnvTimeout, value)
		}
		cfg.Timeout = time.Duration(secs) * time.Second
		cfg.EnvOverrides = append(cfg.EnvOverrides, EnvTimeout)
	}

	return cfg, nil
}

// Validate checks the configuration that will actually be used. It is called
// once the command-line flags have been applied, so a value from the file can
// always be corrected on the command line.
func (c Config) Validate() error {
	if _, err := NormalizeURL(c.URL); err != nil {
		return err
	}
	if err := ghostboard.ValidateBoardName(c.Board); err != nil {
		return &BoardNameError{Name: c.Board}
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("request timeout must be positive, got %s", c.Timeout)
	}
	if c.PIN != "" {
		if err := ghostboard.ValidatePIN(c.PIN); err != nil {
			return err
		}
	}
	return nil
}

// ConfigPaths returns the configuration files the CLI looks for, most specific
// first: next to the executable (portable installs), then the per-user file. It
// never touches the filesystem.
func ConfigPaths(opts LoadOptions) []string {
	candidates := make([]string, 0, 2)
	if dir := executableDir(opts.ExecutablePath); dir != "" {
		candidates = append(candidates, filepath.Join(dir, ConfigFileName))
	}
	if path := userConfigPath(opts); path != "" {
		candidates = append(candidates, path)
	}
	return candidates
}

// userConfigPath returns the per-user configuration file, following
// XDG_CONFIG_HOME when it is set.
func userConfigPath(opts LoadOptions) string {
	if opts.XDGConfigHome != "" {
		return filepath.Join(opts.XDGConfigHome, userConfigDirName, ConfigFileName)
	}
	if opts.HomeDir != "" {
		return filepath.Join(opts.HomeDir, homeConfigSubdir, userConfigDirName, ConfigFileName)
	}
	return ""
}

// resolveConfigPath returns the configuration file to read, or an empty string
// when none exists. An explicitly requested file must be readable.
func resolveConfigPath(opts LoadOptions) (string, error) {
	if opts.SkipFile {
		return "", nil
	}

	if opts.ConfigPath != "" {
		if _, err := os.Stat(opts.ConfigPath); err != nil {
			return "", fmt.Errorf("config file %s: %w", opts.ConfigPath, err)
		}
		return opts.ConfigPath, nil
	}

	for _, candidate := range ConfigPaths(opts) {
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			continue
		case err == nil:
			return candidate, nil
		case errors.Is(err, fs.ErrNotExist):
			continue
		default:
			return "", fmt.Errorf("checking config file %s: %w", candidate, err)
		}
	}
	return "", nil
}

func executableDir(executablePath string) string {
	if executablePath == "" {
		return ""
	}
	return filepath.Dir(executablePath)
}

// knownConfigKeys are the keys a configuration file may carry.
var knownConfigKeys = []string{"url", "board", "pin", "timeout_secs"}

// loadFile applies the keys of a configuration file on top of cfg. Absent keys
// keep their current value; the values themselves are validated later, on the
// effective configuration, so a flag can override a mistyped file entry. The
// returned warnings name the keys the CLI does not understand.
func loadFile(path string, cfg *Config) ([]string, error) {
	reader := viper.New()
	reader.SetConfigFile(path)
	if err := reader.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	if reader.InConfig("url") {
		cfg.URL = strings.TrimSpace(reader.GetString("url"))
	}
	if reader.InConfig("board") {
		cfg.Board = strings.TrimSpace(reader.GetString("board"))
	}
	if reader.InConfig("timeout_secs") {
		cfg.Timeout = time.Duration(reader.GetInt("timeout_secs")) * time.Second
	}
	if reader.InConfig("pin") {
		cfg.PIN = strings.TrimSpace(reader.GetString("pin"))
	}

	return unknownKeys(reader, path), nil
}

// unknownKeys names the configuration entries the CLI does not read, so a
// mistyped key is reported instead of silently keeping a default.
func unknownKeys(reader *viper.Viper, path string) []string {
	settings := reader.AllSettings()
	names := make([]string, 0, len(settings))
	for name := range settings {
		names = append(names, name)
	}
	sort.Strings(names)

	var warnings []string
	for _, name := range names {
		if slices.Contains(knownConfigKeys, name) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("unknown key %q in %s: known keys are url, board, pin and timeout_secs", name, path))
	}
	return warnings
}
