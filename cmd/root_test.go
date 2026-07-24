package cmd

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestBuildRootCmdRegistersScanSubcommand(t *testing.T) {
	t.Parallel()

	root := buildRootCmd()

	scanCmd, _, err := root.Find([]string{"scan"})
	if err != nil {
		t.Fatalf("root.Find(\"scan\") error = %v", err)
	}

	if scanCmd.Use != "scan" {
		t.Fatalf("scanCmd.Use = %q, want %q", scanCmd.Use, "scan")
	}

	flag := root.PersistentFlags().Lookup("verbose")
	if flag == nil {
		t.Fatal("root command has no --verbose flag")
	}
}

func TestBuildScanCmdDefaultsConfigPath(t *testing.T) {
	t.Parallel()

	scanCmd := buildScanCmd()

	flag := scanCmd.Flags().Lookup("config")
	if flag == nil {
		t.Fatal("scan command has no --config flag")
	}

	if flag.DefValue != defaultConfigPath {
		t.Fatalf("--config default = %q, want %q", flag.DefValue, defaultConfigPath)
	}
}

func TestRunScanFailsOnMissingConfig(t *testing.T) {
	t.Parallel()

	err := runScan(context.Background(), "/nonexistent/path/to/config.yaml")
	if err == nil {
		t.Fatal("runScan() error = nil, want non-nil for a missing config file")
	}

	if !strings.Contains(err.Error(), "loading configuration") {
		t.Fatalf("runScan() error = %v, want it to mention %q", err, "loading configuration")
	}
}

func TestSetupLoggerSetsLevel(t *testing.T) {
	previous := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(previous) })

	t.Run("non-verbose stays at info level", func(t *testing.T) {
		setupLogger(false)

		if zap.L().Core().Enabled(zapcore.DebugLevel) {
			t.Fatal("logger has debug enabled, want info level when verbose=false")
		}

		if !zap.L().Core().Enabled(zapcore.InfoLevel) {
			t.Fatal("logger does not have info enabled, want it enabled when verbose=false")
		}
	})

	t.Run("verbose enables debug level", func(t *testing.T) {
		setupLogger(true)

		if !zap.L().Core().Enabled(zapcore.DebugLevel) {
			t.Fatal("logger does not have debug enabled, want it enabled when verbose=true")
		}
	})
}

func TestRootCmdRunsScanSubcommandEndToEnd(t *testing.T) {
	previous := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(previous) })

	root := buildRootCmd()
	root.SetArgs([]string{"scan", "--config", "/nonexistent/path/to/config.yaml"})
	root.SetContext(context.Background())
	root.SilenceUsage = true
	root.SilenceErrors = true

	err := root.Execute()
	if err == nil {
		t.Fatal("root.Execute() error = nil, want non-nil for a missing config file")
	}

	if !strings.Contains(err.Error(), "loading configuration") {
		t.Fatalf("root.Execute() error = %v, want it to mention %q", err, "loading configuration")
	}
}
