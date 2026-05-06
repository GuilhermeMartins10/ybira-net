package mapper

import (
	"encoding/binary"
	"testing"
)

func TestParseTcpTable_Basic(t *testing.T) {
	// Build a buffer with 1 TCP entry:
	// IP 192.168.1.100:8080 -> 10.0.0.1:443, PID 1234
	data := make([]byte, 4+tcpRowSize)

	// dwNumEntries = 1
	binary.LittleEndian.PutUint32(data[0:4], 1)

	row := data[4:]
	// State (ignored for parsing)
	binary.LittleEndian.PutUint32(row[0:4], 5) // ESTABLISHED

	// LocalAddr: 192.168.1.100 in network byte order
	row[4] = 192
	row[5] = 168
	row[6] = 1
	row[7] = 100

	// LocalPort: 8080 in network byte order (first 2 bytes of 4-byte field)
	row[8] = 0x1F // 8080 = 0x1F90
	row[9] = 0x90
	row[10] = 0
	row[11] = 0

	// RemoteAddr: 10.0.0.1 in network byte order
	row[12] = 10
	row[13] = 0
	row[14] = 0
	row[15] = 1

	// RemotePort: 443 in network byte order (first 2 bytes of 4-byte field)
	row[16] = 0x01 // 443 = 0x01BB
	row[17] = 0xBB
	row[18] = 0
	row[19] = 0

	// OwningPid: 1234
	binary.LittleEndian.PutUint32(row[20:24], 1234)

	processNames := map[uint32]string{
		1234: "myapp.exe",
	}

	events := parseTcpTable(data, processNames)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Protocol != "tcp" {
		t.Errorf("Protocol = %q, want %q", e.Protocol, "tcp")
	}
	if e.LocalIP != "192.168.1.100" {
		t.Errorf("LocalIP = %q, want %q", e.LocalIP, "192.168.1.100")
	}
	if e.LocalPort != 8080 {
		t.Errorf("LocalPort = %d, want %d", e.LocalPort, 8080)
	}
	if e.RemoteIP != "10.0.0.1" {
		t.Errorf("RemoteIP = %q, want %q", e.RemoteIP, "10.0.0.1")
	}
	if e.RemotePort != 443 {
		t.Errorf("RemotePort = %d, want %d", e.RemotePort, 443)
	}
	if e.PID != 1234 {
		t.Errorf("PID = %d, want %d", e.PID, 1234)
	}
	if e.ProcessName != "myapp.exe" {
		t.Errorf("ProcessName = %q, want %q", e.ProcessName, "myapp.exe")
	}
}

func TestParseTcpTable_SkipsPID0(t *testing.T) {
	data := make([]byte, 4+tcpRowSize)
	binary.LittleEndian.PutUint32(data[0:4], 1)

	row := data[4:]
	// LocalAddr: 127.0.0.1
	row[4] = 127
	row[5] = 0
	row[6] = 0
	row[7] = 1
	// LocalPort: 80
	row[8] = 0x00
	row[9] = 0x50
	// RemoteAddr: 0.0.0.0
	// RemotePort: 0
	// OwningPid: 0
	binary.LittleEndian.PutUint32(row[20:24], 0)

	events := parseTcpTable(data, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events (PID 0 skipped), got %d", len(events))
	}
}

func TestParseTcpTable_UnknownProcessName(t *testing.T) {
	data := make([]byte, 4+tcpRowSize)
	binary.LittleEndian.PutUint32(data[0:4], 1)

	row := data[4:]
	row[4] = 10
	row[5] = 0
	row[6] = 0
	row[7] = 1
	row[8] = 0x00
	row[9] = 0x50 // port 80
	binary.LittleEndian.PutUint32(row[20:24], 9999)

	// Empty process names map — should get "unknown"
	events := parseTcpTable(data, map[uint32]string{})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ProcessName != "unknown" {
		t.Errorf("ProcessName = %q, want %q", events[0].ProcessName, "unknown")
	}
}

func TestParseTcpTable_TruncatedBuffer(t *testing.T) {
	// Claims 2 entries but only has data for 1
	data := make([]byte, 4+tcpRowSize)
	binary.LittleEndian.PutUint32(data[0:4], 2)

	row := data[4:]
	row[4] = 192
	row[5] = 168
	row[6] = 0
	row[7] = 1
	row[8] = 0x00
	row[9] = 0x50
	binary.LittleEndian.PutUint32(row[20:24], 42)

	events := parseTcpTable(data, map[uint32]string{42: "test"})
	// Should parse the 1 valid entry and stop gracefully
	if len(events) != 1 {
		t.Fatalf("expected 1 event from truncated buffer, got %d", len(events))
	}
}

func TestParseTcpTable_EmptyBuffer(t *testing.T) {
	// Too short to even read count
	events := parseTcpTable([]byte{1, 2}, nil)
	if events != nil {
		t.Fatalf("expected nil for too-short buffer, got %v", events)
	}

	// Zero entries
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data[0:4], 0)
	events = parseTcpTable(data, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for zero-entry table, got %d", len(events))
	}
}

func TestParseUdpTable_Basic(t *testing.T) {
	// Build a buffer with 1 UDP entry:
	// IP 172.16.0.5:53, PID 500
	data := make([]byte, 4+udpRowSize)

	// dwNumEntries = 1
	binary.LittleEndian.PutUint32(data[0:4], 1)

	row := data[4:]
	// LocalAddr: 172.16.0.5 in network byte order
	row[0] = 172
	row[1] = 16
	row[2] = 0
	row[3] = 5

	// LocalPort: 53 in network byte order (first 2 bytes of 4-byte field)
	row[4] = 0x00 // 53 = 0x0035
	row[5] = 0x35
	row[6] = 0
	row[7] = 0

	// OwningPid: 500
	binary.LittleEndian.PutUint32(row[8:12], 500)

	processNames := map[uint32]string{
		500: "dns.exe",
	}

	events := parseUdpTable(data, processNames)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Protocol != "udp" {
		t.Errorf("Protocol = %q, want %q", e.Protocol, "udp")
	}
	if e.LocalIP != "172.16.0.5" {
		t.Errorf("LocalIP = %q, want %q", e.LocalIP, "172.16.0.5")
	}
	if e.LocalPort != 53 {
		t.Errorf("LocalPort = %d, want %d", e.LocalPort, 53)
	}
	if e.RemoteIP != "0.0.0.0" {
		t.Errorf("RemoteIP = %q, want %q", e.RemoteIP, "0.0.0.0")
	}
	if e.RemotePort != 0 {
		t.Errorf("RemotePort = %d, want %d", e.RemotePort, 0)
	}
	if e.PID != 500 {
		t.Errorf("PID = %d, want %d", e.PID, 500)
	}
	if e.ProcessName != "dns.exe" {
		t.Errorf("ProcessName = %q, want %q", e.ProcessName, "dns.exe")
	}
}

func TestParseUdpTable_SkipsPID0(t *testing.T) {
	data := make([]byte, 4+udpRowSize)
	binary.LittleEndian.PutUint32(data[0:4], 1)

	row := data[4:]
	row[0] = 0
	row[1] = 0
	row[2] = 0
	row[3] = 0
	row[4] = 0x00
	row[5] = 0x35
	// OwningPid: 0
	binary.LittleEndian.PutUint32(row[8:12], 0)

	events := parseUdpTable(data, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events (PID 0 skipped), got %d", len(events))
	}
}

func TestParseUdpTable_TruncatedBuffer(t *testing.T) {
	// Claims 3 entries but only has data for 2
	data := make([]byte, 4+2*udpRowSize)
	binary.LittleEndian.PutUint32(data[0:4], 3)

	// Entry 1
	row1 := data[4:]
	row1[0] = 10
	row1[1] = 0
	row1[2] = 0
	row1[3] = 1
	row1[4] = 0x00
	row1[5] = 0x35
	binary.LittleEndian.PutUint32(row1[8:12], 100)

	// Entry 2
	row2 := data[4+udpRowSize:]
	row2[0] = 10
	row2[1] = 0
	row2[2] = 0
	row2[3] = 2
	row2[4] = 0x01
	row2[5] = 0xBB // port 443
	binary.LittleEndian.PutUint32(row2[8:12], 200)

	events := parseUdpTable(data, map[uint32]string{100: "a", 200: "b"})
	if len(events) != 2 {
		t.Fatalf("expected 2 events from truncated buffer, got %d", len(events))
	}
}

func TestParseUdpTable_EmptyBuffer(t *testing.T) {
	events := parseUdpTable([]byte{}, nil)
	if events != nil {
		t.Fatalf("expected nil for empty buffer, got %v", events)
	}

	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data[0:4], 0)
	events = parseUdpTable(data, nil)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for zero-entry table, got %d", len(events))
	}
}

func TestParseTcpTable_MultipleEntries(t *testing.T) {
	// 2 entries, one with PID 0 (skipped), one valid
	data := make([]byte, 4+2*tcpRowSize)
	binary.LittleEndian.PutUint32(data[0:4], 2)

	// Entry 1: PID 0 (should be skipped)
	row1 := data[4:]
	row1[4] = 127
	row1[5] = 0
	row1[6] = 0
	row1[7] = 1
	row1[8] = 0x00
	row1[9] = 0x50
	binary.LittleEndian.PutUint32(row1[20:24], 0)

	// Entry 2: PID 5678
	row2 := data[4+tcpRowSize:]
	row2[4] = 10
	row2[5] = 1
	row2[6] = 2
	row2[7] = 3
	row2[8] = 0x22 // port 8888 = 0x22B8
	row2[9] = 0xB8
	row2[12] = 8
	row2[13] = 8
	row2[14] = 8
	row2[15] = 8
	row2[16] = 0x00 // port 53
	row2[17] = 0x35
	binary.LittleEndian.PutUint32(row2[20:24], 5678)

	events := parseTcpTable(data, map[uint32]string{5678: "chrome.exe"})
	if len(events) != 1 {
		t.Fatalf("expected 1 event (PID 0 skipped), got %d", len(events))
	}

	e := events[0]
	if e.LocalIP != "10.1.2.3" {
		t.Errorf("LocalIP = %q, want %q", e.LocalIP, "10.1.2.3")
	}
	if e.LocalPort != 8888 {
		t.Errorf("LocalPort = %d, want %d", e.LocalPort, 8888)
	}
	if e.RemoteIP != "8.8.8.8" {
		t.Errorf("RemoteIP = %q, want %q", e.RemoteIP, "8.8.8.8")
	}
	if e.RemotePort != 53 {
		t.Errorf("RemotePort = %d, want %d", e.RemotePort, 53)
	}
	if e.PID != 5678 {
		t.Errorf("PID = %d, want %d", e.PID, 5678)
	}
	if e.ProcessName != "chrome.exe" {
		t.Errorf("ProcessName = %q, want %q", e.ProcessName, "chrome.exe")
	}
}
