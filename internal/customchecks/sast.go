package customchecks

import (
	"context"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

const (
	CheckSAST = "SAST"

	sastFileType = "sast"
)

func SAST(ctx context.Context, client *gitlab.Client, projectPath string) (*gitlabtree.ScoreStat, error) {
	return reportArtifactScore(ctx, client, projectPath, sastFileType)
}
