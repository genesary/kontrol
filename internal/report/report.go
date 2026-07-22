// Package report renders the aggregated GitLab tree into a single
// self-contained HTML file with client-side drill-down navigation.
package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	docChecks "github.com/ossf/scorecard/v5/docs/checks"
	"go.uber.org/zap"

	"github.com/genesary/security-hub/internal/gitlabtree"
)

//go:embed templates/report.html.tmpl
var reportTemplateSource string

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
	Remediation []string `json:"remediation"`
	URL         string   `json:"url"`
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

// collectCheckDocs looks up Scorecard's own documentation for every check
// name that appears anywhere in the tree, so the report can show it without
// the viewer needing to know Scorecard's docs site. Checks whose
// documentation can't be loaded are silently omitted rather than failing the
// whole report.
func collectCheckDocs(root *gitlabtree.Node) map[string]checkDoc {
	names := make(map[string]struct{})
	collectCheckNames(root, names)

	docs, err := docChecks.Read()
	if err != nil {
		zap.L().Warn("Failed to load Scorecard check documentation", zap.Error(err))

		return nil
	}

	checkDocs := make(map[string]checkDoc, len(names))

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
