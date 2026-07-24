package customchecks

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

// reportArtifactLookback bounds how many of a project's most recent
// pipelines (across all refs) are sampled for a given report artifact
// type. Recent pipelines rather than just the latest one, so the score
// reflects whether a scanner runs consistently rather than the luck of a
// single pipeline.
const reportArtifactLookback = 5

// reportArtifactScore reports whether a project's recent pipelines produce
// a job artifact of the given GitLab report file_type (e.g. "codequality",
// "sast", "secret_detection", "dependency_scanning"), on a 0-10 scale over
// the fraction of the last reportArtifactLookback pipelines that did. It is
// the shared implementation behind every security-hub-native check built on
// this pattern; each check only differs in which file_type it looks for.
func reportArtifactScore(ctx context.Context, client *gitlab.Client, projectPath, fileType string) (*gitlabtree.ScoreStat, error) {
	pipelines, _, err := client.Pipelines.ListProjectPipelines(projectPath, &gitlab.ListProjectPipelinesOptions{
		ListOptions: gitlab.ListOptions{PerPage: reportArtifactLookback},
		OrderBy:     new("id"),
		Sort:        new("desc"),
	}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("listing pipelines for %q: %w", projectPath, err)
	}

	var withReport int

	for _, pipeline := range pipelines {
		has, err := pipelineHasReportArtifact(ctx, client, projectPath, pipeline.ID, fileType)
		if err != nil {
			return nil, err
		}

		if has {
			withReport++
		}
	}

	return scoreReportArtifact(len(pipelines), withReport), nil
}

// scoreReportArtifact is the pure scoring policy shared by every
// report-artifact check, split out so it can be unit tested without a
// GitLab client. A project with no pipelines to sample is inconclusive
// rather than scored, so it renders as N/A instead of a misleading zero.
func scoreReportArtifact(pipelinesChecked, pipelinesWithReport int) *gitlabtree.ScoreStat {
	if pipelinesChecked == 0 {
		return nil
	}

	return &gitlabtree.ScoreStat{Average: proportional(pipelinesWithReport, pipelinesChecked), Count: 1}
}

// pipelineHasReportArtifact reports whether any job in the given pipeline
// produced a job artifact of the given file_type.
func pipelineHasReportArtifact(ctx context.Context, gl *gitlab.Client, projectPath string, pipelineID int64, fileType string) (bool, error) {
	jobs, _, err := gl.Jobs.ListPipelineJobs(projectPath, pipelineID, nil, gitlab.WithContext(ctx))
	if err != nil {
		return false, fmt.Errorf("listing jobs for pipeline %d of %q: %w", pipelineID, projectPath, err)
	}

	for _, job := range jobs {
		for _, artifact := range job.Artifacts {
			if artifact.FileType == fileType {
				return true, nil
			}
		}
	}

	return false, nil
}
