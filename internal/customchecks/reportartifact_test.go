package customchecks

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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

// TestReportArtifactScoreWalksEveryJobPage asserts that a scanner job past
// the first page of a large pipeline's jobs is still found. GitLab caps the
// jobs endpoint at 20 per page when no per_page is sent, so a pipeline big
// enough to spill over used to score as if it ran no scanner at all.
func TestReportArtifactScoreWalksEveryJobPage(t *testing.T) {
	t.Parallel()

	var jobPagesServed int

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":1}]`)
		case strings.HasSuffix(r.URL.Path, "/pipelines/1/jobs"):
			jobPagesServed++

			w.Header().Set("Content-Type", "application/json")

			// The sast job lives on the second page: only a client that
			// follows X-Next-Page will ever see it.
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `[{"id":21,"artifacts":[{"file_type":"sast"}]}]`)

				return
			}

			w.Header().Set("X-Next-Page", "2")
			fmt.Fprint(w, `[{"id":10,"artifacts":[{"file_type":"trace"}]}]`)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := reportArtifactScore(context.Background(), client, "group/project", "sast")
	if err != nil {
		t.Fatalf("reportArtifactScore() error = %v", err)
	}

	const wantAverage = 10.0

	if got == nil || got.Average != wantAverage {
		t.Fatalf("reportArtifactScore() = %+v, want Average=%v (sast job is on job page 2)", got, wantAverage)
	}

	if jobPagesServed != 2 {
		t.Fatalf("served %d job pages, want 2 (every page must be walked)", jobPagesServed)
	}
}

// TestPipelineHasReportArtifactRequestsAFullJobPage asserts an explicit
// per_page is sent, rather than silently inheriting GitLab's default of 20.
func TestPipelineHasReportArtifactRequestsAFullJobPage(t *testing.T) {
	t.Parallel()

	var gotPerPage string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPerPage = r.URL.Query().Get("per_page")

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	_, err := pipelineHasReportArtifact(context.Background(), client, "group/project", 1, "sast")
	if err != nil {
		t.Fatalf("pipelineHasReportArtifact() error = %v", err)
	}

	if want := strconv.Itoa(jobsPageSize); gotPerPage != want {
		t.Fatalf("jobs request per_page = %q, want %q", gotPerPage, want)
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
