// Package scan runs OpenSSF Scorecard against individual GitLab projects,
// pointing it directly at the self-hosted GitLab instance's API rather than
// cloning repositories locally.
package scan

import (
	"context"
	"fmt"
	"strings"

	"github.com/ossf/scorecard/v5/checks"
	"github.com/ossf/scorecard/v5/clients/gitlabrepo"
	docChecks "github.com/ossf/scorecard/v5/docs/checks"
	"github.com/ossf/scorecard/v5/pkg/scorecard"
	"go.uber.org/zap"

	"github.com/genesary/security-hub/internal/gitlabtree"
)

// Options configures how Scorecard analyzes each project.
type Options struct {
	// Host is the GitLab instance host without scheme, e.g. "gitlab.example.com".
	Host string
	// Token authenticates against the GitLab instance's API.
	Token string
	// Checks restricts analysis to an explicit subset; empty means every
	// check Scorecard supports.
	Checks []string
	// Offline disables checks that call out to public internet services.
	Offline bool
}

// Project runs Scorecard against a single GitLab project (addressed by its
// full path, e.g. "group/subgroup/project") and returns its overall score
// and per-check scores as weight-1 stats ready to be merged into the
// discovery tree.
func Project(ctx context.Context, opts Options, fullPath string) (*gitlabtree.ScoreStat, map[string]*gitlabtree.ScoreStat, error) {
	repo, err := gitlabrepo.MakeGitlabRepo(fmt.Sprintf("%s/%s", opts.Host, fullPath))
	if err != nil {
		return nil, nil, fmt.Errorf("resolving gitlab repo %q: %w", fullPath, err)
	}

	client, err := gitlabrepo.CreateGitlabClientWithToken(ctx, opts.Token, opts.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("creating gitlab client for %q: %w", fullPath, err)
	}

	toRun, skippedOffline := resolveChecks(opts)

	result, err := scorecard.Run(ctx, repo,
		scorecard.WithRepoClient(client),
		scorecard.WithChecks(toRun),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("running scorecard for %q: %w", fullPath, err)
	}

	overall, checkScores, err := toScoreStats(result)
	if err != nil {
		return nil, nil, fmt.Errorf("scoring scorecard result for %q: %w", fullPath, err)
	}

	// Checks skipped by our own offline filter never reach Scorecard, so they
	// never appear in result.Checks. Add them as nil (rendered as "N/A") so
	// they're still visible in the report instead of silently vanishing.
	for _, name := range skippedOffline {
		checkScores[name] = nil
	}

	return overall, checkScores, nil
}

// resolveChecks expands Options into the explicit list of check names
// Scorecard should run, plus (when offline) the names filtered out because
// they require internet access.
func resolveChecks(opts Options) ([]string, []string) {
	candidates := opts.Checks
	if len(candidates) == 0 {
		candidates = make([]string, 0, len(checks.GetAll()))
		for name := range checks.GetAll() {
			candidates = append(candidates, name)
		}
	}

	if !opts.Offline {
		return candidates, nil
	}

	toRun := make([]string, 0, len(candidates))

	var skippedOffline []string

	for _, name := range candidates {
		if isOfflineUnsafe(name) {
			skippedOffline = append(skippedOffline, name)
		} else {
			toRun = append(toRun, name)
		}
	}

	return toRun, skippedOffline
}

// isOfflineUnsafe reports whether a Scorecard check calls out to a public
// internet service (OSV.dev, bestpractices.dev, OSS-Fuzz, deps.dev) and so
// must be skipped in offline mode.
func isOfflineUnsafe(name string) bool {
	switch {
	case strings.EqualFold(name, checks.CheckVulnerabilities):
		return true
	case strings.EqualFold(name, checks.CheckCIIBestPractices):
		return true
	case strings.EqualFold(name, checks.CheckFuzzing):
		return true
	default:
		return false
	}
}

// toScoreStats converts a raw Scorecard result into weight-1 ScoreStats. Every
// requested check is included as a key, even when Scorecard couldn't reach a
// conclusion for it (a nil stat, rendered as "N/A" in the report) — a missing
// key would otherwise be indistinguishable from a check that was never
// requested at all. Inconclusive checks carry no weight, so they never skew
// tree-wide averages.
func toScoreStats(result scorecard.Result) (*gitlabtree.ScoreStat, map[string]*gitlabtree.ScoreStat, error) {
	doc, err := docChecks.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading scorecard checks documentation: %w", err)
	}

	overall, err := result.GetAggregateScore(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("computing aggregate score: %w", err)
	}

	checkScores := make(map[string]*gitlabtree.ScoreStat, len(result.Checks))

	for _, check := range result.Checks {
		if check.Score < 0 {
			checkScores[check.Name] = nil

			zap.L().Debug("Check inconclusive",
				zap.String("check", check.Name),
				zap.String("reason", check.Reason),
				zap.Error(check.Error),
			)

			continue
		}

		checkScores[check.Name] = &gitlabtree.ScoreStat{Average: float64(check.Score), Count: 1}
	}

	var overallStat *gitlabtree.ScoreStat
	if overall >= 0 {
		overallStat = &gitlabtree.ScoreStat{Average: overall, Count: 1}
	}

	return overallStat, checkScores, nil
}
