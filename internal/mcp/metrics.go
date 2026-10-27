package mcp

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// latencyWindow is how many recent calls per tool the p95 is computed over.
const latencyWindow = 1000

// Metrics aggregates tool calls in memory (reset on restart): calls, errors, bytes, p95 latency,
// and how often results were paginated or refused as too large.
type Metrics struct {
	mu    sync.Mutex
	tools map[string]*toolStat
}

type toolStat struct {
	calls, errors, truncated, paginated int
	bytes                               int64
	ms                                  []int64 // ring of the last latencyWindow durations
	next                                int
}

func NewMetrics() *Metrics { return &Metrics{tools: map[string]*toolStat{}} }

func (m *Metrics) record(tool string, ms int64, text string, isErr bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.tools[tool]
	if s == nil {
		s = &toolStat{}
		m.tools[tool] = s
	}
	s.calls++
	s.bytes += int64(len(text))
	if isErr {
		s.errors++
	}
	if isErr && strings.Contains(text, `"code":"too_large"`) {
		s.truncated++
	}
	if strings.Contains(text, `"next_cursor":"`) {
		s.paginated++
	}
	if len(s.ms) < latencyWindow {
		s.ms = append(s.ms, ms)
	} else {
		s.ms[s.next] = ms
		s.next = (s.next + 1) % latencyWindow
	}
}

func (m *Metrics) snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	tools := map[string]any{}
	var calls, truncated, paginated int
	var bytes int64
	for name, s := range m.tools {
		sorted := slices.Clone(s.ms)
		slices.Sort(sorted)
		tools[name] = map[string]any{"calls": s.calls, "errors": s.errors, "bytes": s.bytes, "mean_bytes": s.bytes / int64(s.calls),
			"p95_ms": sorted[(len(sorted)*95+99)/100-1], "paginated": s.paginated, "too_large": s.truncated}
		calls, truncated, paginated, bytes = calls+s.calls, truncated+s.truncated, paginated+s.paginated, bytes+s.bytes
	}
	out := map[string]any{"tools": tools, "calls": calls, "bytes": bytes}
	if calls > 0 {
		out["too_large_rate"] = float64(truncated) / float64(calls)
		out["paginated_rate"] = float64(paginated) / float64(calls)
	}
	return out
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.snapshot())
}
