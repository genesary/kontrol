package customchecks

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/genesary/security-hub/internal/gitlabtree"
)

const (
	// CheckCodeQuality is reported under this name in the same
	// map[string]*gitlabtree.ScoreStat Scorecard's own checks populate.
	CheckCodeQuality = "Code-Quality"

	// codeQualityLookback bounds how many of a project's most recent pipelines
	// (across all refs) are sampled for a Code Quality report artifact. Recent
	// pipelines rather than just the latest one, so the score reflects whether
	// Code Quality scanning runs consistently rather than the luck of a single
	// pipeline.
	codeQualityLookback = 5

	// codeQualityFileType is the job artifact "file_type" GitLab assigns a
	// report declared under artifacts.reports.codequality in .gitlab-ci.yml.
	// Other report types (sast, secret_detection, coverage_report, ...) are
	// deliberately not counted: they measure different practices and folding
	// them in here would misrepresent what this check is named for.
	codeQualityFileType = "codequality"
)

// CodeQuality reports whether a project's recent pipelines produce Code
// Quality report artifacts, on a 0-10 scale over the fraction of the last
// codeQualityLookback pipelines that did.
func CodeQuality(ctx context.Context, client *gitlab.Client, projectPath string) (*gitlabtree.ScoreStat, error) {
	pipelines, _, err := client.Pipelines.ListProjectPipelines(projectPath, &gitlab.ListProjectPipelinesOptions{
		ListOptions: gitlab.ListOptions{PerPage: codeQualityLookback},
		OrderBy:     new("id"),
		Sort:        new("desc"),
	}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("listing pipelines for %q: %w", projectPath, err)
	}

	var withReport int

	for _, pipeline := range pipelines {
		has, err := pipelineHasCodeQualityReport(ctx, client, projectPath, pipeline.ID)
		if err != nil {
			return nil, err
		}

		if has {
			withReport++
		}
	}

	return scoreCodeQuality(len(pipelines), withReport), nil
}

// scoreCodeQuality is the pure scoring policy for CodeQuality, split out so
// it can be unit tested without a GitLab client. A project with no
// pipelines to sample is inconclusive rather than scored, so it renders as
// N/A instead of a misleading zero.
func scoreCodeQuality(pipelinesChecked, pipelinesWithReport int) *gitlabtree.ScoreStat {
	if pipelinesChecked == 0 {
		return nil
	}

	return &gitlabtree.ScoreStat{Average: proportional(pipelinesWithReport, pipelinesChecked), Count: 1}
}

// pipelineHasCodeQualityReport reports whether any job in the given
// pipeline produced a codequality-type artifact.
func pipelineHasCodeQualityReport(ctx context.Context, gl *gitlab.Client, projectPath string, pipelineID int64) (bool, error) {
	jobs, _, err := gl.Jobs.ListPipelineJobs(projectPath, pipelineID, nil, gitlab.WithContext(ctx))
	if err != nil {
		return false, fmt.Errorf("listing jobs for pipeline %d of %q: %w", pipelineID, projectPath, err)
	}

	for _, job := range jobs {
		for _, artifact := range job.Artifacts {
			if artifact.FileType == codeQualityFileType {
				return true, nil
			}
		}
	}

	return false, nil
}
