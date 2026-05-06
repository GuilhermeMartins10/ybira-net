package capture

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/ybira-net/ybira-net/internal/types"
	"go.uber.org/zap"
)

type CaptureStats struct {
	PacketsCaptured int64
	PacketsDropped  int64
}

type Capturer interface {
	Start(ctx context.Context, out chan<- types.PacketInfo) error
	Stop() error
	Stats() CaptureStats
}

type PacketSource interface {
	Packets() chan gopacket.Packet
}

type HandleOpener interface {
	OpenLive(iface string, snaplen int32, promisc bool, timeout time.Duration) (PacketHandle, error)
}

type PacketHandle interface {
	SetBPFFilter(filter string) error
	NewPacketSource(decoder gopacket.Decoder) PacketSource
	Close()
}

type pcapHandleWrapper struct {
	handle *pcap.Handle
}

func (w *pcapHandleWrapper) SetBPFFilter(filter string) error {
	return w.handle.SetBPFFilter(filter)
}

func (w *pcapHandleWrapper) NewPacketSource(decoder gopacket.Decoder) PacketSource {
	return gopacket.NewPacketSource(w.handle, decoder)
}

func (w *pcapHandleWrapper) Close() {
	w.handle.Close()
}

type defaultHandleOpener struct{}

func (d *defaultHandleOpener) OpenLive(iface string, snaplen int32, promisc bool, timeout time.Duration) (PacketHandle, error) {
	handle, err := pcap.OpenLive(iface, snaplen, promisc, timeout)
	if err != nil {
		return nil, err
	}
	return &pcapHandleWrapper{handle: handle}, nil
}

type LiveCapture struct {
	iface   string
	logger  *zap.Logger
	opener  HandleOpener
	handle  PacketHandle
	stopped atomic.Bool

	capturedCount atomic.Int64
	dropCount     atomic.Int64
}

func NewLiveCapture(iface string, logger *zap.Logger) *LiveCapture {
	return &LiveCapture{
		iface:  iface,
		logger: logger.Named("capture"),
		opener: &defaultHandleOpener{},
	}
}

func NewLiveCaptureWithOpener(iface string, logger *zap.Logger, opener HandleOpener) *LiveCapture {
	return &LiveCapture{
		iface:  iface,
		logger: logger.Named("capture"),
		opener: opener,
	}
}

func (lc *LiveCapture) Start(ctx context.Context, out chan<- types.PacketInfo) error {
	handle, err := lc.openHandle()
	if err != nil {
		return fmt.Errorf("failed to open capture handle: %w", err)
	}
	lc.handle = handle

	source := handle.NewPacketSource(layers.LayerTypeEthernet)
	packets := source.Packets()

	for {
		select {
		case <-ctx.Done():
			lc.handle.Close()
			return nil
		case pkt, ok := <-packets:
			if !ok {
				if lc.stopped.Load() {
					return nil
				}
				lc.logger.Error("packet source closed, attempting reconnection",
					zap.String("interface", lc.iface))
				handle, packets = lc.reconnect(ctx)
				if handle == nil {
					return nil
				}
				lc.handle = handle
				continue
			}

			info, ok := lc.parsePacket(pkt)
			if !ok {
				continue
			}

			lc.capturedCount.Add(1)

			select {
			case out <- info:
			default:
				lc.dropCount.Add(1)
			}
		}
	}
}

func (lc *LiveCapture) Stop() error {
	lc.stopped.Store(true)
	if lc.handle != nil {
		lc.handle.Close()
	}
	return nil
}

func (lc *LiveCapture) Stats() CaptureStats {
	return CaptureStats{
		PacketsCaptured: lc.capturedCount.Load(),
		PacketsDropped:  lc.dropCount.Load(),
	}
}

func (lc *LiveCapture) openHandle() (PacketHandle, error) {
	handle, err := lc.opener.OpenLive(lc.iface, 1600, true, pcap.BlockForever)
	if err != nil {
		return nil, err
	}

	if err := handle.SetBPFFilter("tcp or udp"); err != nil {
		handle.Close()
		return nil, fmt.Errorf("failed to set BPF filter: %w", err)
	}

	return handle, nil
}

func (lc *LiveCapture) reconnect(ctx context.Context) (PacketHandle, chan gopacket.Packet) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
			if lc.stopped.Load() {
				return nil, nil
			}
			lc.logger.Info("attempting reconnection", zap.String("interface", lc.iface))
			handle, err := lc.openHandle()
			if err != nil {
				lc.logger.Error("reconnection failed",
					zap.String("interface", lc.iface),
					zap.Error(err))
				continue
			}
			source := handle.NewPacketSource(layers.LayerTypeEthernet)
			return handle, source.Packets()
		}
	}
}

func (lc *LiveCapture) parsePacket(pkt gopacket.Packet) (types.PacketInfo, bool) {
	var info types.PacketInfo

	if networkLayer := pkt.NetworkLayer(); networkLayer != nil {
		switch nl := networkLayer.(type) {
		case *layers.IPv4:
			info.SrcIP = net.IP(nl.SrcIP).To16()
			info.DstIP = net.IP(nl.DstIP).To16()
		case *layers.IPv6:
			info.SrcIP = net.IP(nl.SrcIP).To16()
			info.DstIP = net.IP(nl.DstIP).To16()
		default:
			return info, false
		}
	} else {
		return info, false
	}

	if transportLayer := pkt.TransportLayer(); transportLayer != nil {
		switch tl := transportLayer.(type) {
		case *layers.TCP:
			info.Protocol = "tcp"
			info.SrcPort = uint16(tl.SrcPort)
			info.DstPort = uint16(tl.DstPort)
		case *layers.UDP:
			info.Protocol = "udp"
			info.SrcPort = uint16(tl.SrcPort)
			info.DstPort = uint16(tl.DstPort)
		default:
			return info, false
		}
	} else {
		return info, false
	}

	info.Timestamp = pkt.Metadata().Timestamp
	if info.Timestamp.IsZero() {
		info.Timestamp = time.Now()
	}
	info.Size = len(pkt.Data())

	return info, true
}
