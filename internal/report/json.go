package report

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/genesary/kontrol/internal/gitlabtree"
)

// jsonFileName is the machine-readable dump written next to index.html.
const jsonFileName = "results.json"

// jsonSchemaVersion identifies the shape of results.json, so a consumer can
// detect an incompatible kontrol version instead of silently misreading a
// changed layout. Bump it whenever jsonDocument's fields change meaning.
const jsonSchemaVersion = 1

// jsonDocument is the top level of results.json: the same tree the HTML
// report embeds, wrapped in enough run metadata for a downstream consumer
// to know which instance the numbers describe and when they were taken.
type jsonDocument struct {
	Root          *gitlabtree.Node `json:"root"`
	GeneratedAt   string           `json:"generatedAt"`
	GitlabURL     string           `json:"gitlabUrl"`
	SchemaVersion int              `json:"schemaVersion"`
}

// RenderJSON writes the aggregated tree to outputDir/results.json, creating
// outputDir if it does not already exist. gitlabURL is the scanned GitLab
// instance's base URL, recorded alongside the data so the file is
// self-describing once moved away from the run that produced it.
func RenderJSON(root *gitlabtree.Node, outputDir string, generatedAt time.Time, gitlabURL string) error {
	zap.L().Debug("Rendering JSON results", zap.String("outputDir", outputDir))

	document := jsonDocument{
		SchemaVersion: jsonSchemaVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		GitlabURL:     gitlabURL,
		Root:          root,
	}

	// Indented, because results.json is as likely to be read by a person
	// grepping through it as by a program.
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON results: %w", err)
	}

	encoded = append(encoded, '\n')

	err = writeArtifact(outputDir, jsonFileName, encoded)
	if err != nil {
		return fmt.Errorf("writing JSON results: %w", err)
	}

	return nil
}
