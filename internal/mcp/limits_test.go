package mcp

import (
	"testing"
	"time"
)

func TestLimiterWindowAndAtomicTake(t *testing.T) {
	l := newLimiter(5, time.Hour)
	now := time.Now()
	l.now = func() time.Time { return now }

	if _, ok := l.take("a", 3); !ok {
		t.Fatal("first take refused")
	}
	// A take that does not fit records nothing, so a smaller one still fits.
	if wait, ok := l.take("a", 3); ok || wait <= 0 {
		t.Fatalf("over-limit take allowed (wait %v)", wait)
	}
	if _, ok := l.take("a", 2); !ok {
		t.Fatal("refused take consumed budget")
	}
	if _, ok := l.take("b", 5); !ok {
		t.Fatal("keys share a budget")
	}
	now = now.Add(time.Hour)
	if _, ok := l.take("a", 5); !ok {
		t.Fatal("window did not reset")
	}
}
