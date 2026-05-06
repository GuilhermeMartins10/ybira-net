package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	maxWSClients = 100
	writeWait    = 5 * time.Second
	pongWait     = 60 * time.Second
	pingPeriod   = (pongWait * 9) / 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Hub struct {
	querier StatsQuerier
	logger  *zap.Logger

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}

	clientCount atomic.Int64

	stopCh chan struct{}
	done   chan struct{}
}

func NewHub(querier StatsQuerier, logger *zap.Logger) *Hub {
	return &Hub{
		querier: querier,
		logger:  logger.Named("ws-hub"),
		clients: make(map[*websocket.Conn]struct{}),
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (h *Hub) Start(ctx context.Context) {
	go h.broadcastLoop(ctx)
}

func (h *Hub) Stop() {
	close(h.stopCh)
	<-h.done
}

func (h *Hub) broadcastLoop(ctx context.Context) {
	defer close(h.done)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.broadcast()
		case <-h.stopCh:
			h.closeAllClients()
			return
		case <-ctx.Done():
			h.closeAllClients()
			return
		}
	}
}

func (h *Hub) broadcast() {
	stats := h.querier.TopN(60, 10)

	entries := make([]statEntry, 0, len(stats))
	for _, st := range stats {
		entries = append(entries, statEntry{
			PID:     st.PID,
			Process: st.Process,
			Bytes:   st.TotalBytes,
		})
	}

	msg := struct {
		Window int         `json:"window"`
		Stats  []statEntry `json:"stats"`
	}{
		Window: 60,
		Stats:  entries,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal broadcast message", zap.Error(err))
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.clients {
		conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			h.removeClientLocked(conn)
		}
	}
}

func (h *Hub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	if _, ok := h.clients[conn]; ok {
		delete(h.clients, conn)
		h.clientCount.Add(-1)
		conn.Close()
	}
	h.mu.Unlock()
}

func (h *Hub) removeClientLocked(conn *websocket.Conn) {
	delete(h.clients, conn)
	h.clientCount.Add(-1)
	conn.Close()
}

func (h *Hub) closeAllClients() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.clients {
		conn.Close()
		delete(h.clients, conn)
	}
	h.clientCount.Store(0)
}

func (h *Hub) ClientCount() int64 {
	return h.clientCount.Load()
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.hub.clientCount.Add(1) > maxWSClients {
		s.hub.clientCount.Add(-1)
		http.Error(w, "too many WebSocket clients", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.hub.clientCount.Add(-1)
		s.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	s.hub.Register(conn)
	go s.readPump(conn)
}

func (s *Server) readPump(conn *websocket.Conn) {
	defer s.hub.Unregister(conn)

	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}
