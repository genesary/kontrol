package scan

import (
	"testing"

	"github.com/ossf/scorecard/v5/checker"
	"github.com/ossf/scorecard/v5/pkg/scorecard"

	"github.com/genesary/security-hub/internal/gitlabtree"
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
