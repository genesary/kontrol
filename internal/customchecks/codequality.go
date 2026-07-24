package customchecks

import (
	"context"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/genesary/security-hub/internal/gitlabtree"
)

const (
	CheckCodeQuality = "Code-Quality"

	codeQualityFileType = "codequality"
)

func CodeQuality(ctx context.Context, client *gitlab.Client, projectPath string) (*gitlabtree.ScoreStat, error) {
	return reportArtifactScore(ctx, client, projectPath, codeQualityFileType)
}
