package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type statEntry struct {
	PID     int    `json:"pid"`
	Process string `json:"process"`
	Bytes   int64  `json:"bytes"`
}

type statsResponse struct {
	Window int         `json:"window"`
	Stats  []statEntry `json:"stats"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "stats" {
		fmt.Fprintln(stderr, "usage: ybira-cli stats [--window N] [--top N] [--addr URL]")
		return 1
	}

	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	window := fs.Int("window", 60, "time window in seconds (60, 300, 3600)")
	top := fs.Int("top", 10, "number of top processes to display")
	addr := fs.String("addr", "http://localhost:8080", "daemon address")

	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	url := fmt.Sprintf("%s/stats?window=%d&top=%d", strings.TrimRight(*addr, "/"), *window, *top)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(stderr, "error: failed to connect to daemon: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(stderr, "error: failed to read response: %v\n", err)
		return 1
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "error: daemon returned status %d: %s\n", resp.StatusCode, string(body))
		return 1
	}

	var statsResp statsResponse
	if err := json.Unmarshal(body, &statsResp); err != nil {
		fmt.Fprintf(stderr, "error: failed to parse response: %v\n", err)
		return 1
	}

	fmt.Fprint(stdout, formatTable(statsResp.Stats))
	return 0
}

func formatTable(stats []statEntry) string {
	if len(stats) == 0 {
		return "PID | PROCESS | BYTES\n"
	}

	pidWidth := len("PID")
	procWidth := len("PROCESS")
	bytesWidth := len("BYTES")

	for _, s := range stats {
		pw := len(fmt.Sprintf("%d", s.PID))
		if pw > pidWidth {
			pidWidth = pw
		}
		if len(s.Process) > procWidth {
			procWidth = len(s.Process)
		}
		bw := len(fmt.Sprintf("%d", s.Bytes))
		if bw > bytesWidth {
			bytesWidth = bw
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-*s | %-*s | %-*s\n", pidWidth, "PID", procWidth, "PROCESS", bytesWidth, "BYTES"))
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("%-*d | %-*s | %-*d\n", pidWidth, s.PID, procWidth, s.Process, bytesWidth, s.Bytes))
	}

	return sb.String()
}
