package observability

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/trace"
	"sync"
	"syscall"
	"time"

	"tcg-rulex-engine/pkg/gos"
	"tcg-rulex-engine/pkg/logs"
)

const (
	defaultOutputDir = "/tmp/traces"
	traceFileName    = "flight_trace.out"
	defaultMaxBytes  = 10 * 1024 * 1024 // 10MB
	defaultMinAge    = 10 * time.Minute
)

// FlightRecorderConfig holds configuration for the FlightRecorder
type FlightRecorderConfig struct {
	OutputDir string        // directory to write trace files, default: /tmp/traces
	MaxBytes  uint64        // max bytes for in-memory trace buffer, default: 10MB
	MinAge    time.Duration // minimum age of trace data to retain, default: 10 minutes
}

// defaultConfig returns the default FlightRecorderConfig
func defaultConfig() FlightRecorderConfig {
	return FlightRecorderConfig{
		OutputDir: defaultOutputDir,
		MaxBytes:  defaultMaxBytes,
		MinAge:    defaultMinAge,
	}
}

// FlightRecorder wraps Go's runtime/trace.FlightRecorder with signal-based trace dumping.
// Send SIGUSR1 or SIGUSR2 to the process to trigger a trace dump:
//
//	kill -USR1 <pid>
//
// Trace file is always written to: /tmp/traces/flight_trace.out
type FlightRecorder struct {
	ctx            context.Context
	flightRecorder *trace.FlightRecorder
	sigChan        chan os.Signal
	stopChan       chan struct{}
	cfg            FlightRecorderConfig
	wg             sync.WaitGroup
	stopOnce       sync.Once
}

// NewFlightRecorder creates and starts a FlightRecorder with default configuration.
func NewFlightRecorder() (*FlightRecorder, error) {
	return NewFlightRecorderWithConfig(defaultConfig())
}

// NewFlightRecorderWithConfig creates and starts a FlightRecorder with custom configuration.
func NewFlightRecorderWithConfig(cfg FlightRecorderConfig) (*FlightRecorder, error) {
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxBytes
	}
	if cfg.MinAge <= 0 {
		cfg.MinAge = defaultMinAge
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o750); err != nil {
		return nil, err
	}

	fr := trace.NewFlightRecorder(trace.FlightRecorderConfig{
		MaxBytes: cfg.MaxBytes,
		MinAge:   cfg.MinAge,
	})

	if err := fr.Start(); err != nil {
		return nil, err
	}

	recorder := &FlightRecorder{
		ctx:            context.Background(),
		flightRecorder: fr,
		cfg:            cfg,
		sigChan:        make(chan os.Signal, 1),
		stopChan:       make(chan struct{}),
	}

	recorder.listenSignal()
	return recorder, nil
}

// tracePath returns the fixed output file path.
func (r *FlightRecorder) tracePath() string {
	return filepath.Join(r.cfg.OutputDir, traceFileName)
}

// listenSignal listens for SIGUSR1/SIGUSR2 and triggers a trace dump on receipt.
func (r *FlightRecorder) listenSignal() {
	signal.Notify(r.sigChan, syscall.SIGUSR1, syscall.SIGUSR2)

	r.wg.Go(func() {
		defer gos.Recover()
		for {
			select {
			case sig := <-r.sigChan:
				if err := r.dumpTrace(); err != nil {
					log.Printf("[trace] dump failed: %v (signal=%v)", err, sig)
				}

			case <-r.stopChan:
				logs.Info(r.ctx, "[FlightRecorder] signal listener stopped")
				return
			}
		}
	})
}

// dumpTrace writes the current in-memory trace buffer to a fixed file path.
// Each dump overwrites the previous file.
func (r *FlightRecorder) dumpTrace() error {
	path := r.tracePath()
	log.Printf("[trace] dumping trace to %s ...", path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	if _, err := r.flightRecorder.WriteTo(f); err != nil {
		return err
	}

	logs.Info(r.ctx, "[FlightRecorder] trace dumped successfully: %s", path)
	return nil
}

// Stop gracefully shuts down the FlightRecorder and signal listener.
// It blocks until the signal-listener goroutine has exited.
// Safe to call more than once; subsequent calls are no-ops.
func (r *FlightRecorder) Stop() {
	r.stopOnce.Do(func() {
		if r.flightRecorder != nil {
			r.flightRecorder.Stop()
		}

		close(r.stopChan)
		r.wg.Wait() // ensure the goroutine has exited before unregistering the channel

		signal.Stop(r.sigChan)
		close(r.sigChan)
	})
}

// DumpNow manually triggers a trace dump without waiting for a signal.
// Useful for programmatic trace collection (e.g., on error or on demand).
func (r *FlightRecorder) DumpNow() error {
	return r.dumpTrace()
}
