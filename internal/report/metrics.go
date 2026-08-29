package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/genesary/kontrol/internal/gitlabtree"
)

// metricsFileName is the Prometheus text exposition file written next to
// index.html. It is deliberately extensionless: it is meant to be served as
// the body of a /metrics endpoint (or dropped into a textfile collector
// directory), not opened as a document.
const metricsFileName = "metrics"

// instanceKind is the value of the "kind" label on the synthetic root node,
// distinguishing the whole-instance rollup from the real GitLab groups
// below it (both of which are gitlabtree.KindGroup).
const instanceKind = "instance"

// Metric names exposed in the metrics file. They share the kontrol_ prefix
// so a scraper can select the whole report with a single {__name__=~"kontrol_.*"}.
const (
	metricInfo        = "kontrol_info"
	metricGeneratedAt = "kontrol_report_generated_at_seconds"
	metricScore       = "kontrol_score"
	metricCheckScore  = "kontrol_check_score"
	metricProjects    = "kontrol_projects"
	metricScanFailed  = "kontrol_scan_failed"
)

// newLabelEscaper builds the replacer escaping the three characters
// Prometheus' text format does not allow raw inside a label value.
func newLabelEscaper() *strings.Replacer {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
}

// RenderMetrics writes the aggregated tree to outputDir/metrics in the
// Prometheus text exposition format, creating outputDir if it does not
// already exist. Every level of the tree is exposed (instance root, groups
// and projects alike), each distinguished by its "kind" label, so a
// dashboard can read the report's own project-count-weighted rollups
// directly instead of re-deriving unweighted ones in PromQL.
//
// Inconclusive scores are omitted rather than exported as zero: a check that
// could not run (offline mode, an unsupported repository) is genuinely
// absent, and a zero would be indistinguishable from a real score of 0.
func RenderMetrics(root *gitlabtree.Node, outputDir string, generatedAt time.Time, gitlabURL string) error {
	zap.L().Debug("Rendering metrics", zap.String("outputDir", outputDir))

	err := writeArtifact(outputDir, metricsFileName, []byte(buildMetrics(root, generatedAt, gitlabURL)))
	if err != nil {
		return fmt.Errorf("writing metrics file: %w", err)
	}

	return nil
}

// buildMetrics renders the whole exposition body. Prometheus wants all
// samples of a metric grouped under a single HELP/TYPE pair, so the tree is
// walked once per metric family rather than once overall.
func buildMetrics(root *gitlabtree.Node, generatedAt time.Time, gitlabURL string) string {
	nodes := flatten(root, nil)
	out := &exposition{escaper: newLabelEscaper()}

	out.family(metricInfo, "Metadata about the kontrol run that produced this file.")
	out.sample(metricInfo, []label{{"gitlab_url", gitlabURL}}, 1)

	out.family(metricGeneratedAt, "Unix timestamp of the scan that produced this file.")
	out.sample(metricGeneratedAt, nil, float64(generatedAt.Unix()))

	out.family(metricScore,
		"Overall OpenSSF Scorecard score (0-10), aggregated project-count-weighted at group and instance level.")

	for _, node := range nodes {
		if node.Score == nil {
			continue
		}

		out.sample(metricScore, nodeLabels(node), node.Score.Average)
	}

	out.family(metricCheckScore,
		"Per-check score (0-10), aggregated project-count-weighted at group and instance level.")

	for _, node := range nodes {
		for _, name := range sortedCheckNames(node.Checks) {
			stat := node.Checks[name]
			if stat == nil {
				continue
			}

			out.sample(metricCheckScore, append(nodeLabels(node), label{"check", name}), stat.Average)
		}
	}

	out.family(metricProjects, "Number of projects contributing to this node's scores.")

	for _, node := range nodes {
		out.sample(metricProjects, nodeLabels(node), float64(node.ProjectCount))
	}

	out.family(metricScanFailed, "1 when the project could not be analyzed, 0 otherwise.")

	for _, node := range nodes {
		if node.Kind != gitlabtree.KindProject {
			continue
		}

		failed := 0.0
		if node.ScanError != "" {
			failed = 1
		}

		out.sample(metricScanFailed, nodeLabels(node), failed)
	}

	return out.builder.String()
}

// exposition accumulates the metrics file, carrying the label escaper
// alongside the buffer so it is built once per render rather than once per
// label value.
type exposition struct {
	escaper *strings.Replacer
	builder strings.Builder
}

// label is a single Prometheus label name/value pair.
type label struct {
	name  string
	value string
}

// nodeLabels returns the labels identifying a node across every metric
// family. The instance root has an empty path, which is what makes it
// selectable on its own: kontrol_score{kind="instance"}.
func nodeLabels(node *gitlabtree.Node) []label {
	return []label{
		{"kind", kindOf(node)},
		{"path", node.FullPath},
	}
}

// kindOf maps a node to its "kind" label value, promoting the synthetic
// root out of the plain group kind it shares with real GitLab groups.
func kindOf(node *gitlabtree.Node) string {
	if node.Kind == gitlabtree.KindGroup && node.FullPath == "" {
		return instanceKind
	}

	return string(node.Kind)
}

// flatten collects the tree into a depth-first slice, preserving the
// alphabetical child ordering discovery already applied so the file is
// byte-stable between runs over unchanged data.
func flatten(node *gitlabtree.Node, into []*gitlabtree.Node) []*gitlabtree.Node {
	into = append(into, node)

	for _, child := range node.Children {
		into = flatten(child, into)
	}

	return into
}

func sortedCheckNames(checks map[string]*gitlabtree.ScoreStat) []string {
	names := make([]string, 0, len(checks))

	for name := range checks {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// family opens a new metric family, blank-line separated from the previous
// one. Every metric kontrol exposes is a gauge: they are all point-in-time
// readings of the last scan, never monotonic counters.
func (out *exposition) family(name string, help string) {
	if out.builder.Len() > 0 {
		out.builder.WriteString("\n")
	}

	fmt.Fprintf(&out.builder, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
}

func (out *exposition) sample(name string, labels []label, value float64) {
	out.builder.WriteString(name)

	if len(labels) > 0 {
		out.builder.WriteString("{")

		for index, item := range labels {
			if index > 0 {
				out.builder.WriteString(",")
			}

			out.builder.WriteString(item.name)
			out.builder.WriteString(`="`)
			out.builder.WriteString(out.escaper.Replace(item.value))
			out.builder.WriteString(`"`)
		}

		out.builder.WriteString("}")
	}

	out.builder.WriteString(" ")
	// 'f' with -1 precision round-trips the float in its shortest form
	// without the trailing zeroes a fixed precision would add, and without
	// the exponent notation 'g' would use for a Unix timestamp.
	out.builder.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
	out.builder.WriteString("\n")
}
