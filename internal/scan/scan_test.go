package scan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ossf/scorecard/v5/checker"
	"github.com/ossf/scorecard/v5/checks"
	"github.com/ossf/scorecard/v5/pkg/scorecard"
	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/boxboxjason/security-hub/internal/customchecks"
	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

func TestWeightFor(t *testing.T) {
	t.Parallel()

	weights := map[string]int{"Vulnerabilities": 3, "Code-Quality": 0}

	if got := weightFor("Vulnerabilities", weights); got != 3 {
		t.Errorf("weightFor(Vulnerabilities) = %d, want 3", got)
	}

	if got := weightFor("Code-Quality", weights); got != 0 {
		t.Errorf("weightFor(Code-Quality) = %d, want 0", got)
	}

	if got := weightFor("Maintained", weights); got != defaultWeight {
		t.Errorf("weightFor(Maintained) = %d, want default %d", got, defaultWeight)
	}
}

func TestWithoutZeroWeight(t *testing.T) {
	t.Parallel()

	names := []string{"Vulnerabilities", "Code-Quality", "Maintained"}

	got := withoutZeroWeight(names, map[string]int{"Code-Quality": 0})
	want := []string{"Vulnerabilities", "Maintained"}

	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("withoutZeroWeight() = %v, want %v", got, want)
	}

	if got := withoutZeroWeight(names, nil); len(got) != len(names) {
		t.Errorf("withoutZeroWeight() with no weights = %v, want unchanged %v", got, names)
	}
}

func TestResolveChecksDropsZeroWeightChecks(t *testing.T) {
	t.Parallel()

	opts := Options{
		Checks:  []string{"Vulnerabilities", "Maintained", "Fuzzing"},
		Weights: map[string]int{"Maintained": 0},
	}

	toRun, skippedOffline := resolveChecks(opts)

	for _, name := range toRun {
		if name == "Maintained" {
			t.Fatalf("resolveChecks() toRun = %v, want weight-0 check excluded", toRun)
		}
	}

	if len(skippedOffline) != 0 {
		t.Errorf("skippedOffline = %v, want none (not offline)", skippedOffline)
	}
}

func TestToScoreStatsAppliesWeights(t *testing.T) {
	t.Parallel()

	result := scorecard.Result{
		Checks: []checker.CheckResult{
			{Name: "Vulnerabilities", Score: 8},
			{Name: "Maintained", Score: -1},
		},
	}

	checkScores := toScoreStats(result, map[string]int{"Vulnerabilities": 3})

	stat := checkScores["Vulnerabilities"]
	if stat == nil || stat.Average != 8 || stat.Count != 3 {
		t.Errorf("checkScores[Vulnerabilities] = %+v, want Average=8 Count=3", stat)
	}

	if checkScores["Maintained"] != nil {
		t.Errorf("checkScores[Maintained] = %+v, want nil (inconclusive)", checkScores["Maintained"])
	}
}

func TestRunCustomCheckAppliesWeight(t *testing.T) {
	t.Parallel()

	checkScores := map[string]*gitlabtree.ScoreStat{}

	runCustomCheck(checkScores, "Code-Quality", 3, "group/project", func() (*gitlabtree.ScoreStat, error) {
		return &gitlabtree.ScoreStat{Average: 8}, nil
	})

	stat := checkScores["Code-Quality"]
	if stat == nil || stat.Average != 8 || stat.Count != 3 {
		t.Errorf("checkScores[Code-Quality] = %+v, want Average=8 Count=3", stat)
	}
}

func TestRunCustomCheckErrorIsRecordedAsNil(t *testing.T) {
	t.Parallel()

	checkScores := map[string]*gitlabtree.ScoreStat{}

	runCustomCheck(checkScores, "Code-Quality", 3, "group/project", func() (*gitlabtree.ScoreStat, error) {
		return nil, errors.New("boom")
	})

	if checkScores["Code-Quality"] != nil {
		t.Errorf("checkScores[Code-Quality] = %+v, want nil (error)", checkScores["Code-Quality"])
	}
}

// A nil stat with no error is the "inconclusive" result every custom check
// can legitimately return (e.g. no pipelines to sample, no contributors).
// It must be recorded as N/A rather than dereferenced, which previously
// panicked with a nil pointer dereference.
func TestRunCustomCheckNilStatWithoutErrorIsRecordedAsNil(t *testing.T) {
	t.Parallel()

	checkScores := map[string]*gitlabtree.ScoreStat{}

	runCustomCheck(checkScores, "Contributors", 3, "group/project", func() (*gitlabtree.ScoreStat, error) {
		return nil, nil
	})

	if checkScores["Contributors"] != nil {
		t.Errorf("checkScores[Contributors] = %+v, want nil (inconclusive)", checkScores["Contributors"])
	}
}

func TestOverallScoreForWeightedCombinesEveryCheck(t *testing.T) {
	t.Parallel()

	checkScores := map[string]*gitlabtree.ScoreStat{
		"Vulnerabilities": {Average: 10, Count: 3},
		"Maintained":      {Average: 0, Count: 1},
		"Fuzzing":         nil,
	}

	overall, err := overallScoreFor(scorecard.Result{}, checkScores, map[string]int{"Vulnerabilities": 3})
	if err != nil {
		t.Fatalf("overallScoreFor() error = %v", err)
	}

	const want = 30.0 / 4.0

	if overall == nil || overall.Average != want || overall.Count != 1 {
		t.Errorf("overallScoreFor() = %+v, want Average=%v Count=1", overall, want)
	}
}

func TestOverallScoreForWeightedAllExcludedIsNil(t *testing.T) {
	t.Parallel()

	checkScores := map[string]*gitlabtree.ScoreStat{"Maintained": nil}

	overall, err := overallScoreFor(scorecard.Result{}, checkScores, map[string]int{"Vulnerabilities": 0})
	if err != nil {
		t.Fatalf("overallScoreFor() error = %v", err)
	}

	if overall != nil {
		t.Errorf("overallScoreFor() = %+v, want nil", overall)
	}
}

func TestIsOfflineUnsafe(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want bool
	}{
		"Vulnerabilities is unsafe":           {checks.CheckVulnerabilities, true},
		"CII-Best-Practices is unsafe":        {checks.CheckCIIBestPractices, true},
		"Fuzzing is unsafe":                   {checks.CheckFuzzing, true},
		"case-insensitive match":              {"vulnerabilities", true},
		"Maintained is safe":                  {"Maintained", false},
		"Code-Quality (custom check) is safe": {"Code-Quality", false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := isOfflineUnsafe(tc.name); got != tc.want {
				t.Errorf("isOfflineUnsafe(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestRequestedCustomChecks(t *testing.T) {
	t.Parallel()

	opts := Options{
		CustomChecks: []string{customchecks.CheckSAST, customchecks.CheckContributors, customchecks.CheckCodeQuality},
		Weights:      map[string]int{customchecks.CheckSAST: 3, customchecks.CheckCodeQuality: 0},
	}

	requested := requestedCustomChecks(opts)

	if got, ok := requested[customchecks.CheckSAST]; !ok || got != 3 {
		t.Errorf("requestedCustomChecks()[SAST] = %d (present=%v), want 3", got, ok)
	}

	if got, ok := requested[customchecks.CheckContributors]; !ok || got != defaultWeight {
		t.Errorf("requestedCustomChecks()[Contributors] = %d (present=%v), want default %d", got, ok, defaultWeight)
	}

	if _, ok := requested[customchecks.CheckCodeQuality]; ok {
		t.Errorf("requestedCustomChecks()[Code-Quality] present, want absent (weight 0 skips the check entirely)")
	}

	if empty := requestedCustomChecks(Options{}); len(empty) != 0 {
		t.Errorf("requestedCustomChecks() with no custom checks = %v, want empty", empty)
	}
}

func TestResolveChecksDefaultsToAllChecksWhenNoneConfigured(t *testing.T) {
	t.Parallel()

	toRun, skippedOffline := resolveChecks(Options{})

	if len(toRun) != len(checks.GetAll()) {
		t.Errorf("resolveChecks() toRun has %d checks, want all %d registered checks", len(toRun), len(checks.GetAll()))
	}

	if len(skippedOffline) != 0 {
		t.Errorf("skippedOffline = %v, want none (not offline)", skippedOffline)
	}
}

func TestResolveChecksOfflineSplitsUnsafeChecks(t *testing.T) {
	t.Parallel()

	opts := Options{
		Checks:  []string{checks.CheckVulnerabilities, "Maintained", checks.CheckFuzzing},
		Offline: true,
	}

	toRun, skippedOffline := resolveChecks(opts)

	if len(toRun) != 1 || toRun[0] != "Maintained" {
		t.Errorf("resolveChecks() toRun = %v, want only [Maintained]", toRun)
	}

	wantSkipped := map[string]bool{checks.CheckVulnerabilities: true, checks.CheckFuzzing: true}
	if len(skippedOffline) != len(wantSkipped) {
		t.Fatalf("skippedOffline = %v, want %v", skippedOffline, wantSkipped)
	}

	for _, name := range skippedOffline {
		if !wantSkipped[name] {
			t.Errorf("skippedOffline contains unexpected %q", name)
		}
	}
}

func TestOverallScoreForUnweightedInconclusive(t *testing.T) {
	t.Parallel()

	overall, err := overallScoreFor(scorecard.Result{}, map[string]*gitlabtree.ScoreStat{}, nil)
	if err != nil {
		t.Fatalf("overallScoreFor() error = %v", err)
	}

	if overall != nil {
		t.Errorf("overallScoreFor() = %+v, want nil (no checks ran, inconclusive)", overall)
	}
}

func TestOverallScoreForUnweightedSuccess(t *testing.T) {
	t.Parallel()

	result := scorecard.Result{
		Checks: []checker.CheckResult{{Name: "Maintained", Score: 8}},
	}

	overall, err := overallScoreFor(result, map[string]*gitlabtree.ScoreStat{}, nil)
	if err != nil {
		t.Fatalf("overallScoreFor() error = %v", err)
	}

	const want = 8.0

	if overall == nil || overall.Average != want || overall.Count != 1 {
		t.Errorf("overallScoreFor() = %+v, want Average=%v Count=1", overall, want)
	}
}

func TestRunCustomChecksRespectsEnabledAndWeight(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/contributors"):
			fmt.Fprint(w, `[{"name":"alice"},{"name":"bob"},{"name":"carol"}]`)
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			fmt.Fprint(w, `[{"id":1}]`)
		case strings.HasSuffix(r.URL.Path, "/pipelines/1/jobs"):
			fmt.Fprint(w, `[{"id":10,"artifacts":[{"file_type":"sast"},{"file_type":"codequality"},`+
				`{"file_type":"dependency_scanning"},{"file_type":"secret_detection"}]}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL), gitlab.WithoutRetries())
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}

	opts := Options{
		GitlabClient: client,
		CustomChecks: []string{
			customchecks.CheckContributors,
			customchecks.CheckSAST,
			customchecks.CheckDependencyScanning,
			customchecks.CheckSecretDetection,
		},
		Weights: map[string]int{customchecks.CheckSAST: 0},
	}

	checkScores := map[string]*gitlabtree.ScoreStat{}

	runCustomChecks(context.Background(), opts, "group/project", "main", checkScores)

	stat := checkScores[customchecks.CheckContributors]
	if stat == nil || stat.Count != defaultWeight {
		t.Errorf("checkScores[Contributors] = %+v, want a stat weighted %d", stat, defaultWeight)
	}

	if _, ok := checkScores[customchecks.CheckSAST]; ok {
		t.Errorf("checkScores[SAST] = %+v, want absent (weight 0 skips the check entirely)", checkScores[customchecks.CheckSAST])
	}

	if _, ok := checkScores[customchecks.CheckCodeQuality]; ok {
		t.Errorf("checkScores[Code-Quality] present, want absent (not requested via CustomChecks)")
	}

	for _, name := range []string{customchecks.CheckDependencyScanning, customchecks.CheckSecretDetection} {
		if stat := checkScores[name]; stat == nil || stat.Count != defaultWeight {
			t.Errorf("checkScores[%s] = %+v, want a stat weighted %d", name, stat, defaultWeight)
		}
	}
}

func TestProjectFailsOnInvalidRepoPath(t *testing.T) {
	t.Parallel()

	_, _, err := Project(context.Background(), Options{Host: ""}, "onlyonesegment", "main")
	if err == nil {
		t.Fatal("Project() error = nil, want non-nil for a repo path with no host")
	}

	if !strings.Contains(err.Error(), "resolving gitlab repo") {
		t.Fatalf("Project() error = %v, want it to mention %q", err, "resolving gitlab repo")
	}
}

func TestProjectFailsWhenScorecardCannotReachHost(t *testing.T) {
	t.Parallel()

	// A short deadline makes the underlying HTTP client give up on the
	// unreachable host almost immediately instead of exhausting its default
	// retry/backoff schedule (which would make this test take seconds). The
	// host contains "gitlab." so gitlabrepo.MakeGitlabRepo's IsValid() skips
	// its own live network probe, letting scorecard.Run be what fails.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := Project(ctx, Options{Host: "gitlab.invalid.example"}, "group/project", "main")
	if err == nil {
		t.Fatal("Project() error = nil, want non-nil when the gitlab host is unreachable")
	}

	if !strings.Contains(err.Error(), "running scorecard") {
		t.Fatalf("Project() error = %v, want it to mention %q", err, "running scorecard")
	}
}
