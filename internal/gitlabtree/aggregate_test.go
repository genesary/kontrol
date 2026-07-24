package gitlabtree_test

import (
	"testing"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

// project is a small helper building a leaf project node with a given
// overall score and a single "Check" score, both weight 1.
func project(name string, score float64) *gitlabtree.Node {
	return &gitlabtree.Node{
		Kind:         gitlabtree.KindProject,
		Name:         name,
		ProjectCount: 1,
		Score:        &gitlabtree.ScoreStat{Average: score, Count: 1},
		Checks: map[string]*gitlabtree.ScoreStat{
			"Check": {Average: score, Count: 1},
		},
	}
}

func TestAggregateWeightsByProjectCount(t *testing.T) {
	t.Parallel()

	// Group A has a single project scoring 10; group B has three projects
	// all scoring 0. A naive average-of-subgroup-averages would yield
	// (10+0)/2 = 5. Weighted by project count, it must be (10*1+0*3)/4 = 2.5.
	groupA := &gitlabtree.Node{
		Kind:     gitlabtree.KindGroup,
		Name:     "A",
		Children: []*gitlabtree.Node{project("a1", 10)},
	}
	groupB := &gitlabtree.Node{
		Kind: gitlabtree.KindGroup,
		Name: "B",
		Children: []*gitlabtree.Node{
			project("b1", 0),
			project("b2", 0),
			project("b3", 0),
		},
	}
	root := &gitlabtree.Node{
		Kind:     gitlabtree.KindGroup,
		Name:     "root",
		Children: []*gitlabtree.Node{groupA, groupB},
	}

	gitlabtree.Aggregate(root)

	const wantProjectCount = 4

	if root.ProjectCount != wantProjectCount {
		t.Fatalf("ProjectCount = %d, want %d", root.ProjectCount, wantProjectCount)
	}

	const wantScore = 2.5
	if root.Score == nil || root.Score.Average != wantScore {
		t.Fatalf("Score = %+v, want average %v (weighted by project count, not %v)", root.Score, wantScore, 5.0)
	}

	if root.Checks["Check"].Average != wantScore {
		t.Fatalf("Checks[Check] = %+v, want average %v", root.Checks["Check"], wantScore)
	}
}

func TestAggregateIgnoresScanFailures(t *testing.T) {
	t.Parallel()

	failed := &gitlabtree.Node{
		Kind:         gitlabtree.KindProject,
		Name:         "failed",
		ProjectCount: 1,
		ScanError:    "boom",
	}
	root := &gitlabtree.Node{
		Kind:     gitlabtree.KindGroup,
		Name:     "root",
		Children: []*gitlabtree.Node{project("ok", 8), failed},
	}

	gitlabtree.Aggregate(root)

	const wantProjectCount = 2
	if root.ProjectCount != wantProjectCount {
		t.Fatalf("ProjectCount = %d, want %d", root.ProjectCount, wantProjectCount)
	}

	const wantScore = 8.0
	if root.Score == nil || root.Score.Average != wantScore {
		t.Fatalf("Score = %+v, want average %v (failed scan must not drag it down)", root.Score, wantScore)
	}
}

func TestAggregateKeepsInconclusiveChecksVisible(t *testing.T) {
	t.Parallel()

	// "Fuzzing" is inconclusive (nil) on every project below root: it must
	// still show up as a key in root.Checks (rendered as "N/A" in the report)
	// rather than silently vanishing, which would be indistinguishable from
	// never having been requested at all.
	inconclusive := &gitlabtree.Node{
		Kind:         gitlabtree.KindProject,
		Name:         "inconclusive",
		ProjectCount: 1,
		Score:        &gitlabtree.ScoreStat{Average: 6, Count: 1},
		Checks: map[string]*gitlabtree.ScoreStat{
			"Fuzzing": nil,
		},
	}
	root := &gitlabtree.Node{
		Kind:     gitlabtree.KindGroup,
		Name:     "root",
		Children: []*gitlabtree.Node{project("ok", 8), inconclusive},
	}

	gitlabtree.Aggregate(root)

	stat, ok := root.Checks["Fuzzing"]
	if !ok {
		t.Fatalf("root.Checks[%q] missing entirely, want key present with a nil (N/A) value", "Fuzzing")
	}

	if stat != nil {
		t.Fatalf("root.Checks[%q] = %+v, want nil (N/A) since every project was inconclusive for it", "Fuzzing", stat)
	}

	if _, ok := root.Checks["Check"]; !ok {
		t.Fatalf(`root.Checks["Check"] missing, want key present with the "ok" project's real score`)
	}
}
