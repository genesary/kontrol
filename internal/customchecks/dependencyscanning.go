package customchecks

import (
	"context"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

const (
	CheckDependencyScanning = "Dependency-Scanning"

	dependencyScanningFileType = "dependency_scanning"
)

func DependencyScanning(ctx context.Context, client *gitlab.Client, projectPath string) (*gitlabtree.ScoreStat, error) {
	return reportArtifactScore(ctx, client, projectPath, dependencyScanningFileType)
}
