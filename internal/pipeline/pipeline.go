// Package pipeline wires together discovery, scanning, aggregation and
// report rendering into a single end-to-end run.
package pipeline

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/genesary/security-hub/internal/config"
	"github.com/genesary/security-hub/internal/gitlabtree"
	"github.com/genesary/security-hub/internal/report"
	"github.com/genesary/security-hub/internal/scan"
)

// unlimitedConcurrency is the errgroup.SetLimit value that removes any cap
// on the number of active goroutines, used when cfg.Scan.MaxConcurrency is
// left empty (zero) in the config file.
const unlimitedConcurrency = -1

// Run discovers the GitLab hierarchy described by cfg, runs Scorecard
// against every project it finds, aggregates the results into
// project-count-weighted averages, and renders the HTML report.
func Run(ctx context.Context, cfg *config.Config) error {
	zap.L().Info("Starting security-hub scan",
		zap.String("gitlab", cfg.Gitlab.URL),
		zap.Bool("offline", cfg.Scorecard.Offline),
		zap.Bool("experimental", cfg.Scorecard.Experimental))

	host, err := hostOf(cfg.Gitlab.URL)
	if err != nil {
		return err
	}

	client, err := gitlab.NewClient(cfg.Gitlab.Token, gitlab.WithBaseURL(cfg.Gitlab.URL))
	if err != nil {
		return fmt.Errorf("creating gitlab client: %w", err)
	}

	root, err := gitlabtree.Discover(ctx, client, cfg.Gitlab.Filters)
	if err != nil {
		return fmt.Errorf("discovering gitlab hierarchy: %w", err)
	}

	projects := leafProjects(root)
	zap.L().Info("Discovery complete", zap.Int("projects", len(projects)))

	scanOpts := scan.Options{
		Host:         host,
		Token:        cfg.Gitlab.Token,
		Checks:       cfg.Scorecard.Checks,
		CustomChecks: cfg.CustomScores,
		Offline:      cfg.Scorecard.Offline,
		GitlabClient: client,
		Weights:      cfg.Weights,
	}

	err = setScorecardEnv(cfg)
	if err != nil {
		return err
	}

	err = scanProjects(ctx, scanOpts, projects, cfg.Scorecard.MaxConcurrency)
	if err != nil {
		return err
	}

	gitlabtree.Aggregate(root)
	zap.L().Info("Aggregation complete", zap.Float64("overallScore", overallScore(root)))

	err = report.Render(root, cfg.Output.Path, time.Now(), cfg.Gitlab.URL)
	if err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}

	zap.L().Info("Report written", zap.String("path", cfg.Output.Path))

	return nil
}

// setScorecardEnv exports the env vars Scorecard's GitLab client reads
// directly, independent of the token/options passed to it as a library.
func setScorecardEnv(cfg *config.Config) error {
	// Scorecard's GitLab client reads this env var directly (independent of
	// the token passed to gitlabrepo.CreateGitlabClientWithToken) for its
	// GraphQL-based merge request lookups and tarball download auth header.
	err := os.Setenv("GITLAB_AUTH_TOKEN", cfg.Gitlab.Token)
	if err != nil {
		return fmt.Errorf("setting GITLAB_AUTH_TOKEN: %w", err)
	}

	if !cfg.Scorecard.Experimental {
		return nil
	}

	// Scorecard gates the SBOM check (and Webhooks, though that one stays
	// excluded on GitLab regardless, see config.Scorecard.Experimental)
	// behind this env var, checked inline in the check function rather than
	// exposed as a library option.
	err = os.Setenv("SCORECARD_EXPERIMENTAL", "1")
	if err != nil {
		return fmt.Errorf("setting SCORECARD_EXPERIMENTAL: %w", err)
	}

	return nil
}

func overallScore(root *gitlabtree.Node) float64 {
	if root.Score == nil {
		return -1
	}

	return root.Score.Average
}

// hostOf extracts the bare host (with port, if any, but no scheme) from a
// GitLab instance URL, matching the addressing Scorecard's gitlabrepo client
// expects.
func hostOf(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing gitlab url %q: %w", rawURL, err)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("gitlab url %q has no host", rawURL)
	}

	return parsed.Host, nil
}

// scanProjects runs Scorecard against every given project, up to
// maxConcurrency at a time. A maxConcurrency of zero (the config file's
// empty value) or negative means no limit. Individual scan failures are
// recorded on their node rather than aborting the run.
func scanProjects(ctx context.Context, opts scan.Options, projects []*gitlabtree.Node, maxConcurrency int) error {
	limit := unlimitedConcurrency
	concurrencyField := zap.String("concurrency", "unlimited")

	if maxConcurrency > 0 {
		limit = maxConcurrency
		concurrencyField = zap.Int("concurrency", limit)
	}

	zap.L().Info("Scanning projects", zap.Int("projects", len(projects)), concurrencyField)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(limit)

	for _, project := range projects {
		group.Go(func() error {
			scanOne(groupCtx, opts, project)

			return nil
		})
	}

	return group.Wait() //nolint:wrapcheck // scanOne never returns an error; Wait can only surface ctx cancellation
}

// scanOne runs Scorecard against a single project and stores the result
// directly on its node. Scan failures are recorded as ScanError rather than
// returned, so a handful of unreachable projects don't prevent reporting on
// the rest of the instance.
func scanOne(ctx context.Context, opts scan.Options, node *gitlabtree.Node) {
	zap.L().Debug("Scanning project", zap.String("project", node.FullPath))

	score, checkScores, err := scan.Project(ctx, opts, node.FullPath)
	if err != nil {
		zap.L().Warn("Scan failed", zap.String("project", node.FullPath), zap.Error(err))

		node.ScanError = err.Error()

		return
	}

	node.Score = score
	node.Checks = checkScores

	scoreField := zap.Skip()
	if score != nil {
		scoreField = zap.Float64("score", score.Average)
	}

	zap.L().Debug("Scan complete", zap.String("project", node.FullPath), scoreField)
}

func leafProjects(node *gitlabtree.Node) []*gitlabtree.Node {
	if node.Kind == gitlabtree.KindProject {
		return []*gitlabtree.Node{node}
	}

	var projects []*gitlabtree.Node

	for _, child := range node.Children {
		projects = append(projects, leafProjects(child)...)
	}

	return projects
}
