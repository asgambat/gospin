package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/logger"
)

// emptyDataFile writes a valid, empty data document and returns its path: the
// wiring tests care that newApp builds the container, not about the contents of
// an installation.
func emptyDataFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("writing the data file: %v", err)
	}
	return path
}

func TestApplyLogLevel(t *testing.T) {
	tests := []struct {
		level string
		want  logrus.Level
	}{
		{level: "debug", want: logrus.DebugLevel},
		{level: "warn", want: logrus.WarnLevel},
		// A typo does not stop the server: it falls back to info.
		{level: "verbose-ish", want: logrus.InfoLevel},
	}

	previous := logger.Logger.GetLevel()
	t.Cleanup(func() { logger.Logger.SetLevel(previous) })

	for _, tt := range tests {
		if got := applyLogLevel(tt.level); got != tt.want {
			t.Errorf("applyLogLevel(%q) = %v, want %v", tt.level, got, tt.want)
		}
		if got := logger.Logger.GetLevel(); got != tt.want {
			t.Errorf("logger level after applyLogLevel(%q) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

// syncBuffer is a bytes.Buffer safe to share between the test goroutine and
// the goroutines logrus spawns for its writer shim: both read and write here.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// captureLogs redirects the application logger into a buffer for the duration
// of a test, so a log assertion does not depend on the process output.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()

	buffer := &syncBuffer{}
	previous := logger.Logger.Out
	logger.Logger.SetOutput(buffer)
	t.Cleanup(func() { logger.Logger.SetOutput(previous) })

	return buffer
}

// waitForText polls the buffer: the logrus writer hands the bytes to the logger
// through a pipe, so the line arrives shortly after the write, not inside it.
func waitForText(t *testing.T, buffer *syncBuffer, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buffer.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the log output never contained %q: %q", want, buffer.String())
}

func TestLogStartupAnnouncesTheBuildAndBothPorts(t *testing.T) {
	logs := captureLogs(t)

	logStartup(&config.Config{Server: config.ServerConfig{Port: 8084, WaitingServerPort: 8085}})

	waitForText(t, logs, "starting gospin")
	waitForText(t, logs, "Waiting server will run on port: 8085")
	waitForText(t, logs, "App will run on port: 8084")
}

func TestConfigureGinSendsGinOutputToTheApplicationLogger(t *testing.T) {
	previousMode := gin.Mode()
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	t.Cleanup(func() {
		gin.SetMode(previousMode)
		gin.DefaultWriter = previousWriter
		gin.DefaultErrorWriter = previousErrorWriter
	})
	logs := captureLogs(t)

	configureGin(gin.ReleaseMode)

	if got := gin.Mode(); got != gin.ReleaseMode {
		t.Errorf("gin mode = %q, want %q", got, gin.ReleaseMode)
	}
	if gin.DefaultWriter == nil || gin.DefaultErrorWriter == nil {
		t.Fatal("gin kept no writer for its own output")
	}

	if _, err := fmt.Fprintln(gin.DefaultWriter, "gin-says-hello"); err != nil {
		t.Fatalf("writing to gin.DefaultWriter: %v", err)
	}
	if _, err := fmt.Fprintln(gin.DefaultErrorWriter, "gin-says-error"); err != nil {
		t.Fatalf("writing to gin.DefaultErrorWriter: %v", err)
	}

	waitForText(t, logs, "gin-says-hello")
	waitForText(t, logs, "gin-says-error")
}

func TestNewAppBuildsTheContainer(t *testing.T) {
	cfg := &config.Config{
		Data: config.DataConfig{FilePath: emptyDataFile(t)},
		Misc: config.MiscConfig{RuntimeType: "memory"},
	}

	appCtx, err := newApp(cfg)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	defer appCtx.Shutdown()

	if appCtx.Config != cfg {
		t.Error("the application does not carry the configuration it was built from")
	}
	if appCtx.Runtime == nil || appCtx.Cache == nil || appCtx.Repo == nil {
		t.Error("the application is missing a dependency")
	}
	if appCtx.Ghostboard == nil {
		t.Error("the ghostboard store was not created")
	}
}

func TestNewAppReportsTheFailingStep(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr string
	}{
		{
			name:    "repository",
			cfg:     &config.Config{Data: config.DataConfig{FilePath: ""}},
			wantErr: "cannot init repository",
		},
		{
			name: "data file",
			cfg: &config.Config{Data: config.DataConfig{
				FilePath: filepath.Join(t.TempDir(), "broken.json"),
			}},
			wantErr: "cannot load data file",
		},
		{
			name: "runtime",
			cfg: &config.Config{
				Data: config.DataConfig{FilePath: emptyDataFile(t)},
				Misc: config.MiscConfig{RuntimeType: "not-a-runtime"},
			},
			wantErr: "cannot init runtime",
		},
	}

	// The broken data file is written here so the table stays declarative.
	if err := os.WriteFile(tests[1].cfg.Data.FilePath, []byte("{"), 0o600); err != nil {
		t.Fatalf("writing the broken data file: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx, err := newApp(tt.cfg)
			if err == nil {
				appCtx.Shutdown()
				t.Fatal("newApp succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to name %q", err, tt.wantErr)
			}
			if appCtx != nil {
				t.Error("a failed wiring returned an application")
			}
		})
	}
}

func TestCreateGraceHttpServerAppliesTheConfiguredTimeouts(t *testing.T) {
	ctx := context.Background()
	cfg := config.ServerConfig{
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    4 * time.Second,
		IdleTimeout:     5 * time.Second,
		ShutDownTimeout: 2 * time.Second,
	}

	srv := createGraceHttpServer(ctx, "test-server", cfg, gin.New())

	if srv.Handler == nil {
		t.Fatal("the server has no handler")
	}
	if srv.ReadTimeout != cfg.ReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", srv.ReadTimeout, cfg.ReadTimeout)
	}
	if srv.WriteTimeout != cfg.WriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", srv.WriteTimeout, cfg.WriteTimeout)
	}
	if srv.IdleTimeout != cfg.IdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, cfg.IdleTimeout)
	}
	if srv.ErrorLog == nil {
		t.Error("the server has no error log")
	}
	if srv.BaseContext == nil {
		t.Fatal("the server has no base context")
	}
	if got := srv.BaseContext(nil); got != ctx {
		t.Error("the base context is not the application context")
	}
}

func TestCreateWaitingServerUsesTheApplicationContext(t *testing.T) {
	appCtx := newTestAppCtx(newMockRuntime(), newTestStore())

	srv := createWaitingServer(appCtx, logrus.New())

	if srv.Handler == nil {
		t.Fatal("the waiting server has no handler")
	}
	if got := srv.BaseContext(nil); got != appCtx.BaseCtx {
		t.Error("the waiting server does not carry the application context")
	}
}

// TestStartWaitingServerReportsABindFailure holds the configured port, so the
// server cannot start: the failure has to reach the caller instead of leaving a
// silent second server behind.
func TestStartWaitingServerReportsABindFailure(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("holding a port: %v", err)
	}
	defer func() {
		if cerr := listener.Close(); cerr != nil {
			t.Errorf("closing the held listener: %v", cerr)
		}
	}()

	appCtx := newTestAppCtx(newMockRuntime(), newTestStore())
	appCtx.Config.Server.WaitingServerPort = listener.Addr().(*net.TCPAddr).Port

	srv, done := startWaitingServer(appCtx, appCtx.Config.Server, logrus.New())
	if srv == nil {
		t.Fatal("startWaitingServer returned no server")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("startWaitingServer reported no error for a port already in use")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting server never reported the bind failure")
	}
}
