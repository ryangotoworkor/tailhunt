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

// Rank is the ranking the search handler uses.
var Rank = rankLegacy
