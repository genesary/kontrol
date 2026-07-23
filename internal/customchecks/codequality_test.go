package customchecks

import "testing"

func TestScoreCodeQuality(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pipelinesChecked    int
		pipelinesWithReport int
		wantNil             bool
		wantAverage         float64
	}{
		"no pipelines is inconclusive": {
			pipelinesChecked:    0,
			pipelinesWithReport: 0,
			wantNil:             true,
		},
		"no pipeline has a report": {
			pipelinesChecked:    5,
			pipelinesWithReport: 0,
			wantAverage:         0,
		},
		"every pipeline has a report": {
			pipelinesChecked:    5,
			pipelinesWithReport: 5,
			wantAverage:         10,
		},
		"some pipelines have a report": {
			pipelinesChecked:    5,
			pipelinesWithReport: 2,
			wantAverage:         4,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := scoreCodeQuality(tc.pipelinesChecked, tc.pipelinesWithReport)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("scoreCodeQuality(%d, %d) = %+v, want nil", tc.pipelinesChecked, tc.pipelinesWithReport, got)
				}

				return
			}

			if got == nil || got.Average != tc.wantAverage || got.Count != 1 {
				t.Fatalf("scoreCodeQuality(%d, %d) = %+v, want {Average: %v, Count: 1}",
					tc.pipelinesChecked, tc.pipelinesWithReport, got, tc.wantAverage)
			}
		})
	}
}
