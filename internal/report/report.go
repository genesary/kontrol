// Package report renders the aggregated GitLab tree into a static,
// relocatable HTML report (index.html plus its static/ CSS and JS assets)
// with client-side drill-down navigation.
package report

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	docChecks "github.com/ossf/scorecard/v5/docs/checks"
	"go.uber.org/zap"

	"github.com/genesary/kontrol/internal/gitlabtree"
)

//go:embed templates/report.html.tmpl
var reportTemplateSource string

// staticAssets holds the report's CSS and JS, compiled and hand-written
// respectively under static/ (see internal/report/tailwind for the Tailwind
// source and `make frontend` for how static/css/app.css is regenerated).
// They are copied next to index.html on every Render so the report stays a
// self-contained, relocatable output directory.
//
//go:embed static
var staticAssets embed.FS

const staticAssetsRoot = "static"

const (
	outputFileName = "index.html"
	// The report is a static file meant to be served or opened directly by
	// anyone with access to the output directory, so it is world-readable.
	outputDirPermission  = 0o755
	outputFilePermission = 0o644
)

// templateData is the set of values injected into the report template.
type templateData struct {
	GeneratedAt   string
	GitlabURL     string
	DataJSON      template.JS
	CheckDocsJSON template.JS
}

// checkDoc carries the Scorecard-authored documentation for a single check,
// so the report can explain what a check means and how to fix a low score
// without the viewer having to leave the page.
type checkDoc struct {
	Short       string   `json:"short"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Remediation []string `json:"remediation"`
}

// Render writes a self-contained HTML report for the given tree to
// outputDir/index.html, creating outputDir if it does not already exist.
// gitlabURL is the scanned GitLab instance's base URL, shown at the top of
// the report so a reader knows which instance the data came from.
func Render(root *gitlabtree.Node, outputDir string, generatedAt time.Time, gitlabURL string) error {
	zap.L().Debug("Rendering report", zap.String("outputDir", outputDir))

	dataJSON, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshaling report data: %w", err)
	}

	checkDocsJSON, err := json.Marshal(collectCheckDocs(root))
	if err != nil {
		return fmt.Errorf("marshaling check documentation: %w", err)
	}

	tmpl, err := template.New("report").Parse(reportTemplateSource)
	if err != nil {
		return fmt.Errorf("parsing report template: %w", err)
	}

	err = os.MkdirAll(outputDir, outputDirPermission)
	if err != nil {
		return fmt.Errorf("creating output directory %q: %w", outputDir, err)
	}

	err = copyStaticAssets(outputDir)
	if err != nil {
		return fmt.Errorf("copying static assets: %w", err)
	}

	outputPath := filepath.Join(outputDir, outputFileName)

	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, outputFilePermission) //nolint:gosec // report output is meant to be world-readable
	if err != nil {
		return fmt.Errorf("creating report file %q: %w", outputPath, err)
	}

	data := templateData{
		GeneratedAt:   generatedAt.Format(time.RFC1123),
		GitlabURL:     gitlabURL,
		DataJSON:      template.JS(dataJSON),      //nolint:gosec // dataJSON is our own marshaled struct, not attacker-controlled
		CheckDocsJSON: template.JS(checkDocsJSON), //nolint:gosec // checkDocsJSON is our own marshaled struct, not attacker-controlled
	}

	err = tmpl.Execute(file, data)
	if err != nil {
		_ = file.Close()

		return fmt.Errorf("rendering report template: %w", err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("closing report file %q: %w", outputPath, err)
	}

	return nil
}

// writeArtifact drops a single generated file (data) into outputDir under
// fileName, creating outputDir first with the same permissions Render uses.
// It is shared by the non-HTML renderers (RenderJSON, RenderMetrics), which
// all write exactly one file into the report directory.
func writeArtifact(outputDir, fileName string, data []byte) error {
	err := os.MkdirAll(outputDir, outputDirPermission)
	if err != nil {
		return fmt.Errorf("creating output directory %q: %w", outputDir, err)
	}

	outputPath := filepath.Join(outputDir, fileName)

	err = os.WriteFile(outputPath, data, outputFilePermission)
	if err != nil {
		return fmt.Errorf("writing %q: %w", outputPath, err)
	}

	return nil
}

// copyStaticAssets copies the embedded static/ tree (compiled CSS and
// hand-written JS) into outputDir, preserving its static/css/... and
// static/js/... layout so the paths referenced by the report template
// resolve relative to index.html.
func copyStaticAssets(outputDir string) error {
	err := fs.WalkDir(staticAssets, staticAssetsRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		destPath := filepath.Join(outputDir, path)

		if entry.IsDir() {
			return os.MkdirAll(destPath, outputDirPermission)
		}

		contents, err := staticAssets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded asset %q: %w", path, err)
		}

		return os.WriteFile(destPath, contents, outputFilePermission)
	})
	if err != nil {
		return fmt.Errorf("walking embedded static assets: %w", err)
	}

	return nil
}

// collectCheckDocs looks up Scorecard's own documentation for every check
// name that appears anywhere in the tree, so the report can show it without
// the viewer needing to know Scorecard's docs site. Checks whose
// documentation can't be loaded are silently omitted rather than failing the
// whole report.
func collectCheckDocs(root *gitlabtree.Node) map[string]checkDoc {
	names := make(map[string]struct{})
	collectCheckNames(root, names)

	checkDocs := make(map[string]checkDoc, len(names))

	docs, err := docChecks.Read()
	if err != nil {
		zap.L().Warn("Failed to load Scorecard check documentation", zap.Error(err))
	} else {
		for name := range names {
			doc, err := docs.GetCheck(name)
			if err != nil {
				zap.L().Warn("No Scorecard documentation for check", zap.String("check", name), zap.Error(err))

				continue
			}

			checkDocs[name] = checkDoc{
				Short:       doc.GetShort(),
				Description: doc.GetDescription(),
				Remediation: doc.GetRemediation(),
				URL:         doc.GetDocumentationURL(""),
			}
		}
	}

	for name, doc := range customCheckDocs() {
		if _, requested := names[name]; requested {
			checkDocs[name] = doc
		}
	}

	return checkDocs
}

// collectCheckNames walks the tree collecting the union of every check name
// referenced at any level, since a check can be present at one node and
// absent (never requested) at another.
func collectCheckNames(node *gitlabtree.Node, names map[string]struct{}) {
	for name := range node.Checks {
		names[name] = struct{}{}
	}

	for _, child := range node.Children {
		collectCheckNames(child, names)
	}
}
