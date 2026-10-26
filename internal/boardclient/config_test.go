package boardclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// noEnv ignores the environment so a test never depends on the host.
func noEnv(string) string {
	return ""
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

func TestLoadDefaults(t *testing.T) {
	home := t.TempDir()
	exeDir := t.TempDir()

	cfg, err := Load(LoadOptions{
		HomeDir:        home,
		ExecutablePath: filepath.Join(exeDir, "gb-cli"),
		Getenv:         noEnv,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != DefaultURL {
		t.Fatalf("URL %q, want %q", cfg.URL, DefaultURL)
	}
	if cfg.Board != "default" {
		t.Fatalf("board %q, want %q", cfg.Board, "default")
	}
	if cfg.Timeout != DefaultTimeout {
		t.Fatalf("timeout %s, want %s", cfg.Timeout, DefaultTimeout)
	}
	if cfg.FilePath != "" {
		t.Fatalf("file path %q, want none", cfg.FilePath)
	}
}

func TestLoadUserConfigFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	writeConfig(t, path, "url: http://board.example.com:9000/\nboard: lavoro\ntimeout_secs: 7\n")

	cfg, err := Load(LoadOptions{HomeDir: home, Getenv: noEnv})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "http://board.example.com:9000/" {
		t.Fatalf("URL %q", cfg.URL)
	}
	if cfg.Board != "lavoro" {
		t.Fatalf("board %q", cfg.Board)
	}
	if cfg.Timeout != 7*time.Second {
		t.Fatalf("timeout %s", cfg.Timeout)
	}
	if cfg.FilePath != path {
		t.Fatalf("file path %q, want %q", cfg.FilePath, path)
	}
}

func TestLoadExecutableConfigFileWins(t *testing.T) {
	home := t.TempDir()
	exeDir := t.TempDir()

	writeConfig(t, filepath.Join(home, ".config", "ghostboard", "config.yaml"), "url: http://home.example.com\n")
	writeConfig(t, filepath.Join(exeDir, ConfigFileName), "url: http://portable.example.com\n")

	cfg, err := Load(LoadOptions{
		HomeDir:        home,
		ExecutablePath: filepath.Join(exeDir, "gb-cli"),
		Getenv:         noEnv,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "http://portable.example.com" {
		t.Fatalf("URL %q, want the file next to the executable", cfg.URL)
	}
}

func TestLoadExplicitConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	writeConfig(t, path, "url: http://explicit.example.com\n")

	cfg, err := Load(LoadOptions{
		ConfigPath: path,
		HomeDir:    t.TempDir(),
		Getenv:     noEnv,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "http://explicit.example.com" {
		t.Fatalf("URL %q", cfg.URL)
	}
}

func TestLoadExplicitConfigFileMissing(t *testing.T) {
	_, err := Load(LoadOptions{
		ConfigPath: filepath.Join(t.TempDir(), "missing.yaml"),
		HomeDir:    t.TempDir(),
		Getenv:     noEnv,
	})
	if err == nil {
		t.Fatal("expected an error for a missing explicit config file")
	}
	if !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("error %q does not name the file", err)
	}
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, filepath.Join(home, ".config", "ghostboard", "config.yaml"),
		"url: http://file.example.com\nboard: lavoro\ntimeout_secs: 5\n")

	env := func(key string) string {
		switch key {
		case EnvURL:
			return "http://env.example.com"
		case EnvTimeout:
			return "12"
		default:
			return ""
		}
	}

	cfg, err := Load(LoadOptions{HomeDir: home, Getenv: env})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "http://env.example.com" {
		t.Fatalf("URL %q, want the environment value", cfg.URL)
	}
	if cfg.Timeout != 12*time.Second {
		t.Fatalf("timeout %s, want 12s", cfg.Timeout)
	}
	if cfg.Board != "lavoro" {
		t.Fatalf("board %q, want the file value", cfg.Board)
	}
}

func TestLoadDefersValueValidation(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, filepath.Join(home, ".config", "ghostboard", "config.yaml"),
		"url: \"https://gospin.lan.asga.uk/ui/dashboard/\"\nboard: bad/name\ntimeout_secs: -3\n")

	cfg, err := Load(LoadOptions{HomeDir: home, Getenv: noEnv})
	if err != nil {
		t.Fatalf("Load must leave the values to Validate, got: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected the effective configuration to be invalid")
	}
}

func TestLoadRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		env     func(string) string
		wants   string
	}{
		{
			name:    "malformed yaml",
			content: "url: [unclosed\n",
			wants:   "reading config file",
		},
		{
			name:    "invalid timeout env",
			content: "url: http://example.com\n",
			env: func(key string) string {
				if key == EnvTimeout {
					return "soon"
				}
				return ""
			},
			wants: EnvTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			writeConfig(t, filepath.Join(home, ".config", "ghostboard", "config.yaml"), test.content)

			env := noEnv
			if test.env != nil {
				env = test.env
			}

			_, err := Load(LoadOptions{HomeDir: home, Getenv: env})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.wants) {
				t.Fatalf("error %q does not contain %q", err, test.wants)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name  string
		wants string
		cfg   Config
	}{
		{
			name: "valid",
			cfg:  Config{URL: "http://example.com", Board: "default", Timeout: time.Second},
		},
		{
			name:  "dashboard page",
			cfg:   Config{URL: "https://gospin.lan.asga.uk/ui/dashboard/", Board: "default", Timeout: time.Second},
			wants: "GoSpin page",
		},
		{
			name:  "unsupported scheme",
			cfg:   Config{URL: "ftp://example.com", Board: "default", Timeout: time.Second},
			wants: "scheme must be http or https",
		},
		{
			name:  "empty url",
			cfg:   Config{URL: "", Board: "default", Timeout: time.Second},
			wants: "server URL is empty",
		},
		{
			name:  "invalid board",
			cfg:   Config{URL: "http://example.com", Board: "bad/name", Timeout: time.Second},
			wants: "invalid board name",
		},
		{
			name:  "non positive timeout",
			cfg:   Config{URL: "http://example.com", Board: "default", Timeout: 0},
			wants: "timeout must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cfg.Validate()
			if test.wants == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.wants) {
				t.Fatalf("error %q does not contain %q", err, test.wants)
			}
		})
	}
}

func TestLoadFromXDGConfigHome(t *testing.T) {
	xdg := t.TempDir()
	path := filepath.Join(xdg, "ghostboard", ConfigFileName)
	writeConfig(t, path, "url: http://xdg.example.com\nboard: lavoro\n")

	cfg, err := Load(LoadOptions{
		HomeDir:       t.TempDir(),
		XDGConfigHome: xdg,
		Getenv:        noEnv,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "http://xdg.example.com" {
		t.Fatalf("URL %q, want the XDG configuration", cfg.URL)
	}
	if cfg.FilePath != path {
		t.Fatalf("file path %q, want %q", cfg.FilePath, path)
	}
}

func TestLoadSkipsFiles(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, filepath.Join(home, ".config", "ghostboard", ConfigFileName), "url: [unclosed\n")

	cfg, err := Load(LoadOptions{HomeDir: home, Getenv: noEnv, SkipFile: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != DefaultURL {
		t.Fatalf("URL %q, want the default", cfg.URL)
	}
	if cfg.FilePath != "" {
		t.Fatalf("file path %q, want none", cfg.FilePath)
	}
}

func TestConfigPaths(t *testing.T) {
	home := t.TempDir()
	exeDir := t.TempDir()

	paths := ConfigPaths(LoadOptions{
		HomeDir:        home,
		ExecutablePath: filepath.Join(exeDir, "gb-cli"),
	})
	if len(paths) != 2 {
		t.Fatalf("paths %v, want two candidates", paths)
	}
	if paths[0] != filepath.Join(exeDir, ConfigFileName) {
		t.Fatalf("first candidate %q, want the executable directory", paths[0])
	}
	if paths[1] != filepath.Join(home, ".config", "ghostboard", ConfigFileName) {
		t.Fatalf("second candidate %q", paths[1])
	}

	xdgPaths := ConfigPaths(LoadOptions{
		HomeDir:       home,
		XDGConfigHome: "/xdg",
	})
	if len(xdgPaths) != 1 || xdgPaths[0] != filepath.Join("/xdg", "ghostboard", ConfigFileName) {
		t.Fatalf("xdg paths %v", xdgPaths)
	}

	if empty := ConfigPaths(LoadOptions{}); len(empty) != 0 {
		t.Fatalf("paths %v, want none", empty)
	}
}

func TestValidateReturnsABoardNameError(t *testing.T) {
	err := (Config{URL: "http://example.com", Board: "bad/name", Timeout: time.Second}).Validate()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrBoardName) {
		t.Fatalf("error %q is not ErrBoardName", err)
	}
}

func TestLoadReportsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "url: http://board.example.com\ntimeout_sec: 5\nboard_naam: lavoro\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	cfg, err := Load(LoadOptions{ConfigPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Warnings) != 2 {
		t.Fatalf("warnings %v, want one per unknown key", cfg.Warnings)
	}
	if !strings.Contains(cfg.Warnings[0], `"board_naam"`) || !strings.Contains(cfg.Warnings[1], `"timeout_sec"`) {
		t.Fatalf("warnings %v do not name the unknown keys", cfg.Warnings)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Fatalf("timeout %s, want the default: a mistyped key must not change it", cfg.Timeout)
	}
}

func TestLoadRecordsTheEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("url: http://from-file.example.com\ntimeout_secs: 7\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	env := map[string]string{
		EnvURL:     "http://from-env.example.com",
		EnvTimeout: "9",
	}
	cfg, err := Load(LoadOptions{
		ConfigPath: path,
		Getenv:     func(name string) string { return env[name] },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "http://from-env.example.com" || cfg.Timeout != 9*time.Second {
		t.Fatalf("config %+v does not carry the environment values", cfg)
	}
	if len(cfg.EnvOverrides) != 2 || cfg.EnvOverrides[0] != EnvURL || cfg.EnvOverrides[1] != EnvTimeout {
		t.Fatalf("overrides %v, want both variables in application order", cfg.EnvOverrides)
	}
	if len(cfg.Warnings) != 0 {
		t.Fatalf("warnings %v, want none", cfg.Warnings)
	}
}
