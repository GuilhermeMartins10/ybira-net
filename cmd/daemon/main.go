package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/ybira-net/ybira-net/internal/aggregator"
	"github.com/ybira-net/ybira-net/internal/api"
	"github.com/ybira-net/ybira-net/internal/capture"
	"github.com/ybira-net/ybira-net/internal/config"
	"github.com/ybira-net/ybira-net/internal/daemon"
	"github.com/ybira-net/ybira-net/internal/logging"
	"github.com/ybira-net/ybira-net/internal/mapper"
	"github.com/ybira-net/ybira-net/internal/store"
	"github.com/ybira-net/ybira-net/internal/types"
)

func main() {
	configPath := flag.String("config", "./config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := logging.NewLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("starting ybira-net daemon",
		zap.String("config_path", *configPath),
		zap.String("interface", cfg.Capture.Interface),
		zap.String("listen", cfg.API.Listen),
		zap.String("log_level", cfg.LogLevel),
	)

	Preflight(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agg := aggregator.New()
	go agg.StartClock(ctx)

	chanA := make(chan types.PacketInfo, 1024)
	chanB := make(chan types.FlowEvent, 1024)

	cap := capture.NewLiveCapture(cfg.Capture.Interface, logger)
	procReader := mapper.NewDefaultProcReader()
	m := mapper.NewProcessMapper(procReader, cfg.Mapper.CacheRefreshInterval, logger)
	st := store.NewSQLiteStore(cfg.Store.DatabasePath, cfg.Store.FlushInterval, logger)

	if err := st.Start(ctx); err != nil {
		logger.Fatal("failed to start store", zap.Error(err))
	}

	teeDrops := &daemon.TeeDropCounters{}
	aggChan, storeChan := daemon.Tee(ctx, chanB, teeDrops)

	drops := api.DropCounters{
		CaptureDrops:    func() int64 { return cap.Stats().PacketsDropped },
		MapperDrops:     func() int64 { return m.Stats().Drops },
		AggregatorDrops: func() int64 { return teeDrops.AggDrops.Load() },
		StoreDrops:      func() int64 { return teeDrops.StoreDrops.Load() },
	}

	apiServer := api.NewServer(agg, drops, logger, cfg.API)

	go func() {
		if err := cap.Start(ctx, chanA); err != nil {
			logger.Error("capture engine error", zap.Error(err))
		}
		close(chanA)
	}()

	go func() {
		if err := m.Map(ctx, chanA, chanB); err != nil {
			logger.Error("mapper error", zap.Error(err))
		}
		close(chanB)
	}()

	go agg.Run(ctx, aggChan)

	go func() {
		for {
			select {
			case ev, ok := <-storeChan:
				if !ok {
					return
				}
				if err := st.Write([]types.FlowEvent{ev}); err != nil {
					logger.Error("store write error", zap.Error(err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		if err := apiServer.Start(ctx); err != nil {
			logger.Error("API server error", zap.Error(err))
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logger.Info("drop counters",
					zap.Int64("capture_drops", cap.Stats().PacketsDropped),
					zap.Int64("mapper_drops", m.Stats().Drops),
					zap.Int64("agg_tee_drops", teeDrops.AggDrops.Load()),
					zap.Int64("store_tee_drops", teeDrops.StoreDrops.Load()),
					zap.Int64("store_buffer_drops", st.DropCount()),
				)
			case <-ctx.Done():
				return
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.Info("received signal, initiating shutdown", zap.String("signal", sig.String()))

	cancel()

	time.AfterFunc(10*time.Second, func() {
		logger.Warn("shutdown timeout exceeded, force exit")
		os.Exit(1)
	})

	if err := st.Close(); err != nil {
		logger.Error("store close error", zap.Error(err))
	}

	logger.Info("ybira-net daemon stopped")
}
