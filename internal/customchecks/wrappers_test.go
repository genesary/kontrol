package customchecks

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/boxboxjason/security-hub/internal/gitlabtree"
)

// TestReportArtifactWrappers checks that each thin wrapper around
// reportArtifactScore looks for its own artifact file type and not one of
// the others, so a project with e.g. only a codequality report artifact
// does not get credited for SAST.
func TestReportArtifactWrappers(t *testing.T) {
	t.Parallel()

	wrappers := map[string]struct {
		fileType string
		run      func(context.Context, *gitlab.Client, string) (*gitlabtree.ScoreStat, error)
	}{
		CheckCodeQuality:        {codeQualityFileType, CodeQuality},
		CheckSAST:               {sastFileType, SAST},
		CheckSecretDetection:    {secretDetectionFileType, SecretDetection},
		CheckDependencyScanning: {dependencyScanningFileType, DependencyScanning},
	}

	for name, wrapper := range wrappers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/pipelines"):
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `[{"id":1}]`)
				case strings.HasSuffix(r.URL.Path, "/pipelines/1/jobs"):
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `[{"id":10,"artifacts":[{"file_type":%q}]}]`, wrapper.fileType)
				default:
					http.NotFound(w, r)
				}
			})

			got, err := wrapper.run(context.Background(), client, "group/project")
			if err != nil {
				t.Fatalf("%s() error = %v", name, err)
			}

			const wantAverage = 10.0

			if got == nil || got.Average != wantAverage {
				t.Fatalf("%s() = %+v, want Average=%v (matching %q artifact)", name, got, wantAverage, wrapper.fileType)
			}
		})
	}
}
