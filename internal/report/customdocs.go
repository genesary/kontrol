package report

import "github.com/genesary/security-hub/internal/customchecks"

// customCheckDocs returns hand-written documentation for security-hub's own
// checks (internal/customchecks), which have no Scorecard-authored doc to
// borrow. Or, for Contributors and SAST, would be actively misleading if
// borrowed, since Scorecard's versions of those checks describe things
// GitLab's API can't provide (organizational diversity) or don't apply here
// (GitHub/Azure DevOps app detection). These entries are merged into
// collectCheckDocs's result after Scorecard's own docs, so they always win
// for these names.
func customCheckDocs() map[string]checkDoc {
	return map[string]checkDoc{
		customchecks.CheckCodeQuality: {
			Short: "Recent pipelines produce a Code Quality report",
			Description: "Checks whether the project's most recent pipelines upload a Code Climate-format " +
				"report via artifacts.reports.codequality in .gitlab-ci.yml. This is a linting/code-style " +
				"signal, not a security scan, it is unrelated to SAST, secret detection, or dependency " +
				"scanning.",
			Remediation: []string{
				"Add a job that runs a Code Quality analysis and uploads its report under " +
					"artifacts.reports.codequality.",
				"GitLab ships a ready-made Code-Quality.gitlab-ci.yml template you can include as a fast " +
					"path instead of writing the job by hand.",
				"A partial (neither 0 nor 10) score means the job isn't running on every pipeline, check " +
					"that it isn't restricted to a branch, tag, or rule that recent pipelines don't match.",
			},
		},
		customchecks.CheckContributors: {
			Short: "Bus factor: distinct commit authors on the default branch",
			Description: "Counts distinct commit authors on the project's default branch via GitLab's " +
				"contributors API. Unlike Scorecard's GitHub-based Contributors check, GitLab's REST API " +
				"exposes no organization or company affiliation for a contributor, so this measures raw " +
				"headcount (bus factor) rather than organizational diversity.",
			Remediation: []string{
				"A single contributor scores the minimum: no one else can review changes or step in if " +
					"they're unavailable.",
				"Score rises with each additional distinct commit author and reaches the maximum at six or " +
					"more, so spreading authorship and reviews across more people raises it.",
			},
		},
		customchecks.CheckSAST: {
			Short: "Recent pipelines produce a SAST report",
			Description: "Checks whether the project's most recent pipelines upload a report via " +
				"artifacts.reports.sast in .gitlab-ci.yml. This is a security-hub-native reimplementation: " +
				"Scorecard's own SAST check only recognizes GitHub's CodeQL and SonarCloud apps, so it never " +
				"finds a signal on GitLab.",
			Remediation: []string{
				"Add a job that runs a static analysis security testing scanner and uploads its report " +
					"under artifacts.reports.sast.",
				"GitLab ships a ready-made SAST.gitlab-ci.yml template you can include as a fast path " +
					"instead of writing the job by hand.",
				"A partial (neither 0 nor 10) score means the job isn't running on every pipeline, check " +
					"that it isn't restricted to a branch, tag, or rule that recent pipelines don't match.",
			},
		},
		customchecks.CheckSecretDetection: {
			Short: "Recent pipelines produce a Secret-Detection report",
			Description: "Checks whether the project's most recent pipelines upload a report via " +
				"artifacts.reports.secret_detection in .gitlab-ci.yml. security-hub-native, not a " +
				"Scorecard check: Scorecard has no equivalent check on any platform.",
			Remediation: []string{
				"Add a job that runs a secret-scanning tool and uploads its report under " +
					"artifacts.reports.secret_detection.",
				"GitLab ships a ready-made Secret-Detection.gitlab-ci.yml template you can include as a " +
					"fast path instead of writing the job by hand.",
				"A partial (neither 0 nor 10) score means the job isn't running on every pipeline, check " +
					"that it isn't restricted to a branch, tag, or rule that recent pipelines don't match.",
			},
		},
	}
}
