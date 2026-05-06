package mapper

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
	"go.uber.org/zap"
	"pgregory.net/rapid"
)

// **Validates: Requirements 4.1, 4.2, 2.4, 3.3**
// Property: For any PacketInfo whose socket (src OR dst) matches a cached ConnectionEvent,
// the correct PID and process name are returned. For any socket where neither src
// nor dst is found, PID=0 and process="unknown".
// TCP uses full 4-tuple matching; UDP uses local-only matching.
func TestProperty_MapperResolutionCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of known connections with associated PIDs and process names.
		numConns := rapid.IntRange(1, 10).Draw(t, "numConns")

		type knownConn struct {
			Protocol    string
			LocalIP     string
			LocalPort   uint16
			RemoteIP    string
			RemotePort  uint16
			PID         int
			ProcessName string
		}

		conns := make([]knownConn, 0, numConns)
		usedKeys := make(map[string]bool)

		for i := 0; i < numConns; i++ {
			b1 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("lip_b1_%d", i))
			b2 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_b2_%d", i))
			b3 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_b3_%d", i))
			b4 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("lip_b4_%d", i))
			localIP := fmt.Sprintf("%d.%d.%d.%d", b1, b2, b3, b4)

			localPort := rapid.Uint16Range(1, 65534).Draw(t, fmt.Sprintf("lport_%d", i))
			proto := rapid.SampledFrom([]string{"tcp", "udp"}).Draw(t, fmt.Sprintf("proto_%d", i))

			var remoteIP string
			var remotePort uint16
			var key string

			if proto == "tcp" {
				// TCP: generate remote endpoint for full 4-tuple.
				rb1 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("rip_b1_%d", i))
				rb2 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("rip_b2_%d", i))
				rb3 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("rip_b3_%d", i))
				rb4 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("rip_b4_%d", i))
				remoteIP = fmt.Sprintf("%d.%d.%d.%d", rb1, rb2, rb3, rb4)
				remotePort = rapid.Uint16Range(1, 65534).Draw(t, fmt.Sprintf("rport_%d", i))
				key = fmt.Sprintf("%s:%s:%d:%s:%d", proto, localIP, localPort, remoteIP, remotePort)
			} else {
				// UDP: local-only key.
				remoteIP = ""
				remotePort = 0
				key = fmt.Sprintf("%s:%s:%d", proto, localIP, localPort)
			}

			// Skip duplicate keys.
			if usedKeys[key] {
				continue
			}
			usedKeys[key] = true

			pid := rapid.IntRange(1, 65535).Draw(t, fmt.Sprintf("pid_%d", i))
			procName := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, fmt.Sprintf("proc_%d", i))

			conns = append(conns, knownConn{
				Protocol:    proto,
				LocalIP:     localIP,
				LocalPort:   localPort,
				RemoteIP:    remoteIP,
				RemotePort:  remotePort,
				PID:         pid,
				ProcessName: procName,
			})
		}

		// Need at least one connection for the test.
		if len(conns) == 0 {
			return
		}

		// Build ConnectionEvents from known connections.
		events := make([]ConnectionEvent, len(conns))
		for i, c := range conns {
			events[i] = ConnectionEvent{
				Protocol:    c.Protocol,
				LocalIP:     c.LocalIP,
				LocalPort:   c.LocalPort,
				RemoteIP:    c.RemoteIP,
				RemotePort:  c.RemotePort,
				PID:         c.PID,
				ProcessName: c.ProcessName,
			}
		}

		reader := &MockProcReader{Connections: events}

		logger, _ := zap.NewDevelopment()
		mapper := NewProcessMapper(reader, time.Hour, logger) // Long interval; we refresh manually.

		// Manually refresh cache.
		if err := mapper.refreshCache(); err != nil {
			t.Fatalf("cache refresh failed: %v", err)
		}

		// Decide whether to test a known connection (hit) or unknown (miss).
		testKnown := rapid.Bool().Draw(t, "testKnown")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		in := make(chan types.PacketInfo, 1)
		out := make(chan types.FlowEvent, 1)

		go func() {
			_ = mapper.Map(ctx, in, out)
		}()

		// Give time for Map to start processing.
		time.Sleep(10 * time.Millisecond)

		if testKnown {
			// Pick a random known connection and create a packet that matches it.
			idx := rapid.IntRange(0, len(conns)-1).Draw(t, "connIdx")
			c := conns[idx]

			// Decide whether to put the known socket as src or dst.
			asSrc := rapid.Bool().Draw(t, "asSrc")

			var pkt types.PacketInfo
			if c.Protocol == "tcp" {
				// TCP: packet must match the full 4-tuple.
				if asSrc {
					// local=src, remote=dst
					pkt = types.PacketInfo{
						Timestamp: time.Now(),
						SrcIP:     net.ParseIP(c.LocalIP),
						DstIP:     net.ParseIP(c.RemoteIP),
						SrcPort:   c.LocalPort,
						DstPort:   c.RemotePort,
						Protocol:  c.Protocol,
						Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
					}
				} else {
					// local=dst, remote=src
					pkt = types.PacketInfo{
						Timestamp: time.Now(),
						SrcIP:     net.ParseIP(c.RemoteIP),
						DstIP:     net.ParseIP(c.LocalIP),
						SrcPort:   c.RemotePort,
						DstPort:   c.LocalPort,
						Protocol:  c.Protocol,
						Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
					}
				}
			} else {
				// UDP: packet just needs to match local IP:port.
				if asSrc {
					pkt = types.PacketInfo{
						Timestamp: time.Now(),
						SrcIP:     net.ParseIP(c.LocalIP),
						DstIP:     net.ParseIP("99.99.99.99"),
						SrcPort:   c.LocalPort,
						DstPort:   60000,
						Protocol:  c.Protocol,
						Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
					}
				} else {
					pkt = types.PacketInfo{
						Timestamp: time.Now(),
						SrcIP:     net.ParseIP("99.99.99.99"),
						DstIP:     net.ParseIP(c.LocalIP),
						SrcPort:   60000,
						DstPort:   c.LocalPort,
						Protocol:  c.Protocol,
						Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
					}
				}
			}

			in <- pkt

			select {
			case event := <-out:
				if event.PID != c.PID {
					t.Fatalf("known socket: expected PID %d, got %d", c.PID, event.PID)
				}
				if event.Process != c.ProcessName {
					t.Fatalf("known socket: expected process %q, got %q", c.ProcessName, event.Process)
				}
				if event.Bytes != pkt.Size {
					t.Fatalf("known socket: expected bytes %d, got %d", pkt.Size, event.Bytes)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timeout waiting for flow event")
			}
		} else {
			// Generate a packet with IPs/ports that don't match any known connection.
			unknownIP := "250.250.250.250"
			unknownPort := uint16(65535)
			proto := rapid.SampledFrom([]string{"tcp", "udp"}).Draw(t, "unknownProto")

			pkt := types.PacketInfo{
				Timestamp: time.Now(),
				SrcIP:     net.ParseIP(unknownIP),
				DstIP:     net.ParseIP("251.251.251.251"),
				SrcPort:   unknownPort,
				DstPort:   65534,
				Protocol:  proto,
				Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
			}

			in <- pkt

			select {
			case event := <-out:
				if event.PID != 0 {
					t.Fatalf("unknown socket: expected PID 0, got %d", event.PID)
				}
				if event.Process != "unknown" {
					t.Fatalf("unknown socket: expected process 'unknown', got %q", event.Process)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timeout waiting for flow event")
			}
		}

		cancel()
	})
}

// **Validates: Requirements 4.1, 4.2**
// Property 4: Mapper resolution correctness (UDP local-only)
// For any set of UDP ConnectionEvents loaded into the mapper cache, and any PacketInfo
// whose src or dst local socket matches a cached entry, the mapper SHALL return the
// correct PID and process name. Packets that don't match any cached UDP local socket
// get PID=0, process="unknown". Different remote endpoints hitting the same local UDP
// socket all resolve to the same process (since UDP key is local-only).
func TestProperty_MapperUDPResolutionLocalOnly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of known UDP connections with unique local sockets.
		numConns := rapid.IntRange(1, 10).Draw(t, "numConns")

		type udpConn struct {
			LocalIP     string
			LocalPort   uint16
			PID         int
			ProcessName string
		}

		conns := make([]udpConn, 0, numConns)
		usedKeys := make(map[string]bool)

		for i := 0; i < numConns; i++ {
			b1 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("lip_b1_%d", i))
			b2 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_b2_%d", i))
			b3 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_b3_%d", i))
			b4 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("lip_b4_%d", i))
			localIP := fmt.Sprintf("%d.%d.%d.%d", b1, b2, b3, b4)

			localPort := rapid.Uint16Range(1, 65534).Draw(t, fmt.Sprintf("lport_%d", i))

			key := fmt.Sprintf("udp:%s:%d", localIP, localPort)
			if usedKeys[key] {
				continue
			}
			usedKeys[key] = true

			pid := rapid.IntRange(1, 65535).Draw(t, fmt.Sprintf("pid_%d", i))
			procName := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, fmt.Sprintf("proc_%d", i))

			conns = append(conns, udpConn{
				LocalIP:     localIP,
				LocalPort:   localPort,
				PID:         pid,
				ProcessName: procName,
			})
		}

		if len(conns) == 0 {
			return
		}

		// Build ConnectionEvents (UDP: RemoteIP="", RemotePort=0 for cache key purposes).
		events := make([]ConnectionEvent, len(conns))
		for i, c := range conns {
			events[i] = ConnectionEvent{
				Protocol:    "udp",
				LocalIP:     c.LocalIP,
				LocalPort:   c.LocalPort,
				RemoteIP:    "",
				RemotePort:  0,
				PID:         c.PID,
				ProcessName: c.ProcessName,
			}
		}

		reader := &MockProcReader{Connections: events}
		logger, _ := zap.NewDevelopment()
		m := NewProcessMapper(reader, time.Hour, logger)

		if err := m.refreshCache(); err != nil {
			t.Fatalf("cache refresh failed: %v", err)
		}

		// Choose which scenario to test.
		scenario := rapid.IntRange(0, 2).Draw(t, "scenario")

		switch scenario {
		case 0:
			// Scenario 1: Packet whose src matches a cached UDP connection's local IP:port.
			idx := rapid.IntRange(0, len(conns)-1).Draw(t, "connIdx")
			c := conns[idx]

			// Generate a random remote endpoint (different from local).
			rb1 := rapid.IntRange(1, 254).Draw(t, "rip_b1")
			rb2 := rapid.IntRange(0, 255).Draw(t, "rip_b2")
			rb3 := rapid.IntRange(0, 255).Draw(t, "rip_b3")
			rb4 := rapid.IntRange(1, 254).Draw(t, "rip_b4")
			remoteIP := fmt.Sprintf("%d.%d.%d.%d", rb1, rb2, rb3, rb4)
			remotePort := rapid.Uint16Range(1, 65534).Draw(t, "rport")

			pkt := types.PacketInfo{
				Timestamp: time.Now(),
				SrcIP:     net.ParseIP(c.LocalIP),
				DstIP:     net.ParseIP(remoteIP),
				SrcPort:   c.LocalPort,
				DstPort:   remotePort,
				Protocol:  "udp",
				Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
			}

			event := m.resolve(pkt)
			if event.PID != c.PID {
				t.Fatalf("UDP src match: expected PID %d, got %d", c.PID, event.PID)
			}
			if event.Process != c.ProcessName {
				t.Fatalf("UDP src match: expected process %q, got %q", c.ProcessName, event.Process)
			}

		case 1:
			// Scenario 2: Packet whose dst matches a cached UDP connection's local IP:port.
			idx := rapid.IntRange(0, len(conns)-1).Draw(t, "connIdx")
			c := conns[idx]

			rb1 := rapid.IntRange(1, 254).Draw(t, "rip_b1")
			rb2 := rapid.IntRange(0, 255).Draw(t, "rip_b2")
			rb3 := rapid.IntRange(0, 255).Draw(t, "rip_b3")
			rb4 := rapid.IntRange(1, 254).Draw(t, "rip_b4")
			remoteIP := fmt.Sprintf("%d.%d.%d.%d", rb1, rb2, rb3, rb4)
			remotePort := rapid.Uint16Range(1, 65534).Draw(t, "rport")

			pkt := types.PacketInfo{
				Timestamp: time.Now(),
				SrcIP:     net.ParseIP(remoteIP),
				DstIP:     net.ParseIP(c.LocalIP),
				SrcPort:   remotePort,
				DstPort:   c.LocalPort,
				Protocol:  "udp",
				Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
			}

			event := m.resolve(pkt)
			if event.PID != c.PID {
				t.Fatalf("UDP dst match: expected PID %d, got %d", c.PID, event.PID)
			}
			if event.Process != c.ProcessName {
				t.Fatalf("UDP dst match: expected process %q, got %q", c.ProcessName, event.Process)
			}

		case 2:
			// Scenario 3: Packet that doesn't match any cached UDP local socket.
			// Use IPs/ports guaranteed not to be in the generated set.
			pkt := types.PacketInfo{
				Timestamp: time.Now(),
				SrcIP:     net.ParseIP("250.250.250.250"),
				DstIP:     net.ParseIP("251.251.251.251"),
				SrcPort:   65535,
				DstPort:   65535,
				Protocol:  "udp",
				Size:      rapid.IntRange(1, 65535).Draw(t, "size"),
			}

			event := m.resolve(pkt)
			if event.PID != 0 {
				t.Fatalf("UDP miss: expected PID 0, got %d", event.PID)
			}
			if event.Process != "unknown" {
				t.Fatalf("UDP miss: expected process 'unknown', got %q", event.Process)
			}
		}

		// Additional property: different remote endpoints hitting the same local UDP socket
		// all resolve to the same process (since UDP key is local-only).
		if scenario != 2 {
			idx := rapid.IntRange(0, len(conns)-1).Draw(t, "multiRemoteIdx")
			c := conns[idx]

			// Send packets from multiple different remote endpoints to the same local socket.
			numRemotes := rapid.IntRange(2, 5).Draw(t, "numRemotes")
			for r := 0; r < numRemotes; r++ {
				rb1 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("mr_b1_%d", r))
				rb2 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("mr_b2_%d", r))
				rb3 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("mr_b3_%d", r))
				rb4 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("mr_b4_%d", r))
				rIP := fmt.Sprintf("%d.%d.%d.%d", rb1, rb2, rb3, rb4)
				rPort := rapid.Uint16Range(1, 65534).Draw(t, fmt.Sprintf("mr_port_%d", r))

				pkt := types.PacketInfo{
					Timestamp: time.Now(),
					SrcIP:     net.ParseIP(rIP),
					DstIP:     net.ParseIP(c.LocalIP),
					SrcPort:   rPort,
					DstPort:   c.LocalPort,
					Protocol:  "udp",
					Size:      100,
				}

				event := m.resolve(pkt)
				if event.PID != c.PID {
					t.Fatalf("UDP multi-remote: expected PID %d for remote %s:%d, got %d",
						c.PID, rIP, rPort, event.PID)
				}
				if event.Process != c.ProcessName {
					t.Fatalf("UDP multi-remote: expected process %q for remote %s:%d, got %q",
						c.ProcessName, rIP, rPort, event.Process)
				}
			}
		}
	})
}

// **Validates: Requirements 10.4**
// Property 5: Cache eviction after 2 refresh cycles
// For any cache entry that is not present in the ConnectionEvents returned by the last
// 2 consecutive refresh cycles, that entry SHALL be evicted from the cache. Entries
// present in the most recent or second-most-recent cycle SHALL remain.
func TestProperty_CacheEvictionAfterTwoCycles(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a pool of unique connections to draw from across cycles.
		poolSize := rapid.IntRange(3, 15).Draw(t, "poolSize")

		type connInfo struct {
			Protocol    string
			LocalIP     string
			LocalPort   uint16
			RemoteIP    string
			RemotePort  uint16
			PID         int
			ProcessName string
			Key         socketKey
		}

		pool := make([]connInfo, 0, poolSize)
		usedKeys := make(map[string]bool)

		for i := 0; i < poolSize; i++ {
			b1 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("pool_b1_%d", i))
			b2 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("pool_b2_%d", i))
			b3 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("pool_b3_%d", i))
			b4 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("pool_b4_%d", i))
			localIP := fmt.Sprintf("%d.%d.%d.%d", b1, b2, b3, b4)
			localPort := rapid.Uint16Range(1, 65534).Draw(t, fmt.Sprintf("pool_lport_%d", i))
			proto := rapid.SampledFrom([]string{"tcp", "udp"}).Draw(t, fmt.Sprintf("pool_proto_%d", i))

			var remoteIP string
			var remotePort uint16
			var key socketKey
			var keyStr string

			if proto == "tcp" {
				rb1 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("pool_rb1_%d", i))
				rb2 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("pool_rb2_%d", i))
				rb3 := rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("pool_rb3_%d", i))
				rb4 := rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("pool_rb4_%d", i))
				remoteIP = fmt.Sprintf("%d.%d.%d.%d", rb1, rb2, rb3, rb4)
				remotePort = rapid.Uint16Range(1, 65534).Draw(t, fmt.Sprintf("pool_rport_%d", i))
				key = socketKey{
					Protocol:   proto,
					LocalIP:    localIP,
					LocalPort:  localPort,
					RemoteIP:   remoteIP,
					RemotePort: remotePort,
				}
				keyStr = fmt.Sprintf("%s:%s:%d:%s:%d", proto, localIP, localPort, remoteIP, remotePort)
			} else {
				remoteIP = ""
				remotePort = 0
				key = socketKey{
					Protocol:  proto,
					LocalIP:   localIP,
					LocalPort: localPort,
				}
				keyStr = fmt.Sprintf("%s:%s:%d", proto, localIP, localPort)
			}

			if usedKeys[keyStr] {
				continue
			}
			usedKeys[keyStr] = true

			pid := rapid.IntRange(1, 65535).Draw(t, fmt.Sprintf("pool_pid_%d", i))
			procName := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, fmt.Sprintf("pool_proc_%d", i))

			pool = append(pool, connInfo{
				Protocol:    proto,
				LocalIP:     localIP,
				LocalPort:   localPort,
				RemoteIP:    remoteIP,
				RemotePort:  remotePort,
				PID:         pid,
				ProcessName: procName,
				Key:         key,
			})
		}

		if len(pool) < 2 {
			return
		}

		// Generate N refresh cycles (at least 3 to observe eviction behavior).
		numCycles := rapid.IntRange(3, 8).Draw(t, "numCycles")

		// For each cycle, randomly select a subset of the pool to be "active".
		cycleConnSets := make([][]int, numCycles) // indices into pool
		for c := 0; c < numCycles; c++ {
			// Each cycle has between 1 and len(pool) active connections.
			numActive := rapid.IntRange(1, len(pool)).Draw(t, fmt.Sprintf("cycle_%d_numActive", c))
			// Draw a random subset by shuffling indices.
			indices := make([]int, len(pool))
			for i := range indices {
				indices[i] = i
			}
			// Fisher-Yates-like selection using rapid draws.
			selected := make([]int, 0, numActive)
			available := make([]int, len(pool))
			copy(available, indices)
			for s := 0; s < numActive && len(available) > 0; s++ {
				pick := rapid.IntRange(0, len(available)-1).Draw(t, fmt.Sprintf("cycle_%d_pick_%d", c, s))
				selected = append(selected, available[pick])
				// Remove picked element.
				available[pick] = available[len(available)-1]
				available = available[:len(available)-1]
			}
			cycleConnSets[c] = selected
		}

		// Create the mapper with a mock reader (we'll update its Connections each cycle).
		reader := &MockProcReader{}
		logger, _ := zap.NewDevelopment()
		m := NewProcessMapper(reader, time.Hour, logger)

		// Run through each cycle and verify cache state after each refresh.
		for c := 0; c < numCycles; c++ {
			// Build ConnectionEvents for this cycle.
			activeIndices := cycleConnSets[c]
			events := make([]ConnectionEvent, len(activeIndices))
			for i, idx := range activeIndices {
				p := pool[idx]
				events[i] = ConnectionEvent{
					Protocol:    p.Protocol,
					LocalIP:     p.LocalIP,
					LocalPort:   p.LocalPort,
					RemoteIP:    p.RemoteIP,
					RemotePort:  p.RemotePort,
					PID:         p.PID,
					ProcessName: p.ProcessName,
				}
			}
			reader.Connections = events

			if err := m.refreshCache(); err != nil {
				t.Fatalf("cycle %d: cache refresh failed: %v", c, err)
			}

			// After refresh, verify the cache state.
			// Build a set of which pool indices were active in this cycle and the previous cycle.
			activeThisCycle := make(map[int]bool)
			for _, idx := range cycleConnSets[c] {
				activeThisCycle[idx] = true
			}

			var activePrevCycle map[int]bool
			if c > 0 {
				activePrevCycle = make(map[int]bool)
				for _, idx := range cycleConnSets[c-1] {
					activePrevCycle[idx] = true
				}
			}

			// Check each pool entry's cache presence.
			for idx, p := range pool {
				_, inCache := m.lookupCache(p.Key)

				// An entry should be in cache if it was active in this cycle OR the previous cycle.
				shouldBeInCache := activeThisCycle[idx]
				if c > 0 && activePrevCycle[idx] {
					shouldBeInCache = true
				}

				// An entry should be evicted if it was NOT in this cycle AND NOT in the previous cycle.
				// (For cycle 0, there's no "previous" so only current cycle matters, but entries
				// from before the mapper was created don't exist anyway.)

				if shouldBeInCache && !inCache {
					t.Fatalf("cycle %d: entry %v (pool idx %d) should be in cache (active in current or previous cycle) but was not found",
						c, p.Key, idx)
				}

				if !shouldBeInCache && inCache {
					// This entry was not in the last 2 cycles. It should have been evicted.
					// However, it could still be in cache if it was active 1 cycle ago (for c >= 2).
					// Let's be more precise: entry should be evicted only if absent for 2+ consecutive cycles.
					// Since we already checked shouldBeInCache covers current and previous, if it's still
					// in cache it means the eviction logic didn't work.
					if c >= 2 {
						t.Fatalf("cycle %d: entry %v (pool idx %d) should have been evicted (not seen in last 2 cycles) but was still in cache",
							c, p.Key, idx)
					}
				}
			}
		}
	})
}
