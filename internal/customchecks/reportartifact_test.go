package customchecks

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/genesary/kontrol/internal/gitlabtree"
)

// allReportArtifactChecks is every check built on the report-artifact
// pattern, as a caller would request them.
var allReportArtifactChecks = []string{
	CheckCodeQuality,
	CheckDependencyScanning,
	CheckSAST,
	CheckSecretDetection,
}

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

func TestIsReportArtifactCheck(t *testing.T) {
	t.Parallel()

	for _, name := range allReportArtifactChecks {
		if !IsReportArtifactCheck(name) {
			t.Errorf("IsReportArtifactCheck(%q) = false, want true", name)
		}
	}

	// Contributors is a custom check, but not one built on this pattern: it
	// must not be swept into the batch.
	if IsReportArtifactCheck(CheckContributors) {
		t.Errorf("IsReportArtifactCheck(%q) = true, want false", CheckContributors)
	}

	if IsReportArtifactCheck("Maintained") {
		t.Errorf("IsReportArtifactCheck(%q) = true, want false", "Maintained")
	}
}

func TestReportArtifactScoresEndToEnd(t *testing.T) {
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

	got, err := ReportArtifactScores(context.Background(), client, "group/project", "main", []string{CheckSAST})
	if err != nil {
		t.Fatalf("ReportArtifactScores() error = %v", err)
	}

	const wantAverage = 5.0

	stat := got[CheckSAST]
	if stat == nil || stat.Average != wantAverage || stat.Count != 1 {
		t.Fatalf("ReportArtifactScores()[%s] = %+v, want {Average: %v, Count: 1}", CheckSAST, stat, wantAverage)
	}
}

// TestReportArtifactScoresIsolatesCheckFileTypes asserts that each check is
// credited only for its own artifact file type, so a project whose pipelines
// only upload a codequality report is not also credited for SAST, secret
// detection or dependency scanning.
func TestReportArtifactScoresIsolatesCheckFileTypes(t *testing.T) {
	t.Parallel()

	for _, scored := range allReportArtifactChecks {
		t.Run(scored, func(t *testing.T) {
			t.Parallel()

			fileType := reportArtifactFileTypes()[scored]

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/pipelines"):
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `[{"id":1}]`)
				case strings.HasSuffix(r.URL.Path, "/pipelines/1/jobs"):
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `[{"id":10,"artifacts":[{"file_type":%q}]}]`, fileType)
				default:
					http.NotFound(w, r)
				}
			})

			got, err := ReportArtifactScores(context.Background(), client, "group/project", "main", allReportArtifactChecks)
			if err != nil {
				t.Fatalf("ReportArtifactScores() error = %v", err)
			}

			for _, name := range allReportArtifactChecks {
				want := 0.0
				if name == scored {
					want = 10
				}

				stat := got[name]
				if stat == nil || stat.Average != want {
					t.Errorf("ReportArtifactScores()[%s] = %+v, want Average=%v (only %q artifact present)",
						name, stat, want, fileType)
				}
			}
		})
	}
}

// TestReportArtifactScoresSharesOnePassAcrossChecks asserts that scoring
// every report-artifact check costs one pipeline listing and one jobs
// listing per pipeline, not one of each per check. Running them separately
// multiplied a project's custom-check API traffic by the number of enabled
// checks while fetching byte-for-byte identical responses.
func TestReportArtifactScoresSharesOnePassAcrossChecks(t *testing.T) {
	t.Parallel()

	var pipelineRequests, jobRequests int

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			pipelineRequests++

			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
		case strings.Contains(r.URL.Path, "/pipelines/") && strings.HasSuffix(r.URL.Path, "/jobs"):
			jobRequests++

			fmt.Fprint(w, `[{"id":10,"artifacts":[{"file_type":"sast"}]}]`)
		default:
			http.NotFound(w, r)
		}
	})

	_, err := ReportArtifactScores(context.Background(), client, "group/project", "main", allReportArtifactChecks)
	if err != nil {
		t.Fatalf("ReportArtifactScores() error = %v", err)
	}

	if pipelineRequests != 1 {
		t.Errorf("pipeline listings = %d, want 1 for all %d checks", pipelineRequests, len(allReportArtifactChecks))
	}

	// Two pipelines were returned, so two job listings, not two per check.
	if jobRequests != 2 {
		t.Errorf("job listings = %d, want 2 (one per sampled pipeline) for all %d checks",
			jobRequests, len(allReportArtifactChecks))
	}
}

// TestSampledPipelinesScopesTheSamplingWindow pins the query actually sent
// to GitLab. Every report-artifact score is a fraction of this sample, so a
// silent change to it (say, losing per_page and inheriting GitLab's default
// of 20, or dropping the status filter) would move every score in the report
// without any other test noticing.
func TestSampledPipelinesScopesTheSamplingWindow(t *testing.T) {
	t.Parallel()

	var got url.Values

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	_, err := sampledPipelines(context.Background(), client, "group/project", "main")
	if err != nil {
		t.Fatalf("sampledPipelines() error = %v", err)
	}

	want := map[string]string{
		"per_page": strconv.Itoa(reportArtifactLookback),
		"order_by": "id",
		"sort":     "desc",
		"status":   string(gitlab.Success),
		"ref":      "main",
	}

	for param, wantValue := range want {
		if gotValue := got.Get(param); gotValue != wantValue {
			t.Errorf("pipelines request %s = %q, want %q (full query: %q)", param, gotValue, wantValue, got.Encode())
		}
	}
}

// TestSampledPipelinesFallsBackToEveryRefWithoutADefaultBranch asserts a
// project whose default branch discovery never learned (one with no commits,
// say) is sampled across every ref rather than against an empty ref filter,
// which GitLab would answer with nothing.
func TestSampledPipelinesFallsBackToEveryRefWithoutADefaultBranch(t *testing.T) {
	t.Parallel()

	var got url.Values

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	_, err := sampledPipelines(context.Background(), client, "group/project", "")
	if err != nil {
		t.Fatalf("sampledPipelines() error = %v", err)
	}

	if _, ok := got["ref"]; ok {
		t.Errorf("pipelines request sent ref = %q, want the parameter omitted entirely", got.Get("ref"))
	}

	// The status filter is independent of the ref one and must survive.
	if status := got.Get("status"); status != string(gitlab.Success) {
		t.Errorf("pipelines request status = %q, want %q", status, gitlab.Success)
	}
}

func TestReportArtifactScoresIgnoresUnknownCheckNames(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("ReportArtifactScores() made an API call for a request with no report-artifact checks")
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	})

	got, err := ReportArtifactScores(context.Background(), client, "group/project", "main", []string{CheckContributors, "Maintained"})
	if err != nil {
		t.Fatalf("ReportArtifactScores() error = %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("ReportArtifactScores() = %+v, want no scores for non-report-artifact checks", got)
	}
}

// TestReportArtifactScoresWalkEveryJobPage asserts that a scanner job past
// the first page of a large pipeline's jobs is still found. GitLab caps the
// jobs endpoint at 20 per page when no per_page is sent, so a pipeline big
// enough to spill over used to score as if it ran no scanner at all.
func TestReportArtifactScoresWalkEveryJobPage(t *testing.T) {
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

	got, err := ReportArtifactScores(context.Background(), client, "group/project", "main", []string{CheckSAST})
	if err != nil {
		t.Fatalf("ReportArtifactScores() error = %v", err)
	}

	const wantAverage = 10.0

	if stat := got[CheckSAST]; stat == nil || stat.Average != wantAverage {
		t.Fatalf("ReportArtifactScores()[%s] = %+v, want Average=%v (sast job is on job page 2)",
			CheckSAST, stat, wantAverage)
	}

	if jobPagesServed != 2 {
		t.Fatalf("served %d job pages, want 2 (every page must be walked)", jobPagesServed)
	}
}

// TestPipelineReportArtifactsRequestsAFullJobPage asserts an explicit
// per_page is sent, rather than silently inheriting GitLab's default of 20.
func TestPipelineReportArtifactsRequestsAFullJobPage(t *testing.T) {
	t.Parallel()

	var gotPerPage string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPerPage = r.URL.Query().Get("per_page")

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	_, err := pipelineReportArtifacts(context.Background(), client, "group/project", 1, map[string]string{"sast": CheckSAST})
	if err != nil {
		t.Fatalf("pipelineReportArtifacts() error = %v", err)
	}

	if want := strconv.Itoa(jobsPageSize); gotPerPage != want {
		t.Fatalf("jobs request per_page = %q, want %q", gotPerPage, want)
	}
}

// TestPipelineReportArtifactsStopsPagingOnceEveryCheckIsFound asserts the
// job walk gives up early: there is nothing left to learn from a pipeline
// once every requested check has been seen.
func TestPipelineReportArtifactsStopsPagingOnceEveryCheckIsFound(t *testing.T) {
	t.Parallel()

	var jobPagesServed int

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		jobPagesServed++

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Next-Page", "2")
		fmt.Fprint(w, `[{"id":10,"artifacts":[{"file_type":"sast"}]}]`)
	})

	found, err := pipelineReportArtifacts(context.Background(), client, "group/project", 1, map[string]string{"sast": CheckSAST})
	if err != nil {
		t.Fatalf("pipelineReportArtifacts() error = %v", err)
	}

	if !found[CheckSAST] {
		t.Fatalf("pipelineReportArtifacts() = %+v, want %s found", found, CheckSAST)
	}

	if jobPagesServed != 1 {
		t.Fatalf("served %d job pages, want 1 (stop once every requested check is found)", jobPagesServed)
	}
}

func TestReportArtifactScoresPipelinesListError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	_, err := ReportArtifactScores(context.Background(), client, "group/project", "main", []string{CheckSAST})
	if err == nil {
		t.Fatal("ReportArtifactScores() error = nil, want non-nil for pipeline listing failure")
	}

	if !strings.Contains(err.Error(), "listing pipelines") {
		t.Fatalf("ReportArtifactScores() error = %v, want it to mention %q", err, "listing pipelines")
	}
}

func TestReportArtifactScoresJobsListError(t *testing.T) {
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

	_, err := ReportArtifactScores(context.Background(), client, "group/project", "main", []string{CheckSAST})
	if err == nil {
		t.Fatal("ReportArtifactScores() error = nil, want non-nil for job listing failure")
	}

	if !strings.Contains(err.Error(), "listing jobs") {
		t.Fatalf("ReportArtifactScores() error = %v, want it to mention %q", err, "listing jobs")
	}
}

// TestReportArtifactScoresNoPipelinesIsInconclusive asserts a project with
// nothing to sample renders as N/A for every requested check rather than
// scoring a misleading zero.
func TestReportArtifactScoresNoPipelinesIsInconclusive(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/pipelines") {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	got, err := ReportArtifactScores(context.Background(), client, "group/project", "main", allReportArtifactChecks)
	if err != nil {
		t.Fatalf("ReportArtifactScores() error = %v", err)
	}

	for _, name := range allReportArtifactChecks {
		stat, ok := got[name]
		if !ok {
			t.Errorf("ReportArtifactScores()[%s] missing, want key present with a nil (N/A) value", name)
		}

		if stat != (*gitlabtree.ScoreStat)(nil) {
			t.Errorf("ReportArtifactScores()[%s] = %+v, want nil (no pipelines to sample)", name, stat)
		}
	}
}
