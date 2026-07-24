package report_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
	"github.com/boxboxjason/security-hub/internal/report"
)

func TestRenderWritesValidReport(t *testing.T) {
	t.Parallel()

	root := &gitlabtree.Node{
		Kind: gitlabtree.KindGroup,
		Name: "root",
		Children: []*gitlabtree.Node{
			{
				Kind:         gitlabtree.KindProject,
				Name:         "demo",
				FullPath:     "group/demo",
				ProjectCount: 1,
				Score:        &gitlabtree.ScoreStat{Average: 7.5, Count: 1},
			},
		},
	}
	gitlabtree.Aggregate(root)

	outputDir := t.TempDir()

	err := report.Render(root, outputDir, time.Now(), "https://gitlab.example.com")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(outputDir, "index.html")) //nolint:gosec // test-controlled path under t.TempDir()
	if err != nil {
		t.Fatalf("reading rendered report: %v", err)
	}

	html := string(contents)

	const marker = `<script id="report-data" type="application/json">`

	start := strings.Index(html, marker)
	if start == -1 {
		t.Fatalf("report-data script tag not found in rendered HTML")
	}

	start += len(marker)

	end := strings.Index(html[start:], "</script>")
	if end == -1 {
		t.Fatalf("closing script tag not found in rendered HTML")
	}

	var decoded gitlabtree.Node

	err = json.Unmarshal([]byte(html[start:start+end]), &decoded)
	if err != nil {
		t.Fatalf("embedded report data is not valid JSON: %v", err)
	}

	if decoded.ProjectCount != 1 {
		t.Fatalf("decoded.ProjectCount = %d, want 1", decoded.ProjectCount)
	}

	for _, asset := range []string{
		filepath.Join("static", "css", "app.css"),
		filepath.Join("static", "js", "report.js"),
		filepath.Join("static", "js", "export.js"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, asset)); err != nil {
			t.Fatalf("expected static asset %q to be copied: %v", asset, err)
		}
	}
}

func TestRenderIncludesCheckDocumentation(t *testing.T) {
	t.Parallel()

	root := &gitlabtree.Node{
		Kind: gitlabtree.KindGroup,
		Name: "root",
		Children: []*gitlabtree.Node{
			{
				Kind:         gitlabtree.KindProject,
				Name:         "demo",
				FullPath:     "group/demo",
				ProjectCount: 1,
				Score:        &gitlabtree.ScoreStat{Average: 7.5, Count: 1},
				Checks: map[string]*gitlabtree.ScoreStat{
					"Maintained":       {Average: 8, Count: 1},
					"Contributors":     {Average: 4, Count: 1},
					"Not-A-Real-Check": {Average: 0, Count: 1},
				},
			},
		},
	}
	gitlabtree.Aggregate(root)

	outputDir := t.TempDir()

	err := report.Render(root, outputDir, time.Now(), "https://gitlab.example.com")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(outputDir, "index.html")) //nolint:gosec // test-controlled path under t.TempDir()
	if err != nil {
		t.Fatalf("reading rendered report: %v", err)
	}

	html := string(contents)

	const marker = `<script id="check-docs-data" type="application/json">`

	start := strings.Index(html, marker)
	if start == -1 {
		t.Fatalf("check-docs-data script tag not found in rendered HTML")
	}

	start += len(marker)

	end := strings.Index(html[start:], "</script>")
	if end == -1 {
		t.Fatalf("closing script tag not found in rendered HTML")
	}

	var docs map[string]map[string]any

	err = json.Unmarshal([]byte(html[start:start+end]), &docs)
	if err != nil {
		t.Fatalf("embedded check docs are not valid JSON: %v", err)
	}

	if _, ok := docs["Maintained"]; !ok {
		t.Errorf(`docs["Maintained"] missing, want Scorecard-authored documentation`)
	}

	if _, ok := docs["Contributors"]; !ok {
		t.Errorf(`docs["Contributors"] missing, want security-hub's own custom-check documentation`)
	}

	if _, ok := docs["Not-A-Real-Check"]; ok {
		t.Errorf(`docs["Not-A-Real-Check"] present, want it silently omitted (no documentation source has it)`)
	}
}

func TestRenderFailsOnUnmarshalableData(t *testing.T) {
	t.Parallel()

	root := &gitlabtree.Node{
		Kind:  gitlabtree.KindProject,
		Name:  "demo",
		Score: &gitlabtree.ScoreStat{Average: math.NaN(), Count: 1},
	}

	err := report.Render(root, t.TempDir(), time.Now(), "https://gitlab.example.com")
	if err == nil {
		t.Fatal("Render() error = nil, want non-nil since NaN cannot be marshaled to JSON")
	}
}

func TestRenderFailsWhenOutputDirIsAFile(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "not-a-dir")

	err := os.WriteFile(outputDir, []byte("blocking file"), 0o600)
	if err != nil {
		t.Fatalf("seeding blocking file: %v", err)
	}

	root := &gitlabtree.Node{Kind: gitlabtree.KindProject, Name: "demo"}

	err = report.Render(root, outputDir, time.Now(), "https://gitlab.example.com")
	if err == nil {
		t.Fatal("Render() error = nil, want non-nil when output.path is a file, not a directory")
	}
}

func TestRenderFailsWhenStaticAssetPathIsBlocked(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	err := os.WriteFile(filepath.Join(outputDir, "static"), []byte("blocking file"), 0o600)
	if err != nil {
		t.Fatalf("seeding blocking file: %v", err)
	}

	root := &gitlabtree.Node{Kind: gitlabtree.KindProject, Name: "demo"}

	err = report.Render(root, outputDir, time.Now(), "https://gitlab.example.com")
	if err == nil {
		t.Fatal("Render() error = nil, want non-nil when a static asset path is blocked by an existing file")
	}
}

func TestRenderFailsWhenIndexHTMLPathIsADirectory(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	err := os.Mkdir(filepath.Join(outputDir, "index.html"), 0o700)
	if err != nil {
		t.Fatalf("seeding blocking directory: %v", err)
	}

	root := &gitlabtree.Node{Kind: gitlabtree.KindProject, Name: "demo"}

	err = report.Render(root, outputDir, time.Now(), "https://gitlab.example.com")
	if err == nil {
		t.Fatal("Render() error = nil, want non-nil when index.html already exists as a directory")
	}
}
