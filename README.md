# 🌳 Ybirá Net  
> Rooted intelligence for networks — monitor, sense, and protect.

[![Go Version](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go)](https://go.dev/)  
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)  
[![Build](https://github.com/GuilhermeMartins10/ybira-net/actions/workflows/build.yml/badge.svg)](https://github.com/GuilhermeMartins10/ybira-net/actions)  
[![Status](https://img.shields.io/badge/status-alpha-orange)]()

---

## 🧠 Overview
**Ybirá Net** is a real-time network and process monitor built in Go.  
Inspired by the Tupi-Guarani word *Ybirá* (“tree”), it connects system roots to network branches — observing every flow with precision and efficiency.

> Cross-platform • High-performance • Extensible by plugins

---

## ⚡ Features (MVP)
- 🔍 **Packet capture** via `libpcap` (`gopacket`)  
- 🔗 **Process mapping** using `/proc/net` and `/proc/<pid>` inspection  
- 📊 **Sliding-window aggregator** (60 s / 300 s / 3600 s)  
- 🌐 **REST API + WebSocket** for real-time dashboards  
- 🪵 **Structured logging** using `zap`  
- 💾 **SQLite** local storage (stats + history)  
- 🧩 **Modular architecture** ready for plugins  
- 🔒 **Secure by default** — local-only API, no payload storage  
- 🧱 **Cross-platform build** (Linux / macOS; Windows partial)

---

## 🏗️ Architecture (MVP)

```
ybira-net/
 ├─ cmd/
 │   └─ daemon/
 │        └─ main.go
 ├─ internal/
 │   ├─ capture/
 │   │    ├─ capture.go           # interface genérica
 │   │    ├─ capture_linux.go     # implementação Linux
 │   │    └─ capture_windows.go   # implementação Windows
 │   ├─ mapper/
 │   │    ├─ mapper.go
 │   │    ├─ mapper_linux.go
 │   │    └─ mapper_windows.go
 │   ├─ aggregator/
 │   ├─ api/
 │   ├─ store/
 │   ├─ logging/
 │   └─ config/
 ├─ web/
 ├─ go.mod
 └─ README.md
```

---

## 🚧 Phase 2 – Next Milestones

| Area | Goal | Description |
|------|------|-------------|
| **Capture** | eBPF Agent | Replace PCAP with per-PID eBPF accounting (Linux). |
| **Windows Support** | ETW/Npcap | Accurate per-process metrics on Windows. |
| **Plugins** | Plugin System | gRPC plugin API for detectors & rules. |
| **Detectors** | Smart Alerts | Detect new remote hosts, spikes, and anomalies. |
| **Storage** | Postgres backend | Optional remote DB for multi-host deployments. |
| **UI** | Electron/Tauri desktop | Native UI with auth & history charts. |
| **Observability** | Prometheus metrics | Export internal counters and latency stats. |
| **Security** | API auth & TLS | Token-based auth and HTTPS server mode. |

---

## 🚀 Getting Started

### Prerequisites
- Go **1.22+**
- libpcap (`sudo apt install libpcap-dev` on Debian/Ubuntu)
- Root/admin privileges for packet capture
- (Optional) SQLite CLI for debugging

### Clone and Build
```bash
git clone https://github.com/GuilhermeMartins10/ybira-net.git
cd ybira-net
go mod download
sudo go run ./cmd/daemon
```

### View Stats
- Open [`http://127.0.0.1:5000/stats`](http://127.0.0.1:5000/stats) → JSON output  
- Open `web/index.html` in a browser for live Chart.js UI

**Example API Response:**
```json
{
  "window_seconds": 60,
  "top": [
    {"id": "123", "name": "123:firefox", "bytes": 128000},
    {"id": "456", "name": "456:sshd", "bytes": 32000}
  ]
}
```

---

## ⚙️ Configuration (YAML)

```yaml
capture:
  mode: auto        # auto | pcap | ebpf | etw
  iface: eth0

aggregator:
  windows: [60, 300, 3600]

api:
  listen: "127.0.0.1:5000"

storage:
  driver: sqlite
  path: "./data/ybira.db"

logging:
  level: info
```

---

## 🧱 Development Guide

### Code Style
```bash
go fmt ./...
golangci-lint run
```

### Run Tests
```bash
go test ./... -v
```

### Benchmarks
```bash
go test -bench . -benchmem
```

### Profiling
```bash
go run ./cmd/daemon --pprof :6060
```
Access `http://localhost:6060/debug/pprof/` for profiling.

---

## 🔍 Tech Stack
| Layer | Tool / Lib |
|-------|-------------|
| **Language** | Go 1.22+ |
| **Packet Capture** | gopacket / libpcap → eBPF (phase 2) |
| **Storage** | SQLite → Postgres (phase 2) |
| **API** | net/http + gorilla/websocket |
| **UI** | Chart.js (static web dashboard) |
| **Config** | viper |
| **Logging** | zap |
| **Metrics** | Prometheus (phase 2) |

---

## 🧭 Roadmap

1. ✅ **MVP** – Local packet capture, aggregator, REST/WS API, basic UI  
2. 🚧 **Phase 2** – eBPF capture, plugin system, detectors, desktop UI  
3. 🌐 **Phase 3** – Distributed agents, central dashboard, advanced analytics  

---

## 🤝 Contributing
Pull requests are welcome!  
Follow the [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

---

## 📜 License
MIT © 2026 Ybirá Net Project

---

### 🌿 “Rooted in Brazilian wisdom, built for the world.”
