package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/api/controller"
	"github.com/asgambat/gospin/internal/api/middleware"
	route "github.com/asgambat/gospin/internal/api/route"
	appctx "github.com/asgambat/gospin/internal/app"
	"github.com/asgambat/gospin/internal/cache"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/repository"
	"github.com/asgambat/gospin/internal/runtime"
	"github.com/asgambat/gospin/internal/version"

	"github.com/enrichman/httpgrace"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.WithComponent("main").Fatalf("configuration error: %v", err)
	}

	applyLogLevel(cfg.Misc.LogLevel)
	logStartup(cfg)

	app, err := newApp(cfg)
	if err != nil {
		logger.WithComponent("main").Fatal(err)
	}
	defer app.Shutdown()

	app.StartWatchers()

	configureGin(cfg.Misc.GinMode)

	// Setup and start the secondary waiting server
	startWaitingServer(app, cfg.Server, logger.Logger)

	//setup main server routes and start it!
	r := route.SetupRoutes(app, logger.Logger)
	mainSrv := createGraceHttpServer(app.BaseCtx, "main-server", app.Config.Server, r)

	if err := mainSrv.ListenAndServe(fmt.Sprintf(":%d", cfg.Server.Port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.WithComponent("main").Fatal(err)
	}
}

// applyLogLevel sets the process-wide log level from the configuration. An
// unknown value does not stop the server: it falls back to info with a warning,
// so a typo in one key does not take the dashboard down.
func applyLogLevel(level string) logrus.Level {
	parsed, err := logrus.ParseLevel(level)
	if err != nil {
		logger.WithComponent("main").Warnf("invalid log level '%s', using 'info': %v", level, err)
		parsed = logrus.InfoLevel
	}

	logger.Logger.SetLevel(parsed)
	logger.WithComponent("main").Debugf("log level set to: %s", parsed.String())
	return parsed
}

// logStartup announces the build identity and the two ports. The identity is
// emitted eagerly so it shows up in container logs / `docker logs` output from
// the first line — the complement to the homepage footer summary rendered from
// the /homepage JSON response.
func logStartup(cfg *config.Config) {
	logger.WithComponent("main").Infof("starting gospin %s", version.BuildInfo())
	logger.WithComponent("main").Infof("Waiting server will run on port: %d", cfg.Server.WaitingServerPort)
	logger.WithComponent("main").Infof("App will run on port: %d", cfg.Server.Port)
}

// configureGin applies the gin mode and sends gin's own output to the
// application logger, so access logs and panic traces share the log stream and
// its level instead of writing to stderr unbuffered.
func configureGin(mode string) {
	gin.SetMode(mode)
	gin.DefaultWriter = logger.Logger.Writer()
	gin.DefaultErrorWriter = logger.Logger.Writer()
}

// newApp builds the application container from the configuration: the data
// repository, the in-memory cache, the container runtime and the app that ties
// them together. Each failure names the step that broke, so the fatal message
// the caller logs still says which piece of the wiring failed.
func newApp(cfg *config.Config) (*appctx.App, error) {
	repo, err := repository.NewJSONRepository(cfg.Data.FilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot init repository: %w", err)
	}

	jsonDoc, err := repo.Load(context.Background())
	if err != nil {
		return nil, fmt.Errorf("cannot load data file: %w", err)
	}

	cacheStore := cache.NewStore(*jsonDoc)
	rt, err := runtime.NewRuntimeFromConfig(cfg.Misc.RuntimeType, jsonDoc, cacheStore)
	if err != nil {
		return nil, fmt.Errorf("cannot init runtime: %w", err)
	}

	app, err := appctx.New(cfg, repo, cacheStore, rt)
	if err != nil {
		return nil, fmt.Errorf("cannot init app: %w", err)
	}
	return app, nil
}

// startWaitingServer serves the boot page on its own port, in the background,
// and returns the server with a channel carrying the error it stopped with. A
// server stopped normally (http.ErrServerClosed) reports no error: only a real
// failure, like a port already in use, is logged and returned.
func startWaitingServer(app *appctx.App, cfg config.ServerConfig, log *logrus.Logger) (*httpgrace.Server, <-chan error) {
	srv := createWaitingServer(app, log)
	done := make(chan error, 1)

	go func() {
		err := srv.ListenAndServe(fmt.Sprintf(":%d", cfg.WaitingServerPort))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithField("component", "main").Errorf("Waiting server error: %v", err)
		}
		done <- err
	}()

	return srv, done
}

// createWaitingServer creates a secondary HTTP server dedicated to serving only the waiting page.
// It exposes a single route GET /:name that triggers RuntimeController.WaitingPage.
func createWaitingServer(app *appctx.App, logger *logrus.Logger) *httpgrace.Server {
	return createGraceHttpServer(app.BaseCtx, "waiting-server", app.Config.Server, newWaitingRouter(app, logger))
}

// newWaitingRouter builds the engine of the waiting server: the boot page and
// the readiness endpoint it polls. It carries the same security headers as the
// main server, with the policy that page needs instead of the dashboard one, so
// the second port is not a surface without a CSP.
func newWaitingRouter(app *appctx.App, logger *logrus.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.HoneybadgerMiddleware(logger))
	r.Use(gin.Recovery())
	r.Use(middleware.SecurityHeaders(middleware.SecurityHeaderOptions{
		Enabled:               app.Config.Server.SecurityHeaders.Enabled,
		ContentSecurityPolicy: middleware.WaitingPageContentSecurityPolicy,
		HSTSMaxAge:            app.Config.Server.SecurityHeaders.HSTSMaxAge,
	}))

	// Create RuntimeController for the waiting page
	rc := controller.NewRuntimeController(app)
	cc := controller.NewContainerController(app.BaseCtx, app.Cache, app.Runtime, app.Config.Misc.CosmosBaseUrl, app.Config.Misc.CosmosToken)

	r.GET("/container/:name/ready", cc.Ready)
	r.GET("/:name", rc.WaitingPage)

	return r
}

func createGraceHttpServer(ctx context.Context, name string, serverConfig config.ServerConfig, r *gin.Engine) *httpgrace.Server {
	slogLogger := slog.New(slog.NewTextHandler(logger.Logger.Writer(), nil))

	srv := httpgrace.NewServer(r,
		httpgrace.WithTimeout(serverConfig.ShutDownTimeout),
		httpgrace.WithSignals(syscall.SIGTERM, syscall.SIGINT),
		httpgrace.WithLogger(slogLogger),
		httpgrace.WithBeforeShutdown(func() {
			logger.WithComponent("http").Infof("Shutting down %s server....", name)
		}),
		httpgrace.WithServerOptions(
			httpgrace.WithReadTimeout(serverConfig.ReadTimeout),
			httpgrace.WithWriteTimeout(serverConfig.WriteTimeout),
			httpgrace.WithIdleTimeout(serverConfig.IdleTimeout),
			func(srv *http.Server) {
				srv.BaseContext = func(_ net.Listener) context.Context {
					return ctx
				}
			},
			func(srv *http.Server) {
				srv.ErrorLog = log.New(logger.Logger.Writer(), fmt.Sprintf("[%s] ", name), log.LstdFlags)
			},
		),
	)
	return srv
}
