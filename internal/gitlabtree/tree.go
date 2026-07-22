// Package gitlabtree models the GitLab group/subgroup/project hierarchy and
// rolls up per-project Scorecard results into project-count-weighted
// averages at every level of the tree.
package gitlabtree

// Kind distinguishes a group node from a project (leaf) node.
type Kind string

const (
	// KindGroup marks a node representing a GitLab group or subgroup.
	KindGroup Kind = "group"
	// KindProject marks a node representing a single GitLab project.
	KindProject Kind = "project"
)

// ScoreStat is a weighted average score together with the number of
// projects that contributed to it, so it can be combined with sibling
// scores at the parent level without losing the original weighting.
type ScoreStat struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

// Node is a single entry in the discovered GitLab hierarchy: either a group
// (with children) or a project (a leaf). Score and Checks are populated by
// the scan and aggregate steps; they are nil until then.
type Node struct {
	Score        *ScoreStat            `json:"score,omitempty"`
	Checks       map[string]*ScoreStat `json:"checks,omitempty"`
	Kind         Kind                  `json:"kind"`
	Name         string                `json:"name"`
	FullPath     string                `json:"fullPath"`
	WebURL       string                `json:"webUrl"`
	ScanError    string                `json:"scanError,omitempty"`
	Children     []*Node               `json:"children,omitempty"`
	ProjectCount int                   `json:"projectCount"`
}

// combine merges a set of ScoreStats into a single project-count-weighted
// average, skipping nil or empty stats. It returns nil if none of the
// inputs carry any weight.
func combine(stats []*ScoreStat) *ScoreStat {
	var weightedSum float64

	var totalCount int

	for _, stat := range stats {
		if stat == nil || stat.Count == 0 {
			continue
		}

		weightedSum += stat.Average * float64(stat.Count)
		totalCount += stat.Count
	}

	if totalCount == 0 {
		return nil
	}

	return &ScoreStat{Average: weightedSum / float64(totalCount), Count: totalCount}
}
