package search

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// catalog builds results shaped like the seeded inventory: names such as
// "Cedar Lodge 138", prices that sometimes tie, 2..5 stars. pad=true
// zero-pads the number so no name is a prefix of another (see below).
func catalog(n int, r *rand.Rand, pad ...bool) []Result {
	words := []string{"Lantern", "Harbor", "Cedar", "Maple", "Willow", "Summit", "Garden", "River"}
	kinds := []string{"Inn", "House", "Lodge", "Suites", "Hotel"}
	out := make([]Result, n)
	for i := range out {
		id := 1 + r.IntN(200)
		num := fmt.Sprint(id)
		if len(pad) > 0 && pad[0] {
			num = fmt.Sprintf("%03d", id)
		}
		out[i] = Result{ID: id, Name: words[r.IntN(8)] + " " + kinds[r.IntN(5)] + " " + num, Stars: 2 + r.IntN(4), PriceCents: 30000 + 500*r.IntN(20)}
	}
	return out
}

func firstDiff(a, b []Result) int {
	for i := range a {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}

// The two rankings agree on every input where no name is a prefix of
// another. Where one is ("Maple Inn 1" vs "Maple Inn 178"), the legacy key
// "...|maple inn 1|gold" sorts AFTER "...|maple inn 178|gold" because '|'
// (0x7C) is greater than '8'. This test found that on its first run; the
// fixed ranking keeps the order the comment always promised (by name,
// shortest prefix first), pinned by TestPrefixNamesSortShortestFirst.
func TestFixedRankingMatchesLegacy(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for round := 0; round < 500; round++ {
		a := catalog(50, r, true)
		b := slices.Clone(a)
		rankLegacy(a, "gold")
		rankFixed(b, "gold")
		if i := firstDiff(a, b); i >= 0 {
			t.Fatalf("round %d, position %d:\nlegacy %+v then %+v\nfixed  %+v then %+v", round, i, a[i], a[i+1], b[i], b[i+1])
		}
	}
}

func TestPrefixNamesSortShortestFirst(t *testing.T) {
	mk := func() []Result {
		return []Result{{ID: 178, Name: "Maple Inn 178", PriceCents: 1, Stars: 2}, {ID: 1, Name: "Maple Inn 1", PriceCents: 1, Stars: 2}}
	}
	legacy, fixed := mk(), mk()
	rankLegacy(legacy, "gold")
	rankFixed(fixed, "gold")
	if legacy[0].ID != 178 {
		t.Fatalf("legacy quirk changed: %v", legacy)
	}
	if fixed[0].ID != 1 {
		t.Fatalf("fixed should put Maple Inn 1 first: %v", fixed)
	}
}

func TestFixedRankingAllocatesNothing(t *testing.T) {
	rs := catalog(50, rand.New(rand.NewPCG(3, 4)))
	work := make([]Result, len(rs))
	allocs := testing.AllocsPerRun(100, func() {
		copy(work, rs)
		rankFixed(work, "gold")
	})
	// sort.SliceStable's reflect-based swapper costs a couple of small
	// allocations; anything per element would show up as dozens.
	if allocs > 4 {
		t.Fatalf("%v allocations per ranking, want <= 4", allocs)
	}
}

// @learn:rank-bench
func BenchmarkRank(b *testing.B) {
	rs := catalog(50, rand.New(rand.NewPCG(5, 6)))
	work := make([]Result, len(rs))
	for _, name := range []string{"legacy", "fixed"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				copy(work, rs)
				Rankers[name](work, "gold")
			}
		})
	}
}

// @learn:end rank-bench
