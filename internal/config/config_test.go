package config

import (
	"errors"
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

func TestLoadFailsWhenFileMissing(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load() error = nil, want an error for a missing config file")
	}
}

func TestLoadFailsOnInvalidYAML(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")

	err := os.WriteFile(configPath, []byte("gitlab: [this is not a map]"), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	_, err = Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for malformed YAML")
	}
}

func TestLoadFailsWhenGitlabURLMissing(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	outputPath := filepath.Join(t.TempDir(), "report")

	contents := "output:\n  path: " + outputPath + "\n"

	err := os.WriteFile(configPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	_, err = Load(configPath)
	if !errors.Is(err, errMissingGitlabURL) {
		t.Fatalf("Load() error = %v, want %v", err, errMissingGitlabURL)
	}
}

func TestLoadFailsOnInvalidFilterRegex(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	outputPath := filepath.Join(t.TempDir(), "report")

	contents := "gitlab:\n  url: https://gitlab.example.com\n  filters:\n    - \"(\"\n" +
		"output:\n  path: " + outputPath + "\n"

	err := os.WriteFile(configPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	_, err = Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want an error for an invalid gitlab.filters regex")
	}
}

func TestLoadDefaultsEmptyOutputPath(t *testing.T) {
	// Not t.Parallel(): changes the process working directory, since
	// defaultOutputPath ("./report") is created relative to it.
	t.Chdir(t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.yaml")

	contents := "gitlab:\n  url: https://gitlab.example.com\noutput:\n  path: \"\"\n"

	err := os.WriteFile(configPath, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Output.Path != defaultOutputPath {
		t.Fatalf("Output.Path = %q, want default %q", cfg.Output.Path, defaultOutputPath)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Run("GITLAB_URL overrides the config file", func(t *testing.T) {
		t.Setenv("GITLAB_URL", "https://env.example.com")

		cfg := &Config{Gitlab: Gitlab{URL: "https://file.example.com"}}
		applyEnvOverrides(cfg)

		if cfg.Gitlab.URL != "https://env.example.com" {
			t.Fatalf("Gitlab.URL = %q, want env override", cfg.Gitlab.URL)
		}
	})

	t.Run("GITLAB_TOKEN overrides the config file", func(t *testing.T) {
		t.Setenv("GITLAB_TOKEN", "env-token")

		cfg := &Config{Gitlab: Gitlab{Token: "file-token"}}
		applyEnvOverrides(cfg)

		if cfg.Gitlab.Token != "env-token" {
			t.Fatalf("Gitlab.Token = %q, want env override", cfg.Gitlab.Token)
		}
	})

	t.Run("KONTROL_OFFLINE overrides when a valid bool", func(t *testing.T) {
		t.Setenv("KONTROL_OFFLINE", "true")

		cfg := &Config{}
		applyEnvOverrides(cfg)

		if !cfg.Scorecard.Offline {
			t.Fatal("Scorecard.Offline = false, want true from env override")
		}
	})

	t.Run("KONTROL_OFFLINE is ignored when not a valid bool", func(t *testing.T) {
		t.Setenv("KONTROL_OFFLINE", "not-a-bool")

		cfg := &Config{Scorecard: Scorecard{Offline: true}}
		applyEnvOverrides(cfg)

		if !cfg.Scorecard.Offline {
			t.Fatal("Scorecard.Offline was overwritten by an invalid env value, want unchanged")
		}
	})

	t.Run("KONTROL_EXPERIMENTAL overrides when a valid bool", func(t *testing.T) {
		t.Setenv("KONTROL_EXPERIMENTAL", "true")

		cfg := &Config{}
		applyEnvOverrides(cfg)

		if !cfg.Scorecard.Experimental {
			t.Fatal("Scorecard.Experimental = false, want true from env override")
		}
	})

	t.Run("KONTROL_EXPERIMENTAL is ignored when not a valid bool", func(t *testing.T) {
		t.Setenv("KONTROL_EXPERIMENTAL", "not-a-bool")

		cfg := &Config{Scorecard: Scorecard{Experimental: true}}
		applyEnvOverrides(cfg)

		if !cfg.Scorecard.Experimental {
			t.Fatal("Scorecard.Experimental was overwritten by an invalid env value, want unchanged")
		}
	})
}
