package report_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genesary/security-hub/internal/gitlabtree"
	"github.com/genesary/security-hub/internal/report"
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
