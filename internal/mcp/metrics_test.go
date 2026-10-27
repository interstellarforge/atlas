package mcp

import (
	"testing"
	"time"
)

func TestMetricsClientsAndRecent(t *testing.T) {
	m := NewMetrics()
	clock := time.Now()
	m.now = func() time.Time { return clock }
	m.seen("token:pat-1", "agent/1", func() string { return "ci (Kees)" })
	m.identify("token:pat-1", "claude-code", "2.1", nil)
	m.record("token:pat-1", "items_search", 12, `{"ok":true}`, false)
	m.record("token:pat-1", "item_get", 3, `{"error":{"code":"not_found"}}`, true)
	for i := 0; i < recentCalls+5; i++ { // the ring keeps only the newest calls, newest first
		m.record("token:pat-2", "board_get", int64(i), "x", false)
	}

	snap := m.snapshot()
	clients := snap["clients"].([]Client)
	if len(clients) != 2 || snap["connected"] != 2 {
		t.Fatalf("clients: %+v connected=%v", clients, snap["connected"])
	}
	var c Client
	for _, x := range clients {
		if x.Actor == "token:pat-1" {
			c = x
		}
	}
	if c.Label != "ci (Kees)" || c.Name != "claude-code 2.1" || c.Calls != 2 || c.Errors != 1 || c.LastTool != "item_get" {
		t.Fatalf("client: %+v", c)
	}
	recent := snap["recent"].([]Call)
	if len(recent) != recentCalls || recent[0].MS != recentCalls+4 || recent[0].Tool != "board_get" {
		t.Fatalf("recent: len=%d first=%+v", len(recent), recent[0])
	}

	clock = clock.Add(connectedWindow + time.Second)
	if snap = m.snapshot(); snap["connected"] != 0 {
		t.Fatalf("stale clients still connected: %v", snap["connected"])
	}
	if got := errorCode(`{"error":{"code":"too_large"}}`); got != "too_large" {
		t.Fatalf("errorCode = %q", got)
	}
}
