package customchecks

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

// reportArtifactLookback bounds how many of a project's most recent
// pipelines are sampled for a given report artifact type. Recent pipelines
// rather than just the latest one, so the score reflects whether a scanner
// runs consistently rather than the luck of a single pipeline.
const reportArtifactLookback = 5

// jobsPageSize is how many of a pipeline's jobs are fetched per request.
// GitLab defaults to 20 when no per_page is sent, which is easily exceeded
// by a large pipeline, so it is set explicitly and every page is walked: a
// scanner job missed because it fell past the first page would silently
// score the project as if it ran no scanner at all.
const jobsPageSize = 100

// reportArtifactFileTypes maps each security-hub-native check built on the
// report-artifact pattern to the GitLab report file_type it looks for. These
// checks are identical apart from that file type, which is what lets them all
// be answered from a single pass over a project's pipelines.
func reportArtifactFileTypes() map[string]string {
	return map[string]string{
		CheckCodeQuality:        codeQualityFileType,
		CheckDependencyScanning: dependencyScanningFileType,
		CheckSAST:               sastFileType,
		CheckSecretDetection:    secretDetectionFileType,
	}
}

// IsReportArtifactCheck reports whether name is one of the checks built on
// the report-artifact pattern, so callers can batch them together instead of
// running them one at a time.
func IsReportArtifactCheck(name string) bool {
	_, ok := reportArtifactFileTypes()[name]

	return ok
}

// ReportArtifactScores scores every named report-artifact check on a 0-10
// scale over the fraction of the project's last reportArtifactLookback
// successful default-branch pipelines that produced the check's report
// artifact (see sampledPipelines for why the sample is scoped that way).
// Every requested check is answered from one pass over those pipelines: they
// all read the same jobs and differ only in which artifact file type they
// look for, so scoring them together costs one pipeline listing per project
// instead of one per check. Names that aren't report-artifact checks are
// ignored.
func ReportArtifactScores(
	ctx context.Context, client *gitlab.Client, projectPath, defaultBranch string, names []string,
) (map[string]*gitlabtree.ScoreStat, error) {
	// Inverted (file type -> check name) so each artifact seen below costs a
	// single map lookup rather than a scan of every requested check.
	checkByFileType := make(map[string]string, len(names))
	fileTypes := reportArtifactFileTypes()

	for _, name := range names {
		if fileType, ok := fileTypes[name]; ok {
			checkByFileType[fileType] = name
		}
	}

	if len(checkByFileType) == 0 {
		return map[string]*gitlabtree.ScoreStat{}, nil
	}

	pipelines, err := sampledPipelines(ctx, client, projectPath, defaultBranch)
	if err != nil {
		return nil, err
	}

	withReport := make(map[string]int, len(checkByFileType))

	for _, pipeline := range pipelines {
		found, err := pipelineReportArtifacts(ctx, client, projectPath, pipeline.ID, checkByFileType)
		if err != nil {
			return nil, err
		}

		for name := range found {
			withReport[name]++
		}
	}

	scores := make(map[string]*gitlabtree.ScoreStat, len(checkByFileType))
	for _, name := range checkByFileType {
		scores[name] = scoreReportArtifact(len(pipelines), withReport[name])
	}

	return scores, nil
}

// sampledPipelines returns the pipelines a report-artifact check scores
// over: the most recent successful ones on the project's default branch.
//
// Both filters exist to keep the score answering "does this project run the
// scanner" rather than "what happened to run last". Without the status
// filter, a pipeline that is still running, or was canceled or skipped, has
// no finished artifacts and would count against the project purely for
// existing. Without the ref filter, a burst of feature-branch or merge
// request pipelines that skip scanners would sink a project whose default
// branch runs them on every commit.
//
// A project whose default branch is unknown (no commits yet) falls back to
// sampling every ref; one with no successful pipelines at all yields an
// empty sample, which scores as inconclusive (N/A) rather than zero.
func sampledPipelines(
	ctx context.Context, client *gitlab.Client, projectPath, defaultBranch string,
) ([]*gitlab.PipelineInfo, error) {
	opts := &gitlab.ListProjectPipelinesOptions{
		ListOptions: gitlab.ListOptions{PerPage: reportArtifactLookback},
		OrderBy:     new("id"),
		Sort:        new("desc"),
		Status:      new(gitlab.Success),
	}

	if defaultBranch != "" {
		opts.Ref = new(defaultBranch)
	}

	pipelines, _, err := client.Pipelines.ListProjectPipelines(projectPath, opts, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("listing pipelines for %q: %w", projectPath, err)
	}

	return pipelines, nil
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

// pipelineReportArtifacts returns the set of checks (by name) whose report
// file_type was produced by at least one job in the given pipeline, walking
// every page of the pipeline's jobs rather than only the first.
func pipelineReportArtifacts(
	ctx context.Context, client *gitlab.Client, projectPath string, pipelineID int64, checkByFileType map[string]string,
) (map[string]bool, error) {
	found := make(map[string]bool, len(checkByFileType))

	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{PerPage: jobsPageSize, Page: 1},
	}

	for {
		jobs, resp, err := client.Jobs.ListPipelineJobs(projectPath, pipelineID, opts, gitlab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("listing jobs for pipeline %d of %q: %w", pipelineID, projectPath, err)
		}

		for _, job := range jobs {
			for _, artifact := range job.Artifacts {
				if name, ok := checkByFileType[artifact.FileType]; ok {
					found[name] = true
				}
			}
		}

		// Nothing left to learn from this pipeline once every requested check
		// has been seen, so stop paging early.
		if len(found) == len(checkByFileType) || resp.NextPage == 0 {
			return found, nil
		}

		opts.Page = resp.NextPage
	}
}
