package capture

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/ybira-net/ybira-net/internal/types"
	"go.uber.org/zap"
	"pgregory.net/rapid"
)

// --- Mock infrastructure ---

// mockPacketSource implements PacketSource for testing.
type mockPacketSource struct {
	ch chan gopacket.Packet
}

func (m *mockPacketSource) Packets() chan gopacket.Packet {
	return m.ch
}

// mockHandle implements PacketHandle for testing.
type mockHandle struct {
	source *mockPacketSource
	closed bool
}

func (m *mockHandle) SetBPFFilter(filter string) error {
	return nil
}

func (m *mockHandle) NewPacketSource(decoder gopacket.Decoder) PacketSource {
	return m.source
}

func (m *mockHandle) Close() {
	m.closed = true
	// Close the packet channel to signal end of packets
	select {
	case <-m.source.ch:
	default:
		close(m.source.ch)
	}
}

// mockHandleOpener implements HandleOpener for testing.
type mockHandleOpener struct {
	handle *mockHandle
	err    error
}

func (m *mockHandleOpener) OpenLive(iface string, snaplen int32, promisc bool, timeout time.Duration) (PacketHandle, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.handle, nil
}

// --- Packet construction helpers ---

// buildTCPPacket creates a serialized TCP/IP packet for testing.
func buildTCPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, payloadSize int) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		SrcIP:    srcIP,
		DstIP:    dstIP,
		Protocol: layers.IPProtocolTCP,
		TTL:      64,
	}
	tcp := &layers.TCP{
		SrcPort: layers.TCPPort(srcPort),
		DstPort: layers.TCPPort(dstPort),
		SYN:     true,
	}
	tcp.SetNetworkLayerForChecksum(ip)

	payload := make([]byte, payloadSize)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	gopacket.SerializeLayers(buf, opts, eth, ip, tcp, gopacket.Payload(payload))

	pkt := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	pkt.Metadata().Timestamp = time.Now()
	return pkt
}

// buildUDPPacket creates a serialized UDP/IP packet for testing.
func buildUDPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, payloadSize int) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		SrcIP:    srcIP,
		DstIP:    dstIP,
		Protocol: layers.IPProtocolUDP,
		TTL:      64,
	}
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(srcPort),
		DstPort: layers.UDPPort(dstPort),
	}
	udp.SetNetworkLayerForChecksum(ip)

	payload := make([]byte, payloadSize)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload(payload))

	pkt := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	pkt.Metadata().Timestamp = time.Now()
	return pkt
}

// buildICMPPacket creates a serialized ICMP/IP packet (non-TCP/UDP) for testing.
func buildICMPPacket(srcIP, dstIP net.IP) gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		SrcIP:    srcIP,
		DstIP:    dstIP,
		Protocol: layers.IPProtocolICMPv4,
		TTL:      64,
	}
	icmp := &layers.ICMPv4{
		TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0),
		Id:       1,
		Seq:      1,
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	gopacket.SerializeLayers(buf, opts, eth, ip, icmp, gopacket.Payload([]byte("ping")))

	pkt := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	pkt.Metadata().Timestamp = time.Now()
	return pkt
}

// buildARPPacket creates a serialized ARP packet (no transport layer) for testing.
func buildARPPacket() gopacket.Packet {
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:       net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		EthernetType: layers.EthernetTypeARP,
	}
	arp := &layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		Protocol:          layers.EthernetTypeIPv4,
		HwAddressSize:     6,
		ProtAddressSize:   4,
		Operation:         layers.ARPRequest,
		SourceHwAddress:   net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		SourceProtAddress: net.IP{192, 168, 1, 1},
		DstHwAddress:      net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		DstProtAddress:    net.IP{192, 168, 1, 2},
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	gopacket.SerializeLayers(buf, opts, eth, arp)

	pkt := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	pkt.Metadata().Timestamp = time.Now()
	return pkt
}

// --- Unit Tests ---

func TestLiveCapture_TCPPacket(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 10)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	out := make(chan types.PacketInfo, 10)

	ctx, cancel := context.WithCancel(context.Background())

	// Send a TCP packet
	pktCh <- buildTCPPacket(net.IP{192, 168, 1, 1}, net.IP{10, 0, 0, 1}, 12345, 80, 100)

	go func() {
		// Wait for the packet to be processed
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := lc.Start(ctx, out)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case info := <-out:
		if info.Protocol != "tcp" {
			t.Errorf("expected protocol tcp, got %s", info.Protocol)
		}
		if info.SrcPort != 12345 {
			t.Errorf("expected src port 12345, got %d", info.SrcPort)
		}
		if info.DstPort != 80 {
			t.Errorf("expected dst port 80, got %d", info.DstPort)
		}
		if info.Size == 0 {
			t.Error("expected non-zero packet size")
		}
	default:
		t.Error("expected a PacketInfo on the output channel")
	}

	stats := lc.Stats()
	if stats.PacketsCaptured != 1 {
		t.Errorf("expected 1 packet captured, got %d", stats.PacketsCaptured)
	}
	if stats.PacketsDropped != 0 {
		t.Errorf("expected 0 packets dropped, got %d", stats.PacketsDropped)
	}
}

func TestLiveCapture_UDPPacket(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 10)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	out := make(chan types.PacketInfo, 10)

	ctx, cancel := context.WithCancel(context.Background())

	pktCh <- buildUDPPacket(net.IP{10, 0, 0, 5}, net.IP{8, 8, 8, 8}, 5353, 53, 50)

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := lc.Start(ctx, out)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case info := <-out:
		if info.Protocol != "udp" {
			t.Errorf("expected protocol udp, got %s", info.Protocol)
		}
		if info.SrcPort != 5353 {
			t.Errorf("expected src port 5353, got %d", info.SrcPort)
		}
		if info.DstPort != 53 {
			t.Errorf("expected dst port 53, got %d", info.DstPort)
		}
	default:
		t.Error("expected a PacketInfo on the output channel")
	}
}

func TestLiveCapture_ICMPPacketFiltered(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 10)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	out := make(chan types.PacketInfo, 10)

	ctx, cancel := context.WithCancel(context.Background())

	// Send an ICMP packet (should be filtered out)
	pktCh <- buildICMPPacket(net.IP{192, 168, 1, 1}, net.IP{192, 168, 1, 2})

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := lc.Start(ctx, out)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case info := <-out:
		t.Errorf("expected no PacketInfo for ICMP, got %+v", info)
	default:
		// Expected: no output for ICMP
	}

	stats := lc.Stats()
	if stats.PacketsCaptured != 0 {
		t.Errorf("expected 0 packets captured for ICMP, got %d", stats.PacketsCaptured)
	}
}

func TestLiveCapture_ARPPacketFiltered(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 10)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	out := make(chan types.PacketInfo, 10)

	ctx, cancel := context.WithCancel(context.Background())

	// Send an ARP packet (should be filtered out)
	pktCh <- buildARPPacket()

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := lc.Start(ctx, out)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case info := <-out:
		t.Errorf("expected no PacketInfo for ARP, got %+v", info)
	default:
		// Expected: no output for ARP
	}
}

func TestLiveCapture_Backpressure(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 20)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	// Use a channel with buffer size 1 to trigger backpressure
	out := make(chan types.PacketInfo, 1)

	ctx, cancel := context.WithCancel(context.Background())

	// Send 5 TCP packets - only 1 should fit in the buffer
	for i := 0; i < 5; i++ {
		pktCh <- buildTCPPacket(net.IP{192, 168, 1, 1}, net.IP{10, 0, 0, 1}, uint16(1000+i), 80, 100)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err := lc.Start(ctx, out)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	stats := lc.Stats()
	if stats.PacketsCaptured != 5 {
		t.Errorf("expected 5 packets captured, got %d", stats.PacketsCaptured)
	}
	// At least some packets should be dropped due to full channel
	if stats.PacketsDropped == 0 {
		t.Error("expected some packets to be dropped due to backpressure")
	}
}

func TestLiveCapture_ContextCancellation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 10)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	out := make(chan types.PacketInfo, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- lc.Start(ctx, out)
	}()

	// Cancel immediately
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error on cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

func TestLiveCapture_Stop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pktCh := make(chan gopacket.Packet, 10)
	source := &mockPacketSource{ch: pktCh}
	handle := &mockHandle{source: source}
	opener := &mockHandleOpener{handle: handle}

	lc := NewLiveCaptureWithOpener("eth0", logger, opener)
	out := make(chan types.PacketInfo, 10)

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- lc.Start(ctx, out)
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	err := lc.Stop()
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error after Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

// --- Property-Based Tests ---

// **Validates: Requirements 1.1, 1.6**
// Property: For any non-TCP/UDP packet, no PacketInfo is emitted;
// for any TCP/UDP packet, exactly one PacketInfo with all fields populated.
func TestProperty_PacketFiltering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		logger, _ := zap.NewDevelopment()

		// Generate random packet parameters
		srcIPBytes := rapid.SliceOfN(rapid.Byte(), 4, 4).Draw(t, "srcIP")
		dstIPBytes := rapid.SliceOfN(rapid.Byte(), 4, 4).Draw(t, "dstIP")
		srcPort := rapid.Uint16().Draw(t, "srcPort")
		dstPort := rapid.Uint16().Draw(t, "dstPort")
		payloadSize := rapid.IntRange(0, 1000).Draw(t, "payloadSize")

		srcIP := net.IP(srcIPBytes)
		dstIP := net.IP(dstIPBytes)

		// Choose protocol: tcp, udp, or other (icmp, arp)
		protoChoice := rapid.IntRange(0, 3).Draw(t, "protocol")

		pktCh := make(chan gopacket.Packet, 10)
		source := &mockPacketSource{ch: pktCh}
		handle := &mockHandle{source: source}
		opener := &mockHandleOpener{handle: handle}

		lc := NewLiveCaptureWithOpener("eth0", logger, opener)
		out := make(chan types.PacketInfo, 10)

		ctx, cancel := context.WithCancel(context.Background())

		var isTCPOrUDP bool
		switch protoChoice {
		case 0: // TCP
			pktCh <- buildTCPPacket(srcIP, dstIP, srcPort, dstPort, payloadSize)
			isTCPOrUDP = true
		case 1: // UDP
			pktCh <- buildUDPPacket(srcIP, dstIP, srcPort, dstPort, payloadSize)
			isTCPOrUDP = true
		case 2: // ICMP
			pktCh <- buildICMPPacket(srcIP, dstIP)
			isTCPOrUDP = false
		case 3: // ARP
			pktCh <- buildARPPacket()
			isTCPOrUDP = false
		}

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		lc.Start(ctx, out)

		if isTCPOrUDP {
			// Exactly one PacketInfo should be emitted with all fields populated
			select {
			case info := <-out:
				if info.SrcIP == nil {
					t.Fatal("SrcIP is nil for TCP/UDP packet")
				}
				if info.DstIP == nil {
					t.Fatal("DstIP is nil for TCP/UDP packet")
				}
				if info.Protocol != "tcp" && info.Protocol != "udp" {
					t.Fatalf("Protocol should be tcp or udp, got %q", info.Protocol)
				}
				if info.Size == 0 {
					t.Fatal("Size should be non-zero for TCP/UDP packet")
				}
				if info.Timestamp.IsZero() {
					t.Fatal("Timestamp should be non-zero for TCP/UDP packet")
				}
				// Verify correct protocol
				if protoChoice == 0 && info.Protocol != "tcp" {
					t.Fatalf("expected tcp, got %s", info.Protocol)
				}
				if protoChoice == 1 && info.Protocol != "udp" {
					t.Fatalf("expected udp, got %s", info.Protocol)
				}
				// Verify ports match
				if info.SrcPort != srcPort {
					t.Fatalf("expected src port %d, got %d", srcPort, info.SrcPort)
				}
				if info.DstPort != dstPort {
					t.Fatalf("expected dst port %d, got %d", dstPort, info.DstPort)
				}
			default:
				t.Fatal("expected exactly one PacketInfo for TCP/UDP packet, got none")
			}

			// No second packet should be emitted
			select {
			case extra := <-out:
				t.Fatalf("expected no more PacketInfo, got %+v", extra)
			default:
				// Good
			}
		} else {
			// No PacketInfo should be emitted for non-TCP/UDP
			select {
			case info := <-out:
				t.Fatalf("expected no PacketInfo for non-TCP/UDP packet, got %+v", info)
			default:
				// Good: no output
			}
		}
	})
}
