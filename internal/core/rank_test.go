package core

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func mustBetween(t *testing.T, a, b string) string {
	t.Helper()
	k, err := rankBetween(a, b)
	if err != nil {
		t.Fatalf("rankBetween(%q, %q): %v", a, b, err)
	}
	if (a != "" && k <= a) || (b != "" && k >= b) || strings.HasSuffix(k, "0") {
		t.Fatalf("rankBetween(%q, %q) = %q is out of range or ends in 0", a, b, k)
	}
	return k
}

// Appending and prepending grow a key by one character per ~30 steps; the service rebalances a column
// once a key passes maxRankLen, so a column of thousands of cards keeps short keys.
func TestRankAppendAndPrependGrowSlowly(t *testing.T) {
	last, first := "", ""
	for i := 0; i < 3000; i++ {
		last = mustBetween(t, last, "")
		first = mustBetween(t, "", first)
	}
	if len(last) > 3000/30+2 || len(first) > 3000/30+2 {
		t.Fatalf("3000 steps reached key lengths %d and %d", len(last), len(first))
	}
}

func TestRankRandomInsertionsKeepOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	keys := []string{}
	for i := 0; i < 4000; i++ {
		pos := rng.IntN(len(keys) + 1)
		a, b := "", ""
		if pos > 0 {
			a = keys[pos-1]
		}
		if pos < len(keys) {
			b = keys[pos]
		}
		keys = slices.Insert(keys, pos, mustBetween(t, a, b))
	}
	if !slices.IsSorted(keys) {
		t.Fatal("keys not in order")
	}
	if len(slices.Compact(slices.Clone(keys))) != len(keys) {
		t.Fatal("duplicate keys")
	}
}

func TestRankRejectsUnorderedBoundsAndWorstCaseStillWorks(t *testing.T) {
	if _, err := rankBetween("b", "a"); err == nil {
		t.Fatal("inverted bounds accepted")
	}
	if _, err := rankBetween("a", "a"); err == nil {
		t.Fatal("equal bounds accepted")
	}
	// Always inserting at the same spot is the worst case: keys grow, but stay valid until a rebalance.
	a, b := "V", "W"
	for i := 0; i < 200; i++ {
		b = mustBetween(t, a, b)
	}
}

func TestEvenKeysIncreasing(t *testing.T) {
	for _, n := range []int{1, 2, 10, 2000, 50000} {
		keys := evenKeys(n)
		if len(keys) != n || !slices.IsSorted(keys) || len(slices.Compact(slices.Clone(keys))) != n {
			t.Fatalf("evenKeys(%d) not strictly increasing", n)
		}
		for _, k := range keys {
			if k == "" || strings.HasSuffix(k, "0") || len(k) > 5 {
				t.Fatalf("evenKeys(%d) produced %q", n, k)
			}
		}
	}
}
