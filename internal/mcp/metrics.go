package mcp

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// latencyWindow is how many recent calls per tool the p95 is computed over.
const latencyWindow = 1000

const (
	// connectedWindow is how recently a client must have sent a request to count as connected. MCP over
	// HTTP is stateless, so "connected" means "active lately", not an open socket.
	connectedWindow = 5 * time.Minute
	maxClients      = 200
	recentCalls     = 200
)

// Client is one token or user that talked to /mcp, with what it identified itself as.
type Client struct {
	Actor     string `json:"actor"`
	Label     string `json:"label,omitempty"`      // token name and owner, when known
	Name      string `json:"name,omitempty"`       // clientInfo from initialize, e.g. "claude-code 2.1"
	UserAgent string `json:"user_agent,omitempty"` // as sent, capped
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
	Requests  int    `json:"requests"`
	Calls     int    `json:"calls"`
	Errors    int    `json:"errors"`
	LastTool  string `json:"last_tool,omitempty"`
	Connected bool   `json:"connected"`
}

// Call is one tool call in the recent-calls log.
type Call struct {
	TS    int64  `json:"ts"`
	Actor string `json:"actor"`
	Tool  string `json:"tool"`
	MS    int64  `json:"ms"`
	Bytes int    `json:"bytes"`
	Error string `json:"error,omitempty"` // error code, empty when the call succeeded
}

// Metrics aggregates tool calls in memory (reset on restart): calls, errors, bytes, p95 latency,
// how often results were paginated or refused as too large, who connected, and the latest calls.
type Metrics struct {
	mu      sync.Mutex
	tools   map[string]*toolStat
	clients map[string]*Client
	recent  []Call // ring of the last recentCalls calls
	next    int
	started time.Time
	now     func() time.Time
}

type toolStat struct {
	calls, errors, truncated, paginated int
	bytes                               int64
	ms                                  []int64 // ring of the last latencyWindow durations
	next                                int
}

func NewMetrics() *Metrics {
	return &Metrics{tools: map[string]*toolStat{}, clients: map[string]*Client{}, started: time.Now(), now: time.Now}
}

// seen notes a request from actor. label is only asked for when the actor is new.
func (m *Metrics) seen(actor, userAgent string, label func() string) {
	lbl := m.labelFor(actor, label)
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.client(actor, lbl)
	c.Requests++
	if len(userAgent) > 120 {
		userAgent = userAgent[:120]
	}
	c.UserAgent = userAgent
}

// identify stores the clientInfo a client sent and reports whether it changed (a new or different client).
func (m *Metrics) identify(actor, name, version string, label func() string) bool {
	lbl := m.labelFor(actor, label)
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.client(actor, lbl)
	n := strings.TrimSpace(name + " " + version)
	if len(n) > 120 {
		n = n[:120]
	}
	changed := c.Name != n
	c.Name = n
	return changed
}

// labelFor resolves the label of a not yet known actor. It may hit the database, so it runs outside the lock.
func (m *Metrics) labelFor(actor string, label func() string) string {
	m.mu.Lock()
	_, known := m.clients[actor]
	m.mu.Unlock()
	if known || label == nil {
		return ""
	}
	return label()
}

// client returns the entry for actor, creating it (and evicting the stalest when full). Caller holds mu.
func (m *Metrics) client(actor, label string) *Client {
	now := m.now().UnixMilli()
	c := m.clients[actor]
	if c == nil {
		if len(m.clients) >= maxClients {
			var oldest string
			for k, o := range m.clients {
				if oldest == "" || o.LastSeen < m.clients[oldest].LastSeen {
					oldest = k
				}
			}
			delete(m.clients, oldest)
		}
		c = &Client{Actor: actor, FirstSeen: now}
		c.Label = label
		m.clients[actor] = c
	}
	c.LastSeen = now
	return c
}

func (m *Metrics) record(actor, tool string, ms int64, text string, isErr bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.client(actor, "")
	c.Calls++
	c.LastTool = tool
	call := Call{TS: m.now().UnixMilli(), Actor: actor, Tool: tool, MS: ms, Bytes: len(text)}
	if isErr {
		c.Errors++
		call.Error = errorCode(text)
	}
	if len(m.recent) < recentCalls {
		m.recent = append(m.recent, call)
	} else {
		m.recent[m.next] = call
		m.next = (m.next + 1) % recentCalls
	}
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

// errorCode pulls the code out of an error result ({"error":{"code":"too_large",...}}), else "error".
func errorCode(text string) string {
	const key = `"code":"`
	if i := strings.Index(text, key); i >= 0 {
		rest := text[i+len(key):]
		if j := strings.IndexByte(rest, '"'); j > 0 && j <= 40 {
			return rest[:j]
		}
	}
	return "error"
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
	now := m.now()
	clients := make([]Client, 0, len(m.clients))
	connected := 0
	for _, c := range m.clients {
		c := *c
		c.Connected = now.Sub(time.UnixMilli(c.LastSeen)) < connectedWindow
		if c.Connected {
			connected++
		}
		clients = append(clients, c)
	}
	slices.SortFunc(clients, func(a, b Client) int { return int(b.LastSeen - a.LastSeen) })
	recent := make([]Call, 0, len(m.recent)) // newest first
	for i := range m.recent {
		recent = append(recent, m.recent[(m.next+len(m.recent)-1-i)%len(m.recent)])
	}
	out := map[string]any{"tools": tools, "calls": calls, "bytes": bytes, "status": "ok", "started_at": m.started.UnixMilli(),
		"connected": connected, "clients": clients, "recent": recent}
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
