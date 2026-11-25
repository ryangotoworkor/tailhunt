package search

import (
	"fmt"
	"sort"
	"strings"
)

// Result is one hotel as the guest sees it.
type Result struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Stars       int    `json:"stars"`
	PriceCents  int    `json:"price_cents"` // after the member discount
	RoomsLeft   int    `json:"rooms_left"`
	PayAtHotel  bool   `json:"pay_at_hotel"`
	MemberPrice bool   `json:"member_price"`
}

var tierDiscount = map[string]int{"basic": 0, "silver": 5, "gold": 12}

// memberPrice applies the tier discount to a base price.
func memberPrice(base int, tier string) int { return base * (100 - tierDiscount[tier]) / 100 }

// rankKey orders results: cheapest first, then more stars, then by name.
// It is the version that shipped first. It builds a fresh string for each
// side of every comparison, so ranking fifty hotels formats a few hundred
// strings per request.
// @learn:rank-legacy
func rankKey(r Result, tier string) string {
	return fmt.Sprintf("%010d|%d|%s|%s", r.PriceCents, 9-r.Stars, strings.ToLower(r.Name), tier)
}

func rankLegacy(rs []Result, tier string) {
	sort.Slice(rs, func(i, j int) bool { return rankKey(rs[i], tier) < rankKey(rs[j], tier) })
}

// @learn:end rank-legacy

// rankFixed orders exactly like rankLegacy without building any string:
// it compares the fields directly, and compares names case-insensitively
// byte by byte instead of lowering a copy of each one.
// @learn:rank-fixed
func rankFixed(rs []Result, _ string) {
	sort.SliceStable(rs, func(i, j int) bool { return lessResult(&rs[i], &rs[j]) })
}

func lessResult(a, b *Result) bool {
	if a.PriceCents != b.PriceCents {
		return a.PriceCents < b.PriceCents
	}
	if a.Stars != b.Stars {
		return a.Stars > b.Stars
	}
	return lessFold(a.Name, b.Name)
}

// lessFold is strings.ToLower(a) < strings.ToLower(b) for ASCII names,
// with no allocation. Non-ASCII names fall back to the allocating form.
func lessFold(a, b string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		if x >= 0x80 || y >= 0x80 {
			return strings.ToLower(a) < strings.ToLower(b)
		}
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return x < y
		}
	}
	return len(a) < len(b)
}

// @learn:end rank-fixed

// Rankers are the two implementations, selectable at runtime so the same
// binary can be profiled both ways.
var Rankers = map[string]func([]Result, string){"legacy": rankLegacy, "fixed": rankFixed}
