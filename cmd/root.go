// Package cmd implements kontrol's command-line interface.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/genesary/kontrol/internal/config"
	"github.com/genesary/kontrol/internal/pipeline"
)

const defaultConfigPath = "config.yaml"

// Execute runs the CLI command and exits the process on failure.
func Execute() {
	err := buildRootCmd().Execute()
	if err != nil {
		zap.L().Error(err.Error())

		os.Exit(1)
	}
}

func buildRootCmd() *cobra.Command {
	var verbose bool

	rootCmd := &cobra.Command{
		Use:   "kontrol",
		Short: "Aggregate OpenSSF Scorecard results across a GitLab instance",
		Long:  "kontrol scans every project in a self-hosted GitLab instance with OpenSSF Scorecard and renders a single drill-down HTML report.",
		PersistentPreRun: func(*cobra.Command, []string) {
			setupLogger(verbose)
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose (debug) logging")

	rootCmd.AddCommand(buildScanCmd())

	return rootCmd
}

func buildScanCmd() *cobra.Command {
	var configPath string

	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Discover, scan and report on a GitLab instance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScan(cmd.Context(), configPath)
		},
	}

	scanCmd.Flags().StringVarP(&configPath, "config", "c", defaultConfigPath, "Path to the configuration file")

	return scanCmd
}

func runScan(ctx context.Context, configPath string) error {
	zap.L().Debug("Loading configuration", zap.String("path", configPath))

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	err = pipeline.Run(ctx, cfg)
	if err != nil {
		return fmt.Errorf("running scan: %w", err)
	}

	return nil
}

// setupLogger configures the global zap logger: info level by default, debug
// level when verbose is set. It also redirects output from dependencies
// that log through the standard library "log" package (such as Scorecard's
// GitLab tarball fetcher) into the same structured logger at debug level, so
// that noise is hidden unless verbose mode is on.
func setupLogger(verbose bool) {
	level := zapcore.InfoLevel
	if verbose {
		level = zapcore.DebugLevel
	}

	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logConfig.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	logConfig.Level = zap.NewAtomicLevelAt(level)

	logger, err := logConfig.Build()
	if err != nil {
		zap.L().Fatal("Failed to build logger", zap.Error(err))
	}

	zap.ReplaceGlobals(logger)

	_, err = zap.RedirectStdLogAt(logger, zapcore.DebugLevel)
	if err != nil {
		zap.L().Fatal("Failed to redirect standard library logging", zap.Error(err))
	}
}
