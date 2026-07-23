package customchecks

import "testing"

func TestScoreContributors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		count       int
		wantNil     bool
		wantAverage float64
	}{
		"no contributors is inconclusive": {
			count:   0,
			wantNil: true,
		},
		"single contributor is a bus factor of one": {
			count:       1,
			wantAverage: 0,
		},
		"a few contributors score partially": {
			count:       3,
			wantAverage: 4,
		},
		"six contributors reach the max score": {
			count:       6,
			wantAverage: 10,
		},
		"score is capped past six contributors": {
			count:       20,
			wantAverage: 10,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := scoreContributors(tc.count)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("scoreContributors(%d) = %+v, want nil", tc.count, got)
				}

				return
			}

			if got == nil || got.Average != tc.wantAverage || got.Count != 1 {
				t.Fatalf("scoreContributors(%d) = %+v, want {Average: %v, Count: 1}", tc.count, got, tc.wantAverage)
			}
		})
	}
}
