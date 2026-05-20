package replay

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResourceSample is a single observation of container resource usage.
type ResourceSample struct {
	Timestamp time.Time `json:"ts"`
	CPUPct    float64   `json:"cpuPct"`
	MemBytes  int64     `json:"memBytes"`
	MemPct    float64   `json:"memPct"`
	NetRxKB   float64   `json:"netRxKB"`
	NetTxKB   float64   `json:"netTxKB"`
	BlkReadKB float64   `json:"blkReadKB"`
	BlkWriteKB float64  `json:"blkWriteKB"`
}

// ResourceProfile is the full profile collected during a replay.
type ResourceProfile struct {
	Container   string           `json:"container"`
	Runtime     string           `json:"runtime"`
	StartedAt   time.Time        `json:"startedAt"`
	FinishedAt  time.Time        `json:"finishedAt"`
	Samples     []ResourceSample `json:"samples"`
	PeakCPUPct  float64          `json:"peakCpuPct"`
	PeakMemBytes int64           `json:"peakMemBytes"`
}

// profileLoop polls the container runtime's `stats` once per second and
// appends ResourceSample entries until ctx is done.
func profileLoop(ctx context.Context, runtime, container string) *ResourceProfile {
	prof := &ResourceProfile{
		Container: container, Runtime: runtime, StartedAt: time.Now().UTC(),
	}
	var mu sync.Mutex
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			prof.FinishedAt = time.Now().UTC()
			return prof
		case <-tick.C:
			s, err := pollStatsOnce(runtime, container)
			if err != nil {
				continue
			}
			mu.Lock()
			prof.Samples = append(prof.Samples, *s)
			if s.CPUPct > prof.PeakCPUPct {
				prof.PeakCPUPct = s.CPUPct
			}
			if s.MemBytes > prof.PeakMemBytes {
				prof.PeakMemBytes = s.MemBytes
			}
			mu.Unlock()
		}
	}
}

// pollStatsOnce runs `<runtime> stats --no-stream --format=json <container>`
// and returns one parsed sample.
func pollStatsOnce(runtime, container string) (*ResourceSample, error) {
	cmd := exec.Command(runtime, "stats", "--no-stream", "--format", "{{json .}}", container)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil, errors.New("empty stats output")
	}
	var raw struct {
		CPUPerc string `json:"CPUPerc"`
		MemUsage string `json:"MemUsage"`
		MemPerc string `json:"MemPerc"`
		NetIO   string `json:"NetIO"`
		BlockIO string `json:"BlockIO"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, err
	}
	s := &ResourceSample{Timestamp: time.Now().UTC()}
	s.CPUPct = parsePct(raw.CPUPerc)
	s.MemPct = parsePct(raw.MemPerc)
	s.MemBytes = parseHumanBytesFirst(raw.MemUsage)
	s.NetRxKB, s.NetTxKB = parsePairKB(raw.NetIO)
	s.BlkReadKB, s.BlkWriteKB = parsePairKB(raw.BlockIO)
	return s, nil
}

// parsePct parses "23.45%" → 23.45.
func parsePct(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" || s == "--" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseHumanBytesFirst parses "120MiB / 1.95GiB" → bytes(120MiB).
func parseHumanBytesFirst(s string) int64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 0 {
		return 0
	}
	return parseHumanBytes(strings.TrimSpace(parts[0]))
}

// parsePairKB parses "1.2kB / 3.4kB" → (1.2, 3.4) in KB.
func parsePairKB(s string) (float64, float64) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	return float64(parseHumanBytes(strings.TrimSpace(parts[0]))) / 1024.0,
		float64(parseHumanBytes(strings.TrimSpace(parts[1]))) / 1024.0
}

// parseHumanBytes parses "1.2kB", "3.4MiB", "5GB" etc. into bytes.
func parseHumanBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" {
		return 0
	}
	mults := []struct {
		suffix string
		mult   float64
	}{
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
		{"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3},
		{"B", 1},
	}
	for _, m := range mults {
		if strings.HasSuffix(s, m.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, m.suffix))
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(v * m.mult)
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v)
}
