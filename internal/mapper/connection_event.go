package mapper

type ConnectionEvent struct {
	Protocol    string
	LocalIP     string
	LocalPort   uint16
	RemoteIP    string
	RemotePort  uint16
	PID         int
	ProcessName string
}

type ProcReader interface {
	ReadConnections() ([]ConnectionEvent, error)
}
