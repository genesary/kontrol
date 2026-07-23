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
	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"

	"github.com/genesary/security-hub/internal/customchecks"
	"github.com/genesary/security-hub/internal/gitlabtree"
)

// Options configures how Scorecard analyzes each project.
type Options struct {
	GitlabClient *gitlab.Client
	Host         string
	Token        string
	Checks       []string
	// CustomChecks lists the security-hub-native checks (internal/customchecks)
	// to run, by name. Unlike Checks, an empty list means none of them run,
	// not all of them: these checks make extra GitLab API calls per project,
	// so they stay opt-in.
	CustomChecks []string
	Offline      bool
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

	enabledCustomChecks := toSet(opts.CustomChecks)

	if enabledCustomChecks[customchecks.CheckCodeQuality] {
		runCustomCheck(checkScores, customchecks.CheckCodeQuality, fullPath, func() (*gitlabtree.ScoreStat, error) {
			return customchecks.CodeQuality(ctx, opts.GitlabClient, fullPath)
		})
	}

	if enabledCustomChecks[customchecks.CheckContributors] {
		runCustomCheck(checkScores, customchecks.CheckContributors, fullPath, func() (*gitlabtree.ScoreStat, error) {
			return customchecks.Contributors(ctx, opts.GitlabClient, fullPath)
		})
	}

	return overall, checkScores, nil
}

// runCustomCheck computes a security-hub-native check and records its result
// under name. Unlike a Scorecard-side failure, an error here is logged and
// recorded as nil (rendered as "N/A") rather than propagated: a flaky custom
// check must not abort the whole project's scan.
func runCustomCheck(
	checkScores map[string]*gitlabtree.ScoreStat, name, fullPath string, run func() (*gitlabtree.ScoreStat, error),
) {
	stat, err := run()
	if err != nil {
		zap.L().Warn("Custom check failed",
			zap.String("check", name),
			zap.String("project", fullPath),
			zap.Error(err),
		)

		checkScores[name] = nil

		return
	}

	checkScores[name] = stat
}

// toSet converts a name list into a membership set, so callers can check
// "is this name requested" in constant time regardless of list length.
func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))

	for _, name := range names {
		set[name] = true
	}

	return set
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
// conclusion for it (a nil stat, rendered as "N/A" in the report). A missing
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
