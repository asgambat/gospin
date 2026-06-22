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

	"github.com/bassista/go_spin/internal/api/controller"
	"github.com/bassista/go_spin/internal/api/middleware"
	route "github.com/bassista/go_spin/internal/api/route"
	appctx "github.com/bassista/go_spin/internal/app"
	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/config"
	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
	"github.com/bassista/go_spin/internal/runtime"
	"github.com/bassista/go_spin/internal/version"

	"github.com/enrichman/httpgrace"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.WithComponent("main").Fatalf("configuration error: %v", err)
	}

	// Set log level from configuration
	logLevel, err := logrus.ParseLevel(cfg.Misc.LogLevel)
	if err != nil {
		logger.WithComponent("main").Warnf("invalid log level '%s', using 'info': %v", cfg.Misc.LogLevel, err)
		logLevel = logrus.InfoLevel
	}
	logger.Logger.SetLevel(logLevel)
	logger.WithComponent("main").Debugf("log level set to: %s", logLevel.String())
	// Emit build identity eagerly so it shows up in container logs /
	// `docker logs` output from the first line — complement to the
	// homepage footer summary rendered via the /homepage JSON response.
	logger.WithComponent("main").Infof("starting gospin %s", version.BuildInfo())
	logger.WithComponent("main").Infof("Waiting server will run on port: %d", cfg.Server.WaitingServerPort)
	logger.WithComponent("main").Infof("App will run on port: %d", cfg.Server.Port)

	repo, err := repository.NewJSONRepository(cfg.Data.FilePath)
	if err != nil {
		logger.WithComponent("main").Fatalf("cannot init repository: %v", err)
	}

	jsonDoc, err := repo.Load(context.Background())
	if err != nil {
		logger.WithComponent("main").Fatalf("cannot load data file: %v", err)
	}

	cacheStore := cache.NewStore(*jsonDoc)
	rt, err := runtime.NewRuntimeFromConfig(cfg.Misc.RuntimeType, jsonDoc)
	if err != nil {
		logger.WithComponent("main").Fatalf("cannot init runtime: %v", err)
	}

	app, err := appctx.New(cfg, repo, cacheStore, rt)
	if err != nil {
		logger.WithComponent("main").Fatalf("cannot init app: %v", err)
	}
	defer app.Shutdown()

	app.StartWatchers()

	gin.SetMode(cfg.Misc.GinMode)
	gin.DefaultWriter = logger.Logger.Writer()
	gin.DefaultErrorWriter = logger.Logger.Writer()

	// Setup and start the secondary waiting server
	waitingSrv := createWaitingServer(app, logger.Logger)
	go func() {
		if err := waitingSrv.ListenAndServe(fmt.Sprintf(":%d", cfg.Server.WaitingServerPort)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithComponent("main").Errorf("Waiting server error: %v", err)
		}
	}()

	//setup main server routes and start it!
	r := route.SetupRoutes(app, logger.Logger)
	mainSrv := createGraceHttpServer(app.BaseCtx, "main-server", app.Config.Server, r)

	if err := mainSrv.ListenAndServe(fmt.Sprintf(":%d", cfg.Server.Port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.WithComponent("main").Fatal(err)
	}
}

// createWaitingServer creates a secondary HTTP server dedicated to serving only the waiting page.
// It exposes a single route GET /:name that triggers RuntimeController.WaitingPage.
func createWaitingServer(app *appctx.App, logger *logrus.Logger) *httpgrace.Server {
	r := gin.New()
	r.Use(middleware.HoneybadgerMiddleware(logger))
	r.Use(gin.Recovery())

	// Create RuntimeController for the waiting page
	rc := controller.NewRuntimeController(app)
	cc := controller.NewContainerController(app.BaseCtx, app.Cache, app.Runtime, app.Config.Misc.CosmosBaseUrl, app.Config.Misc.CosmosToken)

	r.GET("/container/:name/ready", cc.Ready)
	r.GET("/:name", rc.WaitingPage)

	return createGraceHttpServer(app.BaseCtx, "waiting-server", app.Config.Server, r)
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
