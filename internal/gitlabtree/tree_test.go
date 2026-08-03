package gitlabtree_test

import (
	"testing"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

// Combine is the arithmetic behind every number in the report, at every
// level of the tree, and scan reuses it to weight a project's checks by
// their configured importance. Aggregate's tests only ever reach it with
// Count-1 stats, so its actual weighting contract is covered here.
func TestCombine(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stats       []*gitlabtree.ScoreStat
		wantNil     bool
		wantAverage float64
		wantCount   int
	}{
		"no stats at all is inconclusive": {
			stats:   nil,
			wantNil: true,
		},
		"only nil stats is inconclusive": {
			stats:   []*gitlabtree.ScoreStat{nil, nil},
			wantNil: true,
		},
		"only zero-count stats is inconclusive": {
			stats:   []*gitlabtree.ScoreStat{{Average: 10, Count: 0}},
			wantNil: true,
		},
		"a single stat passes through": {
			stats:       []*gitlabtree.ScoreStat{{Average: 7.5, Count: 1}},
			wantAverage: 7.5,
			wantCount:   1,
		},
		"stats are weighted by count, not averaged evenly": {
			// An even average would be 5; weighted by count it is
			// (10*3 + 0*1) / 4 = 7.5.
			stats:       []*gitlabtree.ScoreStat{{Average: 10, Count: 3}, {Average: 0, Count: 1}},
			wantAverage: 7.5,
			wantCount:   4,
		},
		"nil and zero-count stats carry no weight": {
			// The nil and the zero-count entry must not drag the result
			// toward zero, nor inflate the returned count.
			stats: []*gitlabtree.ScoreStat{
				{Average: 6, Count: 2},
				nil,
				{Average: 0, Count: 0},
			},
			wantAverage: 6,
			wantCount:   2,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := gitlabtree.Combine(tc.stats)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("Combine(%+v) = %+v, want nil", tc.stats, got)
				}

				return
			}

			if got == nil {
				t.Fatalf("Combine(%+v) = nil, want {Average: %v, Count: %d}", tc.stats, tc.wantAverage, tc.wantCount)
			}

			if got.Average != tc.wantAverage || got.Count != tc.wantCount {
				t.Fatalf("Combine(%+v) = %+v, want {Average: %v, Count: %d}",
					tc.stats, got, tc.wantAverage, tc.wantCount)
			}
		})
	}
}

// TestCombineDoesNotMutateItsInputs guards a subtle hazard: Combine is
// handed stats that are still owned by the nodes they came from, so writing
// through them would corrupt the tree it is summarizing.
func TestCombineDoesNotMutateItsInputs(t *testing.T) {
	t.Parallel()

	first := &gitlabtree.ScoreStat{Average: 10, Count: 3}
	second := &gitlabtree.ScoreStat{Average: 4, Count: 1}

	gitlabtree.Combine([]*gitlabtree.ScoreStat{first, second})

	if first.Average != 10 || first.Count != 3 {
		t.Errorf("first stat = %+v, want it unchanged at {Average: 10, Count: 3}", first)
	}

	if second.Average != 4 || second.Count != 1 {
		t.Errorf("second stat = %+v, want it unchanged at {Average: 4, Count: 1}", second)
	}
}
