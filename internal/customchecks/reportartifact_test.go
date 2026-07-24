package customchecks

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestScoreReportArtifact(t *testing.T) {
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

			got := scoreReportArtifact(tc.pipelinesChecked, tc.pipelinesWithReport)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("scoreReportArtifact(%d, %d) = %+v, want nil", tc.pipelinesChecked, tc.pipelinesWithReport, got)
				}

				return
			}

			if got == nil || got.Average != tc.wantAverage || got.Count != 1 {
				t.Fatalf("scoreReportArtifact(%d, %d) = %+v, want {Average: %v, Count: 1}",
					tc.pipelinesChecked, tc.pipelinesWithReport, got, tc.wantAverage)
			}
		})
	}
}

func TestReportArtifactScoreEndToEnd(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
		case strings.HasSuffix(r.URL.Path, "/pipelines/1/jobs"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":10,"artifacts":[{"file_type":"sast"}]}]`)
		case strings.HasSuffix(r.URL.Path, "/pipelines/2/jobs"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":11,"artifacts":[{"file_type":"codequality"}]}]`)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := reportArtifactScore(context.Background(), client, "group/project", "sast")
	if err != nil {
		t.Fatalf("reportArtifactScore() error = %v", err)
	}

	const wantAverage = 5.0

	if got == nil || got.Average != wantAverage || got.Count != 1 {
		t.Fatalf("reportArtifactScore() = %+v, want {Average: %v, Count: 1}", got, wantAverage)
	}
}

func TestReportArtifactScorePipelinesListError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	_, err := reportArtifactScore(context.Background(), client, "group/project", "sast")
	if err == nil {
		t.Fatal("reportArtifactScore() error = nil, want non-nil for pipeline listing failure")
	}

	if !strings.Contains(err.Error(), "listing pipelines") {
		t.Fatalf("reportArtifactScore() error = %v, want it to mention %q", err, "listing pipelines")
	}
}

func TestReportArtifactScoreJobsListError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":1}]`)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})

	_, err := reportArtifactScore(context.Background(), client, "group/project", "sast")
	if err == nil {
		t.Fatal("reportArtifactScore() error = nil, want non-nil for job listing failure")
	}

	if !strings.Contains(err.Error(), "listing jobs") {
		t.Fatalf("reportArtifactScore() error = %v, want it to mention %q", err, "listing jobs")
	}
}
