// Package config loads security-hub's configuration from a YAML file,
// with select fields overridable via environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultOutputPath = "./report"

// outputDirPermission matches internal/report's own directory permission, so
// the writability probe below creates the directory with the same mode the
// report renderer will later rely on.
const outputDirPermission = 0o755

var errMissingGitlabURL = errors.New("gitlab.url is required (set it in the config file or GITLAB_URL)")

// Gitlab holds the connection settings for the GitLab instance to scan.
type Gitlab struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
	// Filters optionally restricts discovery to projects whose full path
	// (namespace/project, e.g. "team/backend/service") matches at least
	// one of these regular expressions. Patterns are OR'd together: a
	// project is kept as soon as one pattern matches. Empty (the
	// default) keeps every project visible to the token.
	Filters []string `yaml:"filters"`
}

// Scorecard holds the settings controlling how OpenSSF Scorecard is run.
type Scorecard struct {
	Checks []string `yaml:"checks"`
	// MaxConcurrency bounds how many Scorecard analyses run at once. Empty
	// (zero) or negative means no limit.
	MaxConcurrency int  `yaml:"maxConcurrency"`
	Offline        bool `yaml:"offline"`
	// Experimental opts into Scorecard checks gated behind its
	// SCORECARD_EXPERIMENTAL env var. On GitLab this currently only
	// unlocks SBOM & Webhooks is also gated by the same var, but
	// Scorecard's own check registry doesn't declare GitLab support for
	// it, so it stays excluded regardless.
	Experimental bool `yaml:"experimental"`
}

// Output holds the settings controlling where the HTML report is written.
type Output struct {
	Path string `yaml:"path"`
}

// Config is the root configuration for security-hub.
type Config struct {
	Output Output `yaml:"output"`
	Gitlab Gitlab `yaml:"gitlab"`
	// CustomScores opts in to security-hub's own GitLab-native checks (see
	// internal/customchecks), each named after the check it reports under
	// (e.g. "Code-Quality", "Contributors"). Unlike scorecard.checks, an
	// empty list here means none of them run, not all of them: these checks
	// make extra GitLab API calls per project, so they stay opt-in rather
	// than opt-out.
	CustomScores []string  `yaml:"customScores"`
	Scorecard    Scorecard `yaml:"scorecard"`
}

// Load reads the YAML configuration file at path, applies environment
// variable overrides, and validates the result.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Output: Output{Path: defaultOutputPath},
	}

	data, err := os.ReadFile(path) //nolint:gosec // config path is an operator-supplied CLI flag
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	applyEnvOverrides(cfg)

	err = cfg.validate()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyEnvOverrides overlays environment variables on top of values loaded
// from the config file, matching the documented GITLAB_URL, GITLAB_TOKEN and
// SECURITY_HUB_OFFLINE variables.
func applyEnvOverrides(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("GITLAB_URL")); v != "" {
		cfg.Gitlab.URL = v
	}

	if v := strings.TrimSpace(os.Getenv("GITLAB_TOKEN")); v != "" {
		cfg.Gitlab.Token = v
	}

	if v := strings.TrimSpace(os.Getenv("SECURITY_HUB_OFFLINE")); v != "" {
		offline, err := strconv.ParseBool(v)
		if err == nil {
			cfg.Scorecard.Offline = offline
		}
	}

	if v := strings.TrimSpace(os.Getenv("SECURITY_HUB_EXPERIMENTAL")); v != "" {
		experimental, err := strconv.ParseBool(v)
		if err == nil {
			cfg.Scorecard.Experimental = experimental
		}
	}
}

func (cfg *Config) validate() error {
	if strings.TrimSpace(cfg.Gitlab.URL) == "" {
		return errMissingGitlabURL
	}

	for _, filter := range cfg.Gitlab.Filters {
		_, err := regexp.Compile(filter)
		if err != nil {
			return fmt.Errorf("gitlab.filters: invalid regular expression %q: %w", filter, err)
		}
	}

	if strings.TrimSpace(cfg.Output.Path) == "" {
		cfg.Output.Path = defaultOutputPath
	}

	err := checkOutputWritable(cfg.Output.Path)
	if err != nil {
		return err
	}

	return nil
}

// checkOutputWritable confirms outputDir can be created and written to,
// creating it if necessary. Scans take a long time, so this runs at
// configuration load rather than after the report is fully rendered, catching
// a bad output.path before any work is done instead of after it's lost.
func checkOutputWritable(outputDir string) error {
	err := os.MkdirAll(outputDir, outputDirPermission)
	if err != nil {
		return fmt.Errorf("output.path %q: %w", outputDir, err)
	}

	probe, err := os.CreateTemp(outputDir, ".security-hub-write-test-*")
	if err != nil {
		return fmt.Errorf("output.path %q is not writable: %w", outputDir, err)
	}

	probePath := probe.Name()

	err = probe.Close()
	if err != nil {
		return fmt.Errorf("output.path %q is not writable: %w", outputDir, err)
	}

	err = os.Remove(probePath)
	if err != nil {
		return fmt.Errorf("output.path %q: cleaning up write test file %q: %w", outputDir, probePath, err)
	}

	return nil
}
