package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/ybira-net/ybira-net/internal/config"
	"github.com/ybira-net/ybira-net/internal/types"
)

type StatsQuerier interface {
	Query(window int) []types.AggregatedStat
	TopN(window, n int) []types.AggregatedStat
}

type DropCounters struct {
	CaptureDrops    func() int64
	MapperDrops     func() int64
	AggregatorDrops func() int64
	StoreDrops      func() int64
}

type Server struct {
	httpServer *http.Server
	hub        *Hub
	querier    StatsQuerier
	drops      DropCounters
	logger     *zap.Logger
	addr       string
}

func NewServer(querier StatsQuerier, drops DropCounters, logger *zap.Logger, cfg config.APIConfig) *Server {
	s := &Server{
		querier: querier,
		drops:   drops,
		logger:  logger.Named("api"),
		addr:    cfg.Listen,
	}

	s.hub = NewHub(querier, s.logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /ws", s.handleWebSocket)

	webDir := findWebDir()
	mux.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir(webDir))))

	s.httpServer = &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

func (s *Server) Start(ctx context.Context) error {
	s.hub.Start(ctx)

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	s.logger.Info("API server listening", zap.String("addr", s.addr))

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.Shutdown()
	case err := <-errCh:
		return err
	}
}

func (s *Server) Shutdown() error {
	s.hub.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.logger.Info("API server shutting down")
	return s.httpServer.Shutdown(ctx)
}

func findWebDir() string {
	if info, err := os.Stat("web"); err == nil && info.IsDir() {
		return "web"
	}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "web")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	return "web"
}
