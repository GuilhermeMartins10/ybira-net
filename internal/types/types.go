package types

import (
	"net"
	"time"
)

type PacketInfo struct {
	Timestamp time.Time
	SrcIP     net.IP
	DstIP     net.IP
	SrcPort   uint16
	DstPort   uint16
	Protocol  string
	Size      int
}

type FlowEvent struct {
	Timestamp time.Time
	PID       int
	Process   string
	Bytes     int
	SrcIP     net.IP
	DstIP     net.IP
	SrcPort   uint16
	DstPort   uint16
	Protocol  string
}

type AggregatedStat struct {
	PID        int
	Process    string
	TotalBytes int64
	Window     int
}
