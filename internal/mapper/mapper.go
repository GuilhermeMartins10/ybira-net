package mapper

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
	"go.uber.org/zap"
)

type MapperStats struct {
	CacheHits   int64
	CacheMisses int64
	Drops       int64
}

type Mapper interface {
	Map(ctx context.Context, in <-chan types.PacketInfo, out chan<- types.FlowEvent) error
	Stats() MapperStats
}

type socketKey struct {
	Protocol   string
	LocalIP    string
	LocalPort  uint16
	RemoteIP   string
	RemotePort uint16
}

type cacheEntry struct {
	PID        int
	Process    string
	Generation int64
}

type ProcessMapper struct {
	reader          ProcReader
	cache           sync.Map
	generation      int64
	refreshInterval time.Duration
	logger          *zap.Logger

	cacheHits   atomic.Int64
	cacheMisses atomic.Int64
	drops       atomic.Int64
}

func NewProcessMapper(reader ProcReader, refreshInterval time.Duration, logger *zap.Logger) *ProcessMapper {
	return &ProcessMapper{
		reader:          reader,
		refreshInterval: refreshInterval,
		logger:          logger.Named("mapper"),
	}
}

func (m *ProcessMapper) Stats() MapperStats {
	return MapperStats{
		CacheHits:   m.cacheHits.Load(),
		CacheMisses: m.cacheMisses.Load(),
		Drops:       m.drops.Load(),
	}
}

func (m *ProcessMapper) Map(ctx context.Context, in <-chan types.PacketInfo, out chan<- types.FlowEvent) error {
	if err := m.refreshCache(); err != nil {
		m.logger.Warn("initial cache refresh failed", zap.Error(err))
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.refreshLoop(ctx)
	}()

	m.processLoop(ctx, in, out)

	wg.Wait()
	return nil
}

func (m *ProcessMapper) processLoop(ctx context.Context, in <-chan types.PacketInfo, out chan<- types.FlowEvent) {
	for {
		select {
		case pkt, ok := <-in:
			if !ok {
				return
			}
			event := m.resolve(pkt)
			select {
			case out <- event:
			default:
				m.drops.Add(1)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *ProcessMapper) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.refreshCache(); err != nil {
				m.logger.Warn("cache refresh failed", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func normalizeIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func (m *ProcessMapper) resolve(pkt types.PacketInfo) types.FlowEvent {
	srcIP := normalizeIP(pkt.SrcIP)
	dstIP := normalizeIP(pkt.DstIP)

	if pkt.Protocol == "tcp" {
		srcKey := socketKey{
			Protocol:   pkt.Protocol,
			LocalIP:    srcIP,
			LocalPort:  pkt.SrcPort,
			RemoteIP:   dstIP,
			RemotePort: pkt.DstPort,
		}
		if entry, ok := m.lookupCache(srcKey); ok {
			m.cacheHits.Add(1)
			return m.buildFlowEvent(pkt, entry.PID, entry.Process)
		}

		dstKey := socketKey{
			Protocol:   pkt.Protocol,
			LocalIP:    dstIP,
			LocalPort:  pkt.DstPort,
			RemoteIP:   srcIP,
			RemotePort: pkt.SrcPort,
		}
		if entry, ok := m.lookupCache(dstKey); ok {
			m.cacheHits.Add(1)
			return m.buildFlowEvent(pkt, entry.PID, entry.Process)
		}
	} else {
		srcKey := socketKey{
			Protocol:  pkt.Protocol,
			LocalIP:   srcIP,
			LocalPort: pkt.SrcPort,
		}
		if entry, ok := m.lookupCache(srcKey); ok {
			m.cacheHits.Add(1)
			return m.buildFlowEvent(pkt, entry.PID, entry.Process)
		}

		dstKey := socketKey{
			Protocol:  pkt.Protocol,
			LocalIP:   dstIP,
			LocalPort: pkt.DstPort,
		}
		if entry, ok := m.lookupCache(dstKey); ok {
			m.cacheHits.Add(1)
			return m.buildFlowEvent(pkt, entry.PID, entry.Process)
		}
	}

	m.cacheMisses.Add(1)
	return m.buildFlowEvent(pkt, 0, "unknown")
}

func (m *ProcessMapper) lookupCache(key socketKey) (*cacheEntry, bool) {
	val, ok := m.cache.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := val.(*cacheEntry)
	return entry, ok
}

func (m *ProcessMapper) buildFlowEvent(pkt types.PacketInfo, pid int, process string) types.FlowEvent {
	return types.FlowEvent{
		Timestamp: pkt.Timestamp,
		PID:       pid,
		Process:   process,
		Bytes:     pkt.Size,
		SrcIP:     pkt.SrcIP,
		DstIP:     pkt.DstIP,
		SrcPort:   pkt.SrcPort,
		DstPort:   pkt.DstPort,
		Protocol:  pkt.Protocol,
	}
}

func (m *ProcessMapper) refreshCache() error {
	m.generation++
	currentGen := m.generation

	connections, err := m.reader.ReadConnections()
	if err != nil {
		return fmt.Errorf("reading connections: %w", err)
	}

	for _, conn := range connections {
		var key socketKey
		if conn.Protocol == "tcp" {
			key = socketKey{
				Protocol:   conn.Protocol,
				LocalIP:    conn.LocalIP,
				LocalPort:  conn.LocalPort,
				RemoteIP:   conn.RemoteIP,
				RemotePort: conn.RemotePort,
			}
		} else {
			key = socketKey{
				Protocol:  conn.Protocol,
				LocalIP:   conn.LocalIP,
				LocalPort: conn.LocalPort,
			}
		}

		m.cache.Store(key, &cacheEntry{
			PID:        conn.PID,
			Process:    conn.ProcessName,
			Generation: currentGen,
		})
	}

	m.cache.Range(func(key, value any) bool {
		entry, ok := value.(*cacheEntry)
		if !ok {
			m.cache.Delete(key)
			return true
		}
		if currentGen-entry.Generation >= 2 {
			m.cache.Delete(key)
		}
		return true
	})

	return nil
}
