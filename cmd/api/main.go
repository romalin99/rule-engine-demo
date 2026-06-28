// Command api is the layered HTTP service for the rule engine.
//
// The bootstrap tcg-rulex-engine: an `application` container initialises the
// layers in dependency order (config → infra → service → handler →
// observability), configures Fiber v3, mounts the FULL ops API (scoring, rules
// CRUD, versions, web console, swagger), and tears everything down gracefully on
// SIGINT/SIGTERM.
//
//	go run ./cmd/api            # this layered server (full ops surface)
//	go run ./cmd/cli -demo      # engine CLI / demos / benchmarks
//
// Environment is selected by ENV (dev|sit|prod); config is read from
// ./config/<env>.toml.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"

	"tcg-rulex-engine/internal/config"
	"tcg-rulex-engine/internal/handler"
	"tcg-rulex-engine/internal/infra"
	"tcg-rulex-engine/internal/middleware"
	"tcg-rulex-engine/internal/observability"
	"tcg-rulex-engine/internal/router"
	"tcg-rulex-engine/internal/service"
	"tcg-rulex-engine/pkg/gos"
	"tcg-rulex-engine/pkg/logs"
	"tcg-rulex-engine/pkg/memstatus"
	"tcg-rulex-engine/pkg/metrics"
)

var configFile = flag.String("f", "", "optional config file path (default: ./config/<env>.toml)")

func init() {
	cores := runtime.NumCPU()
	runtime.GOMAXPROCS(int(float64(cores)*5 + 0.5))
}

// ── Application container ─────────────────────────────────────────────────────

// application holds all runtime components needed for serving and graceful shutdown.
type application struct {
	com            *infra.ComManager
	ruleSvc        *service.RuleService
	ruleHandler    *handler.RuleHandler
	flightRecorder *observability.FlightRecorder
	pprofServer    *http.Server       // optional; nil if pprof is disabled
	stopMemStats   context.CancelFunc // stops the memstats goroutine
}

// newApplication initialises all layers in dependency order:
// config → infra → service → handler → observability.
func newApplication(cfg *config.Config) (*application, error) {
	ctx := context.Background()

	cfg.InitLog()
	metrics.Init(cfg.Log.ServiceName)
	cfg.TracerProvider = cfg.Telemetry.InitTracer()
	logs.Info(ctx, "server timeout = %vs", cfg.Timeout)

	// infra: rule engine + manager + optional datastores/clients (DB off by default).
	com := infra.NewComManager(cfg)

	// service: compile rules from the repository into the engine cache.
	ruleSvc := service.NewRuleService(com)
	loaded, failed, err := ruleSvc.LoadRules(ctx)
	if err != nil {
		return nil, err
	}
	logs.Info(ctx, "rule cache ready: loaded=%d failed=%d", loaded, failed)

	// handler: full HTTP ops surface over the service.
	ruleHandler := handler.NewRuleHandler(ruleSvc)

	// observability: in-memory flight recorder (SIGUSR1/2 dumps a trace). Non-fatal.
	fr, frErr := observability.NewFlightRecorder()
	if frErr != nil {
		logs.Warn(ctx, "flight recorder disabled: %v", frErr)
	}

	// memstats sampler on a cancellable context.
	memCtx, stopMem := context.WithCancel(ctx)
	gos.GoSafe(func() { memstatus.MemStats(memCtx) })

	return &application{
		com:            com,
		ruleSvc:        ruleSvc,
		ruleHandler:    ruleHandler,
		flightRecorder: fr,
		stopMemStats:   stopMem,
	}, nil
}

// ── Server lifecycle ──────────────────────────────────────────────────────────

// shutdownComponents tears down every runtime component in the correct order.
func (a *application) shutdownComponents(ctx, shutdownCtx context.Context, fiberApp *fiber.App, cfg *config.Config) {
	logs.Info(ctx, "Shutting down Fiber server...")
	if err := fiberApp.ShutdownWithContext(shutdownCtx); err != nil {
		logs.Err(ctx, "Failed to shutdown Fiber server: %v", err)
	}

	if a.pprofServer != nil {
		logs.Info(ctx, "Shutting down pprof server...")
		if err := a.pprofServer.Shutdown(shutdownCtx); err != nil {
			logs.Err(ctx, "Failed to shutdown pprof server: %v", err)
		}
	}

	if a.flightRecorder != nil {
		a.flightRecorder.Stop()
	}

	if a.stopMemStats != nil {
		a.stopMemStats()
	}

	gos.ReleasePool()

	if a.com != nil {
		a.com.Close()
	}

	if cfg != nil {
		cfg.Close()
	}

	logs.Info(ctx, "Flushing logs...")
	logs.Flush()
	logs.Close()
}

// gracefulShutdown waits for a termination signal and shuts down all components
// within the configured timeout. The returned channel closes once cleanup
// (including log flushing) has finished; main() MUST wait on it before returning.
func gracefulShutdown(fiberApp *fiber.App, cfg *config.Config, app *application) <-chan struct{} {
	allDone := make(chan struct{})

	go func() {
		defer close(allDone)
		ctx := context.Background()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		sig := <-sigCh
		logs.Info(ctx, "Received shutdown signal: %v, starting graceful shutdown...", sig)

		shutdownTimeout := 30 * time.Second
		if cfg.ShutdownTimeout > 0 {
			shutdownTimeout = time.Duration(cfg.ShutdownTimeout) * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		done := make(chan struct{}, 1)
		go func() {
			app.shutdownComponents(ctx, shutdownCtx, fiberApp, cfg)
			done <- struct{}{}
		}()

		select {
		case <-done:
			log.Println("✓ Graceful shutdown completed successfully")
		case <-shutdownCtx.Done():
			log.Printf("✗ Graceful shutdown timed out after %v — forcing exit\n", shutdownTimeout)
			logs.Flush()
		}
	}()

	return allDone
}

// displayServerInfos logs the startup banner.
func displayServerInfos(cfg *config.Config, fiberApp *fiber.App) {
	ctx := context.Background()
	logs.Info(ctx, "═══════════════════════════════════════════════════")
	logs.Info(ctx, "Fiber v%s", fiber.Version)
	logs.Info(ctx, "Server URL: http://127.0.0.1:%d", cfg.Port)
	logs.Info(ctx, "Bound on host %s and port %d", cfg.Host, cfg.Port)
	logs.Info(ctx, "Handlers: %d | Processes: 1", fiberApp.HandlersCount())
	logs.Info(ctx, "GOMAXPROCS: %d | PID: %d", runtime.GOMAXPROCS(0), os.Getpid())
	logs.Info(ctx, "═══════════════════════════════════════════════════")
}

// ── Entry point ───────────────────────────────────────────────────────────────

// @title			AIRuleX 规则引擎 API
// @version		2.0
// @description	AIRuleX 实时规则引擎（分层架构，全功能）：在线评分（/match、/match/batch、/evaluate、/evaluate/all）、规则热更新（/rules*）、版本管理（/versions*）、运维控制台（/）。
// @host			localhost:18080
// @BasePath		/tcg-rulex-engine
// @schemes		http
func main() {
	flag.Parse()

	var cfg config.Config
	cfg.Init(*configFile)

	app, err := newApplication(&cfg)
	if err != nil {
		logs.Fatalf(context.Background(), "Failed to initialise application: %v", err)
	}

	// ── pprof server (optional) ───────────────────────────────────
	if cfg.Pprof.Enabled && !fiber.IsChild() {
		addr := net.JoinHostPort(cfg.Pprof.Host, strconv.Itoa(cfg.Pprof.Port))
		srv := &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		app.pprofServer = srv
		gos.GoSafe(func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logs.Err(context.Background(), "pprof server failed: %v", err)
			}
		})
		logs.Info(context.Background(), "serving pprof on http://%s/debug/pprof", addr)
	}

	// ── Fiber app ─────────────────────────────────────────────────
	fiberApp := fiber.New(fiber.Config{
		StrictRouting:     true,
		CaseSensitive:     true,
		ReduceMemoryUsage: true,
		BodyLimit:         cfg.BodyLimit,
		ReadTimeout:       time.Duration(cfg.Timeout) * time.Second,
		WriteTimeout:      time.Duration(cfg.Timeout) * time.Second,
		IdleTimeout:       120 * time.Second,
		ServerHeader:      cfg.Name,
		JSONEncoder:       sonic.Marshal,
		JSONDecoder:       sonic.Unmarshal,
		ErrorHandler:      middleware.ErrorHandler,
	})

	// ── Middleware chain ──────────────────────────────────────────
	fiberApp.Use(cors.New())
	fiberApp.Use(middleware.Recover())
	if cfg.Telemetry.Enabled {
		fiberApp.Use(middleware.EnableOtelTrace(middleware.OtelConfig{SkipPaths: cfg.TraceIgnorePaths}))
	}
	fiberApp.Use(middleware.NewBehaviorLogger(cfg.Log.ServiceName).Handle())

	// ── Routes: full layered ops API + Swagger ────────────────────
	router.RegisterPrometheus(fiberApp, &cfg)
	router.RegisterHandlers(fiberApp, app.ruleHandler)
	router.Init(fiberApp, &cfg)

	// Flush buffered logs on any main() return path.
	defer logs.Close()

	// ── Graceful shutdown ─────────────────────────────────────────
	shutdownDone := gracefulShutdown(fiberApp, &cfg, app)

	// ── Listen ────────────────────────────────────────────────────
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	displayServerInfos(&cfg, fiberApp)
	if err := fiberApp.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
		logs.Fatalf(context.Background(), "Service failed to start: %v", err)
	}

	<-shutdownDone
}
