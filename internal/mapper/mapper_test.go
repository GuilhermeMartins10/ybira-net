package mapper

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
	"go.uber.org/zap"
)

// MockProcReader implements ProcReader for testing.
type MockProcReader struct {
	// Connections is the list of ConnectionEvents to return from ReadConnections.
	Connections []ConnectionEvent
	// ConnErr is returned as error from ReadConnections if set.
	ConnErr error
}

// ReadConnections implements ProcReader.
func (m *MockProcReader) ReadConnections() ([]ConnectionEvent, error) {
	if m.ConnErr != nil {
		return nil, m.ConnErr
	}
	return m.Connections, nil
}

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// mockProcNetTCP generates /proc/net/tcp content with remote address support.
// Format: "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"
func mockProcNetTCP(entries ...struct {
	IP       string
	Port     uint16
	RemoteIP string
	RemPort  uint16
	Inode    uint64
}) []byte {
	lines := []string{"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"}
	for i, e := range entries {
		hexIP := ipToHexLE(net.ParseIP(e.IP))
		hexPort := fmt.Sprintf("%04X", e.Port)
		remIP := "00000000"
		remPort := "0000"
		if e.RemoteIP != "" {
			remIP = ipToHexLE(net.ParseIP(e.RemoteIP))
			remPort = fmt.Sprintf("%04X", e.RemPort)
		}
		line := fmt.Sprintf("   %d: %s:%s %s:%s 0A 00000000:00000000 00:00000000 00000000     0        0 %d",
			i, hexIP, hexPort, remIP, remPort, e.Inode)
		lines = append(lines, line)
	}
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return []byte(result)
}

// mockProcNetUDP generates /proc/net/udp content.
func mockProcNetUDP(entries ...struct {
	IP    string
	Port  uint16
	Inode uint64
}) []byte {
	lines := []string{"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"}
	for i, e := range entries {
		hexIP := ipToHexLE(net.ParseIP(e.IP))
		hexPort := fmt.Sprintf("%04X", e.Port)
		line := fmt.Sprintf("   %d: %s:%s 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 %d",
			i, hexIP, hexPort, e.Inode)
		lines = append(lines, line)
	}
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return []byte(result)
}

// ipToHexLE converts an IPv4 address to hex little-endian format.
func ipToHexLE(ip net.IP) string {
	ip4 := ip.To4()
	if ip4 == nil {
		return "00000000"
	}
	// Little-endian: reverse the bytes.
	return fmt.Sprintf("%02X%02X%02X%02X", ip4[3], ip4[2], ip4[1], ip4[0])
}

func TestParseProcNet_TCP(t *testing.T) {
	data := mockProcNetTCP(
		struct {
			IP       string
			Port     uint16
			RemoteIP string
			RemPort  uint16
			Inode    uint64
		}{"127.0.0.1", 53, "", 0, 12345},
		struct {
			IP       string
			Port     uint16
			RemoteIP string
			RemPort  uint16
			Inode    uint64
		}{"192.168.1.1", 8080, "", 0, 67890},
	)

	entries := ParseProcNet(data)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].IP != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %s", entries[0].IP)
	}
	if entries[0].Port != 53 {
		t.Errorf("expected port 53, got %d", entries[0].Port)
	}
	if entries[0].Inode != 12345 {
		t.Errorf("expected inode 12345, got %d", entries[0].Inode)
	}

	if entries[1].IP != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", entries[1].IP)
	}
	if entries[1].Port != 8080 {
		t.Errorf("expected port 8080, got %d", entries[1].Port)
	}
	if entries[1].Inode != 67890 {
		t.Errorf("expected inode 67890, got %d", entries[1].Inode)
	}
}

func TestParseProcNet_SkipsZeroInode(t *testing.T) {
	data := mockProcNetTCP(
		struct {
			IP       string
			Port     uint16
			RemoteIP string
			RemPort  uint16
			Inode    uint64
		}{"127.0.0.1", 80, "", 0, 0},
	)

	entries := ParseProcNet(data)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries (inode 0 should be skipped), got %d", len(entries))
	}
}

func TestParseHexIP(t *testing.T) {
	tests := []struct {
		hex      string
		expected string
	}{
		{"0100007F", "127.0.0.1"},
		{"0101A8C0", "192.168.1.1"},
		{"00000000", "0.0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.hex, func(t *testing.T) {
			ip, err := parseHexIP(tt.hex)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ip != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, ip)
			}
		})
	}
}

func TestParseSocketLink(t *testing.T) {
	tests := []struct {
		link    string
		inode   uint64
		wantOK  bool
	}{
		{"socket:[12345]", 12345, true},
		{"socket:[0]", 0, true},
		{"socket:[999999]", 999999, true},
		{"pipe:[123]", 0, false},
		{"/dev/null", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.link, func(t *testing.T) {
			inode, ok := parseSocketLink(tt.link)
			if ok != tt.wantOK {
				t.Errorf("expected ok=%v, got ok=%v", tt.wantOK, ok)
			}
			if ok && inode != tt.inode {
				t.Errorf("expected inode %d, got %d", tt.inode, inode)
			}
		})
	}
}

func TestProcessMapper_ResolveSrcSocket(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{
			{
				Protocol:    "tcp",
				LocalIP:     "192.168.1.100",
				LocalPort:   45678,
				RemoteIP:    "10.0.0.1",
				RemotePort:  80,
				PID:         1234,
				ProcessName: "firefox",
			},
		},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent, 10)

	go func() {
		_ = mapper.Map(ctx, in, out)
	}()

	// Wait for initial cache refresh.
	time.Sleep(50 * time.Millisecond)

	// Send a packet with src matching the cached socket's local side.
	in <- types.PacketInfo{
		Timestamp: time.Now(),
		SrcIP:     net.ParseIP("192.168.1.100"),
		DstIP:     net.ParseIP("10.0.0.1"),
		SrcPort:   45678,
		DstPort:   80,
		Protocol:  "tcp",
		Size:      1500,
	}

	select {
	case event := <-out:
		if event.PID != 1234 {
			t.Errorf("expected PID 1234, got %d", event.PID)
		}
		if event.Process != "firefox" {
			t.Errorf("expected process 'firefox', got %q", event.Process)
		}
		if event.Bytes != 1500 {
			t.Errorf("expected 1500 bytes, got %d", event.Bytes)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for flow event")
	}

	cancel()
}

func TestProcessMapper_ResolveDstSocket(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{
			{
				Protocol:    "tcp",
				LocalIP:     "10.0.0.1",
				LocalPort:   80,
				RemoteIP:    "192.168.1.100",
				RemotePort:  45678,
				PID:         5678,
				ProcessName: "nginx",
			},
		},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent, 10)

	go func() {
		_ = mapper.Map(ctx, in, out)
	}()

	time.Sleep(50 * time.Millisecond)

	// Send a packet where dst matches the cached socket's local side.
	in <- types.PacketInfo{
		Timestamp: time.Now(),
		SrcIP:     net.ParseIP("192.168.1.100"),
		DstIP:     net.ParseIP("10.0.0.1"),
		SrcPort:   45678,
		DstPort:   80,
		Protocol:  "tcp",
		Size:      2048,
	}

	select {
	case event := <-out:
		if event.PID != 5678 {
			t.Errorf("expected PID 5678, got %d", event.PID)
		}
		if event.Process != "nginx" {
			t.Errorf("expected process 'nginx', got %q", event.Process)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for flow event")
	}

	cancel()
}

func TestProcessMapper_UnknownSocket(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent, 10)

	go func() {
		_ = mapper.Map(ctx, in, out)
	}()

	time.Sleep(50 * time.Millisecond)

	in <- types.PacketInfo{
		Timestamp: time.Now(),
		SrcIP:     net.ParseIP("1.2.3.4"),
		DstIP:     net.ParseIP("5.6.7.8"),
		SrcPort:   12345,
		DstPort:   443,
		Protocol:  "tcp",
		Size:      100,
	}

	select {
	case event := <-out:
		if event.PID != 0 {
			t.Errorf("expected PID 0, got %d", event.PID)
		}
		if event.Process != "unknown" {
			t.Errorf("expected process 'unknown', got %q", event.Process)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for flow event")
	}

	cancel()
}

func TestProcessMapper_Backpressure(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent) // Unbuffered - will block immediately.

	go func() {
		_ = mapper.Map(ctx, in, out)
	}()

	time.Sleep(50 * time.Millisecond)

	// Send multiple packets - they should be dropped since out is full.
	for i := 0; i < 5; i++ {
		in <- types.PacketInfo{
			Timestamp: time.Now(),
			SrcIP:     net.ParseIP("1.2.3.4"),
			DstIP:     net.ParseIP("5.6.7.8"),
			SrcPort:   uint16(10000 + i),
			DstPort:   80,
			Protocol:  "tcp",
			Size:      100,
		}
	}

	// Give time for processing.
	time.Sleep(100 * time.Millisecond)

	stats := mapper.Stats()
	if stats.Drops < 4 {
		t.Errorf("expected at least 4 drops, got %d", stats.Drops)
	}

	cancel()
}

func TestProcessMapper_GenerationEviction(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{
			{
				Protocol:    "tcp",
				LocalIP:     "192.168.1.1",
				LocalPort:   8080,
				RemoteIP:    "10.0.0.1",
				RemotePort:  12345,
				PID:         100,
				ProcessName: "myapp",
			},
		},
	}

	mapper := NewProcessMapper(reader, 50*time.Millisecond, testLogger())

	// First refresh populates cache.
	if err := mapper.refreshCache(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	// Verify entry exists (TCP uses full 4-tuple key).
	key := socketKey{Protocol: "tcp", LocalIP: "192.168.1.1", LocalPort: 8080, RemoteIP: "10.0.0.1", RemotePort: 12345}
	if _, ok := mapper.lookupCache(key); !ok {
		t.Fatal("expected cache entry after first refresh")
	}

	// Remove the socket from connections (simulating connection closed).
	reader.Connections = []ConnectionEvent{}

	// Second refresh - entry still present (only 1 cycle missed).
	if err := mapper.refreshCache(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if _, ok := mapper.lookupCache(key); !ok {
		t.Fatal("expected cache entry to survive 1 missed cycle")
	}

	// Third refresh - entry should be evicted (2 cycles missed).
	if err := mapper.refreshCache(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if _, ok := mapper.lookupCache(key); ok {
		t.Fatal("expected cache entry to be evicted after 2 missed cycles")
	}
}

func TestProcessMapper_UDPResolution(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{
			{
				Protocol:    "udp",
				LocalIP:     "10.0.0.5",
				LocalPort:   53,
				RemoteIP:    "",
				RemotePort:  0,
				PID:         999,
				ProcessName: "dnsmasq",
			},
		},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent, 10)

	go func() {
		_ = mapper.Map(ctx, in, out)
	}()

	time.Sleep(50 * time.Millisecond)

	in <- types.PacketInfo{
		Timestamp: time.Now(),
		SrcIP:     net.ParseIP("192.168.1.50"),
		DstIP:     net.ParseIP("10.0.0.5"),
		SrcPort:   54321,
		DstPort:   53,
		Protocol:  "udp",
		Size:      64,
	}

	select {
	case event := <-out:
		if event.PID != 999 {
			t.Errorf("expected PID 999, got %d", event.PID)
		}
		if event.Process != "dnsmasq" {
			t.Errorf("expected process 'dnsmasq', got %q", event.Process)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for flow event")
	}

	cancel()
}

func TestProcessMapper_ContextCancellation(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithCancel(context.Background())

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent, 10)

	done := make(chan struct{})
	go func() {
		_ = mapper.Map(ctx, in, out)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Map returned after context cancellation.
	case <-time.After(2 * time.Second):
		t.Fatal("Map did not return after context cancellation")
	}
}

func TestProcessMapper_Stats(t *testing.T) {
	reader := &MockProcReader{
		Connections: []ConnectionEvent{
			{
				Protocol:    "tcp",
				LocalIP:     "192.168.1.1",
				LocalPort:   80,
				RemoteIP:    "10.0.0.1",
				RemotePort:  45000,
				PID:         200,
				ProcessName: "httpd",
			},
		},
	}

	mapper := NewProcessMapper(reader, 100*time.Millisecond, testLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan types.PacketInfo, 10)
	out := make(chan types.FlowEvent, 10)

	go func() {
		_ = mapper.Map(ctx, in, out)
	}()

	time.Sleep(50 * time.Millisecond)

	// Send a hit (packet matches the cached connection's 4-tuple).
	in <- types.PacketInfo{
		Timestamp: time.Now(),
		SrcIP:     net.ParseIP("10.0.0.1"),
		DstIP:     net.ParseIP("192.168.1.1"),
		SrcPort:   45000,
		DstPort:   80,
		Protocol:  "tcp",
		Size:      500,
	}
	// Send a miss.
	in <- types.PacketInfo{
		Timestamp: time.Now(),
		SrcIP:     net.ParseIP("1.1.1.1"),
		DstIP:     net.ParseIP("2.2.2.2"),
		SrcPort:   9999,
		DstPort:   9998,
		Protocol:  "tcp",
		Size:      100,
	}

	// Drain output.
	time.Sleep(100 * time.Millisecond)
	for len(out) > 0 {
		<-out
	}

	stats := mapper.Stats()
	if stats.CacheHits != 1 {
		t.Errorf("expected 1 cache hit, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 1 {
		t.Errorf("expected 1 cache miss, got %d", stats.CacheMisses)
	}

	cancel()
}
