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

	"github.com/boxboxjason/security-hub/internal/customchecks"
	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

// Options configures how Scorecard analyzes each project.
type Options struct {
	GitlabClient *gitlab.Client
	// Weights overrides how much each check (by name, Scorecard's or one
	// listed in CustomChecks) counts toward the overall score. A name with
	// no entry defaults to weight 1; a weight of 0 means the check is
	// skipped entirely rather than merely excluded from the average.
	Weights map[string]int
	Host    string
	Token   string
	Checks  []string
	// CustomChecks lists the security-hub-native checks (internal/customchecks)
	// to run, by name. Unlike Checks, an empty list means none of them run,
	// not all of them: these checks make extra GitLab API calls per project,
	// so they stay opt-in.
	CustomChecks []string
	Offline      bool
}

// defaultWeight is the weight assumed for any check with no entry in
// Options.Weights.
const defaultWeight = 1

// weightFor looks up name's configured weight, defaulting to defaultWeight
// when unset.
func weightFor(name string, weights map[string]int) int {
	if w, ok := weights[name]; ok {
		return w
	}

	return defaultWeight
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

	checkScores := toScoreStats(result, opts.Weights)

	// Checks skipped by our own offline filter never reach Scorecard, so they
	// never appear in result.Checks. Add them as nil (rendered as "N/A") so
	// they're still visible in the report instead of silently vanishing.
	for _, name := range skippedOffline {
		checkScores[name] = nil
	}

	runCustomChecks(ctx, opts, fullPath, checkScores)

	overall, err := overallScoreFor(result, checkScores, opts.Weights)
	if err != nil {
		return nil, nil, fmt.Errorf("scoring scorecard result for %q: %w", fullPath, err)
	}

	return overall, checkScores, nil
}

// overallScoreFor computes a project's overall score. With no weights
// configured, it defers entirely to Scorecard's own risk-tier-weighted
// GetAggregateScore, unchanged from security-hub's original behavior (which
// never sees CustomScores checks). As soon as any weight is configured, it
// instead combines every check present in checkScores (Scorecard's and
// CustomScores', already Count-weighted by toScoreStats/runCustomCheck)
// into security-hub's own weighted mean, so weights actually change the
// number.
func overallScoreFor(
	result scorecard.Result, checkScores map[string]*gitlabtree.ScoreStat, weights map[string]int,
) (*gitlabtree.ScoreStat, error) {
	if len(weights) == 0 {
		doc, err := docChecks.Read()
		if err != nil {
			return nil, fmt.Errorf("reading scorecard checks documentation: %w", err)
		}

		overall, err := result.GetAggregateScore(doc)
		if err != nil {
			return nil, fmt.Errorf("computing aggregate score: %w", err)
		}

		if overall < 0 {
			return nil, nil //nolint:nilnil // nil, nil means "inconclusive", not a failure
		}

		return &gitlabtree.ScoreStat{Average: overall, Count: 1}, nil
	}

	stats := make([]*gitlabtree.ScoreStat, 0, len(checkScores))
	for _, stat := range checkScores {
		stats = append(stats, stat)
	}

	combined := gitlabtree.Combine(stats)
	if combined == nil {
		return nil, nil //nolint:nilnil // nil, nil means "inconclusive", not a failure
	}

	return &gitlabtree.ScoreStat{Average: combined.Average, Count: 1}, nil
}

// customCheckRunner pairs a security-hub-native check's name with the
// closure that computes it, so runCustomChecks can dispatch every check
// through one loop instead of one enabled/weight branch per check: that
// branch count is what previously drove Project's cyclomatic complexity
// over its lint threshold as checks were added.
type customCheckRunner struct {
	run  func() (*gitlabtree.ScoreStat, error)
	name string
}

// runCustomChecks runs every enabled, non-zero-weight security-hub-native
// check for a project and records its result in checkScores.
func runCustomChecks(ctx context.Context, opts Options, fullPath string, checkScores map[string]*gitlabtree.ScoreStat) {
	runners := []customCheckRunner{
		{name: customchecks.CheckCodeQuality, run: func() (*gitlabtree.ScoreStat, error) {
			return customchecks.CodeQuality(ctx, opts.GitlabClient, fullPath)
		}},
		{name: customchecks.CheckContributors, run: func() (*gitlabtree.ScoreStat, error) {
			return customchecks.Contributors(ctx, opts.GitlabClient, fullPath)
		}},
		{name: customchecks.CheckDependencyScanning, run: func() (*gitlabtree.ScoreStat, error) {
			return customchecks.DependencyScanning(ctx, opts.GitlabClient, fullPath)
		}},
		{name: customchecks.CheckSAST, run: func() (*gitlabtree.ScoreStat, error) {
			return customchecks.SAST(ctx, opts.GitlabClient, fullPath)
		}},
		{name: customchecks.CheckSecretDetection, run: func() (*gitlabtree.ScoreStat, error) {
			return customchecks.SecretDetection(ctx, opts.GitlabClient, fullPath)
		}},
	}

	enabled := toSet(opts.CustomChecks)

	for _, runner := range runners {
		if !enabled[runner.name] {
			continue
		}

		weight := weightFor(runner.name, opts.Weights)
		if weight == 0 {
			continue
		}

		runCustomCheck(checkScores, runner.name, weight, fullPath, runner.run)
	}
}

// runCustomCheck computes a security-hub-native check and records its result
// under name, rescaled to weight. Unlike a Scorecard-side failure, an error
// here is logged and recorded as nil (rendered as "N/A") rather than
// propagated: a flaky custom check must not abort the whole project's scan.
func runCustomCheck(
	checkScores map[string]*gitlabtree.ScoreStat, name string, weight int, fullPath string, run func() (*gitlabtree.ScoreStat, error),
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

	stat.Count = weight
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
// they require internet access. A weight of 0 drops a check from the
// candidate list entirely, before the offline split, so it's never sent to
// Scorecard and never appears in the report, not even as "N/A".
func resolveChecks(opts Options) ([]string, []string) {
	candidates := opts.Checks
	if len(candidates) == 0 {
		candidates = make([]string, 0, len(checks.GetAll()))
		for name := range checks.GetAll() {
			candidates = append(candidates, name)
		}
	}

	candidates = withoutZeroWeight(candidates, opts.Weights)

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

// withoutZeroWeight drops any name whose configured weight is exactly 0. An
// empty weights map is a no-op fast path, so behavior is unchanged when
// weights aren't configured at all.
func withoutZeroWeight(names []string, weights map[string]int) []string {
	if len(weights) == 0 {
		return names
	}

	kept := make([]string, 0, len(names))

	for _, name := range names {
		if weightFor(name, weights) != 0 {
			kept = append(kept, name)
		}
	}

	return kept
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

// toScoreStats converts a raw Scorecard result into ScoreStats weighted per
// opts.Weights (weight 1 for any check with no entry). Every requested check
// is included as a key, even when Scorecard couldn't reach a conclusion for
// it (a nil stat, rendered as "N/A" in the report). A missing key would
// otherwise be indistinguishable from a check that was never requested at
// all. Inconclusive checks carry no weight, so they never skew tree-wide
// averages.
func toScoreStats(result scorecard.Result, weights map[string]int) map[string]*gitlabtree.ScoreStat {
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

		checkScores[check.Name] = &gitlabtree.ScoreStat{Average: float64(check.Score), Count: weightFor(check.Name, weights)}
	}

	return checkScores
}
