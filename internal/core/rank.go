package core

import (
	"errors"
	"strings"
)

// Board positions are lexicographic fractional keys: a move writes one new key between two
// neighbours instead of renumbering a column. Keys use the digits 0-9A-Za-z (ASCII order, so SQLite's
// BINARY comparison sorts them correctly) and never end in '0', which keeps a key always available
// between any two others.
const (
	rankDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	rankBase   = len(rankDigits)
	// maxRankLen triggers a rebalance of the column; with the append/prepend rules keys stay far shorter.
	maxRankLen = 24
)

func rankIdx(c byte) int { return strings.IndexByte(rankDigits, c) }

// rankBetween returns a key k with a < k < b. An empty a means "no lower bound", an empty b "no upper bound".
func rankBetween(a, b string) (string, error) {
	if b != "" && a >= b {
		return "", errors.New("rank bounds are not ordered")
	}
	return rankMid(a, b), nil
}

func rankMid(a, b string) string {
	if b != "" {
		// Strip the common prefix (a is read as padded with zeros).
		n := 0
		for n < len(b) {
			ca := byte('0')
			if n < len(a) {
				ca = a[n]
			}
			if ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			return b[:n] + rankMid(rest, b[n:])
		}
	}

	da, db := 0, rankBase
	if a != "" {
		da = rankIdx(a[0])
	}
	if b != "" {
		db = rankIdx(b[0])
	}
	switch {
	case a == "" && b == "":
		return "V"
	case b == "": // append: step the first digit, so repeated appends stay short
		if da < rankBase-1 {
			return string(rankDigits[da+1])
		}
		return "z" + rankMid(a[1:], "")
	case a == "" && db > 1: // prepend: step the first digit down
		return string(rankDigits[db-1])
	case db-da > 1:
		return string(rankDigits[(da+db)/2])
	case len(b) > 1:
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return string(rankDigits[da]) + rankMid(rest, "")
}

// evenKeys returns n strictly increasing keys spread over the key space, for rebalancing a column.
func evenKeys(n int) []string {
	width, space := 4, 62*62*62*62
	for space/(n+1) < 16 {
		width, space = width+1, space*62
	}
	step := space / (n + 1)
	out := make([]string, n)
	for i := range out {
		v := (i + 1) * step
		buf := make([]byte, width)
		for j := width - 1; j >= 0; j-- {
			buf[j] = rankDigits[v%rankBase]
			v /= rankBase
		}
		out[i] = strings.TrimRight(string(buf), "0")
	}
	return out
}
