package pipeline

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/genesary/kontrol/internal/config"
	"github.com/genesary/kontrol/internal/gitlabtree"
	"github.com/genesary/kontrol/internal/scan"
)

func TestHostOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{name: "https with path", rawURL: "https://gitlab.example.com/", want: "gitlab.example.com"},
		{name: "with port", rawURL: "https://gitlab.example.com:8443", want: "gitlab.example.com:8443"},
		{name: "no scheme means no host", rawURL: "gitlab.example.com", wantErr: true},
		{name: "invalid url", rawURL: "://bad-url", wantErr: true},
		{name: "empty url", rawURL: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := hostOf(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("hostOf(%q) error = nil, want non-nil", tt.rawURL)
				}

				return
			}

			if err != nil {
				t.Fatalf("hostOf(%q) unexpected error: %v", tt.rawURL, err)
			}

			if got != tt.want {
				t.Fatalf("hostOf(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestOverallScore(t *testing.T) {
	t.Parallel()

	t.Run("nil score", func(t *testing.T) {
		t.Parallel()

		root := &gitlabtree.Node{Kind: gitlabtree.KindGroup}

		got := overallScore(root)
		if got != -1 {
			t.Fatalf("overallScore() = %v, want -1 when Score is nil", got)
		}
	})

	t.Run("populated score", func(t *testing.T) {
		t.Parallel()

		root := &gitlabtree.Node{
			Kind:  gitlabtree.KindGroup,
			Score: &gitlabtree.ScoreStat{Average: 7.5, Count: 3},
		}

		got := overallScore(root)
		if got != 7.5 {
			t.Fatalf("overallScore() = %v, want 7.5", got)
		}
	})
}

func TestLeafProjects(t *testing.T) {
	t.Parallel()

	t.Run("single project node is its own leaf", func(t *testing.T) {
		t.Parallel()

		project := &gitlabtree.Node{Kind: gitlabtree.KindProject, FullPath: "solo"}

		got := leafProjects(project)
		if len(got) != 1 || got[0] != project {
			t.Fatalf("leafProjects() = %+v, want [solo]", got)
		}
	})

	t.Run("nested groups collect only project leaves", func(t *testing.T) {
		t.Parallel()

		leafA := &gitlabtree.Node{Kind: gitlabtree.KindProject, FullPath: "team/a"}
		leafB := &gitlabtree.Node{Kind: gitlabtree.KindProject, FullPath: "team/sub/b"}
		sub := &gitlabtree.Node{Kind: gitlabtree.KindGroup, FullPath: "team/sub", Children: []*gitlabtree.Node{leafB}}
		root := &gitlabtree.Node{
			Kind:     gitlabtree.KindGroup,
			FullPath: "team",
			Children: []*gitlabtree.Node{leafA, sub},
		}

		got := leafProjects(root)
		if len(got) != 2 || got[0] != leafA || got[1] != leafB {
			t.Fatalf("leafProjects() = %+v, want [team/a, team/sub/b]", got)
		}
	})

	t.Run("empty group yields no projects", func(t *testing.T) {
		t.Parallel()

		root := &gitlabtree.Node{Kind: gitlabtree.KindGroup, FullPath: "empty"}

		got := leafProjects(root)
		if got != nil {
			t.Fatalf("leafProjects() = %+v, want nil", got)
		}
	})
}

func TestSetScorecardEnv(t *testing.T) {
	t.Run("sets auth token and skips experimental env by default", func(t *testing.T) {
		t.Setenv("GITLAB_AUTH_TOKEN", "")
		t.Setenv("SCORECARD_EXPERIMENTAL", "")

		cfg := &config.Config{Gitlab: config.Gitlab{Token: "secret-token"}}

		err := setScorecardEnv(cfg)
		if err != nil {
			t.Fatalf("setScorecardEnv() unexpected error: %v", err)
		}

		if got := os.Getenv("GITLAB_AUTH_TOKEN"); got != "secret-token" {
			t.Fatalf("GITLAB_AUTH_TOKEN = %q, want %q", got, "secret-token")
		}

		if got := os.Getenv("SCORECARD_EXPERIMENTAL"); got != "" {
			t.Fatalf("SCORECARD_EXPERIMENTAL = %q, want unset", got)
		}
	})

	t.Run("sets experimental env when enabled", func(t *testing.T) {
		t.Setenv("GITLAB_AUTH_TOKEN", "")
		t.Setenv("SCORECARD_EXPERIMENTAL", "")

		cfg := &config.Config{
			Gitlab:    config.Gitlab{Token: "secret-token"},
			Scorecard: config.Scorecard{Experimental: true},
		}

		err := setScorecardEnv(cfg)
		if err != nil {
			t.Fatalf("setScorecardEnv() unexpected error: %v", err)
		}

		if got := os.Getenv("SCORECARD_EXPERIMENTAL"); got != "1" {
			t.Fatalf("SCORECARD_EXPERIMENTAL = %q, want %q", got, "1")
		}
	})
}

func TestScanOneRecordsScanErrorOnUnreachableHost(t *testing.T) {
	t.Parallel()

	// A short deadline makes the underlying HTTP client give up on the
	// unreachable host almost immediately instead of exhausting its default
	// retry/backoff schedule. The host contains "gitlab." so
	// gitlabrepo.MakeGitlabRepo's IsValid() skips its own live network probe,
	// letting scorecard.Run be what fails.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	node := &gitlabtree.Node{Kind: gitlabtree.KindProject, FullPath: "group/project"}
	opts := scan.Options{Host: "gitlab.invalid.example"}

	scanOne(ctx, opts, node)

	if node.ScanError == "" {
		t.Fatal("node.ScanError = \"\", want non-empty when the gitlab host is unreachable")
	}

	if node.Score != nil {
		t.Fatalf("node.Score = %+v, want nil on scan failure", node.Score)
	}
}

func TestScanProjectsRecordsScanErrorsWithoutFailingTheGroup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	projects := []*gitlabtree.Node{
		{Kind: gitlabtree.KindProject, FullPath: "group/one"},
		{Kind: gitlabtree.KindProject, FullPath: "group/two"},
	}
	opts := scan.Options{Host: "gitlab.invalid.example"}

	err := scanProjects(ctx, opts, projects, 1)
	if err != nil {
		t.Fatalf("scanProjects() error = %v, want nil (per-project failures are recorded, not returned)", err)
	}

	for _, project := range projects {
		if project.ScanError == "" {
			t.Fatalf("project %q ScanError = \"\", want non-empty when the gitlab host is unreachable", project.FullPath)
		}
	}
}
