package customchecks

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/genesary/kontrol/internal/gitlabtree"
)

const (
	// CheckContributors reuses Scorecard's own check name: it is the same
	// concept (bus factor across contributors), just computed from
	// GitLab's contributors API instead of GitHub's.
	CheckContributors = "Contributors"

	// contributorScoreStep is how many points each additional distinct
	// contributor (beyond the first) adds to the score, capped at maxScore.
	contributorScoreStep = 2
)

// Contributors reports a project's bus factor on a 0-10 scale: the number
// of distinct commit authors on the default branch, per GitLab's
// contributors API. Unlike Scorecard's GitHub-based Contributors check,
// GitLab's REST API exposes no organization/company affiliation, so this
// is a headcount proxy rather than a measure of organizational diversity.
func Contributors(ctx context.Context, gl *gitlab.Client, projectPath string) (*gitlabtree.ScoreStat, error) {
	contributors, _, err := gl.Repositories.Contributors(projectPath, nil, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("listing contributors for %q: %w", projectPath, err)
	}

	return scoreContributors(len(contributors)), nil
}

// scoreContributors is the pure scoring policy for Contributors, split out
// so it can be unit tested without a GitLab client. No contributors is
// inconclusive rather than scored, so it renders as N/A instead of a
// misleading zero; a single contributor is a bus factor of one and scores
// the minimum.
func scoreContributors(count int) *gitlabtree.ScoreStat {
	if count == 0 {
		return nil
	}

	score := min(contributorScoreStep*(count-1), maxScore)

	return &gitlabtree.ScoreStat{Average: float64(score), Count: 1}
}
