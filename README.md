# Ybirá Net

A real-time network monitor that captures TCP/UDP traffic, figures out which process owns each connection, and shows you who's eating your bandwidth. Written in Go, runs on Linux and Windows.

## How it works

Ybirá Net is a pipeline of goroutines connected by channels:

```
┌─────────────┐  chan (buf:1024)  ┌──────────────┐  chan (buf:1024)  ┌─────┐
│   Capture   │───Packet_Info────▶│    Process   │───Flow_Event────▶│ Tee │
│   Engine    │                   │    Mapper    │                   │     │
└─────────────┘                   └──────────────┘                   └──┬──┘
                                                                        │
                                                          ┌─────────────┼─────────────┐
                                                          │ aggChan     │ storeChan   │
                                                          ▼             │             ▼
                                                   ┌────────────┐      │      ┌────────────┐
                                                   │ Aggregator │      │      │   Store    │
                                                   │ (windows)  │      │      │  (SQLite)  │
                                                   └─────┬──────┘      │      └────────────┘
                                                         │             │
                                                         ▼             │
                                                   ┌────────────┐      │
                                                   │ API Server │      │
                                                   │ REST + WS  │◀─────┘
                                                   └────────────┘
```

1. **Capture** — sniffs packets off a network interface via gopacket/libpcap with a `tcp or udp` BPF filter
2. **Mapper** — resolves each packet to a PID and process name using `/proc/net/*` on Linux or the IP Helper API on Windows
3. **Tee** — fans out events to both the aggregator and the store (non-blocking, drops on backpressure)
4. **Aggregator** — keeps per-PID byte counters in sliding windows (60s, 300s, 3600s) using ring buffers
5. **Store** — batches events into SQLite for historical queries
6. **API** — REST + WebSocket for live monitoring and queries

Graceful shutdown via `context.Context` — SIGINT/SIGTERM triggers a 10-second drain deadline.

## Project layout

```
cmd/daemon/       Main daemon service
cmd/cli/          CLI query tool
internal/
  capture/        Packet capture (gopacket/libpcap)
  mapper/         PID resolution (Linux: /proc, Windows: IP Helper API)
  aggregator/     Sliding window counters
  api/            REST + WebSocket server
  store/          SQLite persistence
  logging/        Structured logging (zap)
  config/         YAML + env config loader
  daemon/         Tee fan-out
  types/          Shared data types
web/              Minimal live dashboard
```

## Prerequisites

**Linux / macOS:**
- Go 1.22+
- libpcap-dev (`apt install libpcap-dev` / `dnf install libpcap-devel` / `brew install libpcap`)
- Root privileges

**Windows:**
- Go 1.22+
- [Npcap](https://npcap.com/#download) with "WinPcap API-compatible Mode" enabled
- Administrator privileges

## Build

```bash
# Linux/macOS
go build -o ybira-daemon ./cmd/daemon
go build -o ybira-cli ./cmd/cli

# Windows (no CGO needed — uses golang.org/x/sys/windows)
GOOS=windows CGO_ENABLED=0 go build -o ybira-daemon.exe ./cmd/daemon
GOOS=windows CGO_ENABLED=0 go build -o ybira-cli.exe ./cmd/cli
```

## Usage

### Daemon

```bash
sudo ./ybira-daemon                           # uses ./config.yaml
sudo ./ybira-daemon -config /etc/ybira/config.yaml
```

On Windows, run as Administrator:
```powershell
.\ybira-daemon.exe
.\ybira-daemon.exe -config C:\ybira\config.yaml
```

Npcap must be installed — the daemon exits with an error if `wpcap.dll` can't be loaded.

### CLI

```bash
./ybira-cli stats                              # top 10, last 60s
./ybira-cli stats --window 300 --top 5         # top 5, last 5 min
./ybira-cli stats --addr http://192.168.1.10:8080
```

Output:
```
PID       PROCESS         BYTES
1234      firefox         1048576
5678      curl            524288
```

### Web dashboard

Open `http://localhost:8080/web/` — live-updating via WebSocket.

### REST API

```bash
curl http://localhost:8080/stats?window=60&top=10
```

```json
{
  "window": 60,
  "stats": [
    {"pid": 1234, "process": "firefox", "bytes": 1048576}
  ],
  "meta": {
    "capture_drops": 0,
    "mapper_drops": 0,
    "aggregator_drops": 0,
    "store_drops": 0,
    "timestamp": "2024-01-01T00:00:00Z"
  }
}
```

Valid windows: `60`, `300`, `3600`. Anything else returns 400.

### WebSocket

Connect to `ws://localhost:8080/ws` — pushes top-10 (60s) stats every second. Max 100 concurrent clients.

## Configuration

Loaded from YAML, overridable with `YBIRA_`-prefixed env vars. Missing or broken config file? Defaults are used with a warning.

```yaml
capture:
  interface: "eth0"

api:
  listen: ":8080"

log_level: "info"

store:
  flush_interval: 10
  database_path: "./ybira.db"

mapper:
  cache_refresh_interval: 2
```

| Env var | Overrides | Default |
|---------|-----------|---------|
| `YBIRA_CAPTURE_INTERFACE` | `capture.interface` | `eth0` |
| `YBIRA_API_LISTEN` | `api.listen` | `:8080` |
| `YBIRA_LOG_LEVEL` | `log_level` | `info` |
| `YBIRA_STORE_FLUSH_INTERVAL` | `store.flush_interval` | `10` |
| `YBIRA_STORE_DATABASE_PATH` | `store.database_path` | `./ybira.db` |
| `YBIRA_MAPPER_CACHE_REFRESH_INTERVAL` | `mapper.cache_refresh_interval` | `2` |

## Development

```bash
go test ./...          # run tests
go test -v ./...       # verbose
go fmt ./...           # format
golangci-lint run      # lint (if installed)
```

## Stack

| What | How | Why |
|------|-----|-----|
| Language | Go 1.22+ | Goroutines + channels fit the pipeline model |
| Capture | gopacket + libpcap | Industry standard, BPF support |
| PID mapping | /proc (Linux), IP Helper API (Windows) | No external deps |
| HTTP | net/http | Stdlib, zero overhead |
| WebSocket | gorilla/websocket | De facto Go WS library |
| Database | modernc.org/sqlite | Pure Go, no CGO |
| Logging | go.uber.org/zap | Fast structured logging |
| Config | gopkg.in/yaml.v3 | Standard YAML |

## License

MIT
