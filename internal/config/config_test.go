package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfigFile writes a minimal valid config to a temp file with the
// given output path, returning the config file's path.
func writeConfigFile(t *testing.T, outputPath string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")

	contents := "gitlab:\n  url: https://gitlab.example.com\noutput:\n  path: " + outputPath + "\n"

	err := os.WriteFile(configPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	return configPath
}

// TestLoadCreatesWritableOutputDir asserts that Load succeeds and creates
// output.path when it does not yet exist but is writable.
func TestLoadCreatesWritableOutputDir(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")

	_, err := Load(writeConfigFile(t, outputPath))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("output.path was not created: %v", err)
	}

	if !info.IsDir() {
		t.Fatalf("output.path = %q, want a directory", outputPath)
	}
}

// TestLoadFailsOnUnwritableOutputDir asserts that Load fails fast when
// output.path cannot be written to, rather than only failing later once the
// report is fully rendered and there is nothing left to save it to.
func TestLoadFailsOnUnwritableOutputDir(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	readOnlyParent := t.TempDir()

	err := os.Chmod(readOnlyParent, 0o500)
	if err != nil {
		t.Fatalf("chmod read-only parent: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(readOnlyParent, 0o700)
	})

	outputPath := filepath.Join(readOnlyParent, "report")

	_, err = Load(writeConfigFile(t, outputPath))
	if err == nil {
		t.Fatalf("Load() error = nil, want an error for unwritable output.path %q", outputPath)
	}
}

// TestLoadFailsWhenOutputPathIsAFile asserts that Load fails when
// output.path already exists as a regular file, since the report renderer
// needs it to be a directory.
func TestLoadFailsWhenOutputPathIsAFile(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "report")

	err := os.WriteFile(outputPath, []byte("not a directory"), 0o600)
	if err != nil {
		t.Fatalf("seeding output.path as a file: %v", err)
	}

	_, err = Load(writeConfigFile(t, outputPath))
	if err == nil {
		t.Fatalf("Load() error = nil, want an error when output.path %q is a file", outputPath)
	}
}

func TestLoadParsesWeights(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	outputPath := filepath.Join(t.TempDir(), "report")

	contents := "gitlab:\n  url: https://gitlab.example.com\n" +
		"output:\n  path: " + outputPath + "\n" +
		"weights:\n  Vulnerabilities: 3\n  Code-Quality: 0\n"

	err := os.WriteFile(configPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := map[string]int{"Vulnerabilities": 3, "Code-Quality": 0}
	if len(cfg.Weights) != len(want) || cfg.Weights["Vulnerabilities"] != 3 || cfg.Weights["Code-Quality"] != 0 {
		t.Fatalf("Weights = %v, want %v", cfg.Weights, want)
	}
}

func TestLoadRejectsNegativeWeight(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	outputPath := filepath.Join(t.TempDir(), "report")

	contents := "gitlab:\n  url: https://gitlab.example.com\n" +
		"output:\n  path: " + outputPath + "\n" +
		"weights:\n  Vulnerabilities: -1\n"

	err := os.WriteFile(configPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	_, err = Load(configPath)
	if err == nil {
		t.Fatalf("Load() error = nil, want an error for a negative weight")
	}
}
