package report_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genesary/kontrol/internal/gitlabtree"
	"github.com/genesary/kontrol/internal/report"
)

// sampleTree builds an aggregated two-group tree with one scored project,
// one project whose scan failed, and one check that stayed inconclusive, so
// every branch of the JSON and metrics renderers is exercised at once.
func sampleTree(t *testing.T) *gitlabtree.Node {
	t.Helper()

	root := &gitlabtree.Node{
		Kind: gitlabtree.KindGroup,
		Name: "GitLab instance",
		Children: []*gitlabtree.Node{
			{
				Kind:     gitlabtree.KindGroup,
				Name:     "team",
				FullPath: "team",
				Children: []*gitlabtree.Node{
					{
						Kind:     gitlabtree.KindProject,
						Name:     "demo",
						FullPath: "team/demo",
						WebURL:   "https://gitlab.example.com/team/demo",
						Score:    &gitlabtree.ScoreStat{Average: 7.5, Count: 1},
						Checks: map[string]*gitlabtree.ScoreStat{
							"License":         {Average: 10, Count: 1},
							"Vulnerabilities": nil,
						},
					},
					{
						Kind:      gitlabtree.KindProject,
						Name:      "broken",
						FullPath:  "team/broken",
						ScanError: "clone failed",
					},
				},
			},
		},
	}

	gitlabtree.Aggregate(root)

	return root
}

func TestRenderJSONWritesSelfDescribingResults(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	generatedAt := time.Date(2026, time.August, 28, 15, 20, 0, 0, time.UTC)

	err := report.RenderJSON(sampleTree(t), outputDir, generatedAt, "https://gitlab.example.com")
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(outputDir, "results.json")) //nolint:gosec // test-controlled path under t.TempDir()
	if err != nil {
		t.Fatalf("reading results.json: %v", err)
	}

	var document struct {
		Root          *gitlabtree.Node `json:"root"`
		GeneratedAt   string           `json:"generatedAt"`
		GitlabURL     string           `json:"gitlabUrl"`
		SchemaVersion int              `json:"schemaVersion"`
	}

	err = json.Unmarshal(contents, &document)
	if err != nil {
		t.Fatalf("results.json is not valid JSON: %v", err)
	}

	if document.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", document.SchemaVersion)
	}

	if document.GeneratedAt != "2026-08-28T15:20:00Z" {
		t.Errorf("generatedAt = %q, want RFC3339 UTC timestamp of the run", document.GeneratedAt)
	}

	if document.GitlabURL != "https://gitlab.example.com" {
		t.Errorf("gitlabUrl = %q, want the scanned instance URL", document.GitlabURL)
	}

	if document.Root == nil {
		t.Fatal("root = nil, want the aggregated tree")
	}

	if document.Root.ProjectCount != 2 {
		t.Errorf("root.projectCount = %d, want 2", document.Root.ProjectCount)
	}

	if document.Root.Score == nil || document.Root.Score.Average != 7.5 {
		t.Errorf("root.score = %+v, want the aggregated 7.5 average", document.Root.Score)
	}
}

func TestRenderJSONFailsWhenOutputPathIsAFile(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "not-a-dir")

	err := os.WriteFile(outputDir, []byte("blocking file"), 0o600)
	if err != nil {
		t.Fatalf("seeding blocking file: %v", err)
	}

	err = report.RenderJSON(sampleTree(t), outputDir, time.Now(), "https://gitlab.example.com")
	if err == nil {
		t.Fatal("RenderJSON() error = nil, want non-nil when output.path is a file, not a directory")
	}
}

func TestRenderMetricsWritesPrometheusExposition(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	generatedAt := time.Date(2026, time.August, 28, 15, 20, 0, 0, time.UTC)

	err := report.RenderMetrics(sampleTree(t), outputDir, generatedAt, "https://gitlab.example.com")
	if err != nil {
		t.Fatalf("RenderMetrics() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(outputDir, "metrics")) //nolint:gosec // test-controlled path under t.TempDir()
	if err != nil {
		t.Fatalf("reading metrics: %v", err)
	}

	metrics := string(contents)

	wantLines := []string{
		"# TYPE kontrol_score gauge",
		`kontrol_info{gitlab_url="https://gitlab.example.com"} 1`,
		"kontrol_report_generated_at_seconds 1787930400",
		// Every level of the tree is exposed, the synthetic root under its
		// own "instance" kind rather than as a group.
		`kontrol_score{kind="instance",path=""} 7.5`,
		`kontrol_score{kind="group",path="team"} 7.5`,
		`kontrol_score{kind="project",path="team/demo"} 7.5`,
		`kontrol_check_score{kind="project",path="team/demo",check="License"} 10`,
		`kontrol_projects{kind="instance",path=""} 2`,
		`kontrol_scan_failed{kind="project",path="team/broken"} 1`,
		`kontrol_scan_failed{kind="project",path="team/demo"} 0`,
	}

	for _, want := range wantLines {
		if !strings.Contains(metrics, want+"\n") {
			t.Errorf("metrics file is missing line %q\ngot:\n%s", want, metrics)
		}
	}

	// An inconclusive check has no score to report; exporting it as 0 would
	// be indistinguishable from a genuine score of 0.
	if strings.Contains(metrics, `check="Vulnerabilities"`) {
		t.Errorf("metrics file exports an inconclusive check as a sample\ngot:\n%s", metrics)
	}

	// A project that failed to scan carries no score either.
	if strings.Contains(metrics, `kontrol_score{kind="project",path="team/broken"}`) {
		t.Errorf("metrics file exports a score for an unscanned project\ngot:\n%s", metrics)
	}
}

// TestRenderMetricsIsStable asserts the exposition is byte-identical across
// runs over the same tree, so a scraper's diff only ever reflects real
// score changes rather than Go's randomized map iteration.
func TestRenderMetricsIsStable(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, time.August, 28, 15, 20, 0, 0, time.UTC)

	render := func() string {
		t.Helper()

		outputDir := t.TempDir()

		err := report.RenderMetrics(sampleTree(t), outputDir, generatedAt, "https://gitlab.example.com")
		if err != nil {
			t.Fatalf("RenderMetrics() error = %v", err)
		}

		contents, err := os.ReadFile(filepath.Join(outputDir, "metrics")) //nolint:gosec // test-controlled path under t.TempDir()
		if err != nil {
			t.Fatalf("reading metrics: %v", err)
		}

		return string(contents)
	}

	if first, second := render(), render(); first != second {
		t.Errorf("metrics output is not stable across runs:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestRenderMetricsEscapesLabelValues asserts label values that contain
// quotes or backslashes stay parseable, since a project path is ultimately
// operator-controlled input.
func TestRenderMetricsEscapesLabelValues(t *testing.T) {
	t.Parallel()

	root := &gitlabtree.Node{
		Kind: gitlabtree.KindGroup,
		Name: "GitLab instance",
		Children: []*gitlabtree.Node{
			{
				Kind:     gitlabtree.KindProject,
				Name:     `we"ird`,
				FullPath: `team/we"ird\path`,
				Score:    &gitlabtree.ScoreStat{Average: 3, Count: 1},
			},
		},
	}
	gitlabtree.Aggregate(root)

	outputDir := t.TempDir()

	err := report.RenderMetrics(root, outputDir, time.Now(), `https://gitlab.example.com/"x"`)
	if err != nil {
		t.Fatalf("RenderMetrics() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(outputDir, "metrics")) //nolint:gosec // test-controlled path under t.TempDir()
	if err != nil {
		t.Fatalf("reading metrics: %v", err)
	}

	metrics := string(contents)

	want := `kontrol_score{kind="project",path="team/we\"ird\\path"} 3`
	if !strings.Contains(metrics, want+"\n") {
		t.Errorf("metrics file is missing escaped line %q\ngot:\n%s", want, metrics)
	}

	if !strings.Contains(metrics, `kontrol_info{gitlab_url="https://gitlab.example.com/\"x\""} 1`+"\n") {
		t.Errorf("metrics file does not escape the gitlab_url label\ngot:\n%s", metrics)
	}
}

func TestRenderMetricsFailsWhenOutputPathIsAFile(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "not-a-dir")

	err := os.WriteFile(outputDir, []byte("blocking file"), 0o600)
	if err != nil {
		t.Fatalf("seeding blocking file: %v", err)
	}

	err = report.RenderMetrics(sampleTree(t), outputDir, time.Now(), "https://gitlab.example.com")
	if err == nil {
		t.Fatal("RenderMetrics() error = nil, want non-nil when output.path is a file, not a directory")
	}
}
