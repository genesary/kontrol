package gitlabtree

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

// TestBuildTreeInfersGroupsFromPaths asserts that groups and subgroups are
// derived purely from each project's namespaced path, with no group ever
// fetched from GitLab: "team/backend/service" must produce group "team"
// containing subgroup "team/backend" containing project "service", and a
// group shared by multiple projects (like "team" here) must be created
// exactly once rather than duplicated.
func TestBuildTreeInfersGroupsFromPaths(t *testing.T) {
	t.Parallel()

	projects := []*gitlab.Project{
		{Path: "service", PathWithNamespace: "team/backend/service", WebURL: "https://gitlab.example.com/team/backend/service"},
		{Path: "frontend", PathWithNamespace: "team/frontend", WebURL: "https://gitlab.example.com/team/frontend"},
		{Path: "standalone", PathWithNamespace: "standalone", WebURL: "https://gitlab.example.com/standalone"},
	}

	root := buildTree(projects)
	sortTree(root)

	if len(root.Children) != 2 {
		t.Fatalf("root.Children = %d, want 2 (group %q + project %q)", len(root.Children), "team", "standalone")
	}

	team := root.Children[0]
	if team.Kind != KindGroup || team.Name != "team" || team.FullPath != "team" {
		t.Fatalf("root.Children[0] = %+v, want inferred group %q", team, "team")
	}

	standalone := root.Children[1]
	if standalone.Kind != KindProject || standalone.FullPath != "standalone" {
		t.Fatalf("root.Children[1] = %+v, want top-level project %q", standalone, "standalone")
	}

	if len(team.Children) != 2 {
		t.Fatalf("team.Children = %d, want 2 (subgroup %q + project %q), got %+v", len(team.Children), "backend", "frontend", team.Children)
	}

	backend := team.Children[0]
	if backend.Kind != KindGroup || backend.FullPath != "team/backend" {
		t.Fatalf("team.Children[0] = %+v, want inferred subgroup %q", backend, "team/backend")
	}

	if len(backend.Children) != 1 || backend.Children[0].FullPath != "team/backend/service" {
		t.Fatalf("backend.Children = %+v, want single project %q", backend.Children, "team/backend/service")
	}

	frontend := team.Children[1]
	if frontend.Kind != KindProject || frontend.FullPath != "team/frontend" {
		t.Fatalf("team.Children[1] = %+v, want project %q", frontend, "team/frontend")
	}
}

// TestFilterProjectsORsPatterns asserts that filterProjects keeps a project
// as soon as any one of several filters matches its full path, drops
// projects matched by none, and leaves the input untouched when no filters
// are given.
func TestFilterProjectsORsPatterns(t *testing.T) {
	t.Parallel()

	projects := []*gitlab.Project{
		{PathWithNamespace: "team/backend/service"},
		{PathWithNamespace: "team/frontend"},
		{PathWithNamespace: "standalone"},
	}

	t.Run("no filters keeps everything", func(t *testing.T) {
		t.Parallel()

		got, err := filterProjects(projects, nil)
		if err != nil {
			t.Fatalf("filterProjects() error = %v, want nil", err)
		}

		if len(got) != len(projects) {
			t.Fatalf("filterProjects() = %d projects, want %d (unchanged)", len(got), len(projects))
		}
	})

	t.Run("matches are ORed across patterns", func(t *testing.T) {
		t.Parallel()

		got, err := filterProjects(projects, []string{"^team/backend/", "^standalone$"})
		if err != nil {
			t.Fatalf("filterProjects() error = %v, want nil", err)
		}

		if len(got) != 2 || got[0].PathWithNamespace != "team/backend/service" || got[1].PathWithNamespace != "standalone" {
			t.Fatalf("filterProjects() = %+v, want [team/backend/service, standalone]", got)
		}
	})

	t.Run("invalid pattern returns an error", func(t *testing.T) {
		t.Parallel()

		_, err := filterProjects(projects, []string{"("})
		if err == nil {
			t.Fatal("filterProjects() error = nil, want non-nil for invalid regex")
		}
	})
}

func TestSortTreeOrdersSameKindSiblingsByName(t *testing.T) {
	t.Parallel()

	projects := []*gitlab.Project{
		{Path: "zebra", PathWithNamespace: "zebra"},
		{Path: "alpha", PathWithNamespace: "alpha"},
		{Path: "app", PathWithNamespace: "team/app"},
		{Path: "beta", PathWithNamespace: "team/beta"},
	}

	root := buildTree(projects)
	sortTree(root)

	if len(root.Children) != 3 {
		t.Fatalf("root.Children = %d, want 3", len(root.Children))
	}

	// Groups sort before projects, and same-kind siblings sort by name:
	// "team" (group) first, then the two top-level projects alphabetically.
	if root.Children[0].Name != "team" {
		t.Fatalf("root.Children[0].Name = %q, want %q (group before projects)", root.Children[0].Name, "team")
	}

	if root.Children[1].Name != "alpha" || root.Children[2].Name != "zebra" {
		t.Fatalf("root.Children[1:] = [%q, %q], want same-kind siblings ordered [alpha, zebra]",
			root.Children[1].Name, root.Children[2].Name)
	}

	team := root.Children[0]
	if len(team.Children) != 2 || team.Children[0].Name != "app" || team.Children[1].Name != "beta" {
		t.Fatalf("team.Children = %+v, want [app, beta] ordered by name", team.Children)
	}
}

func TestDiscoverBuildsFilteredSortedTree(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/projects") {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("X-Next-Page", "2")
			fmt.Fprint(w, `[{"path":"service","path_with_namespace":"team/backend/service"}]`)
		default:
			fmt.Fprint(w, `[{"path":"excluded","path_with_namespace":"other/excluded"}]`)
		}
	}))
	t.Cleanup(server.Close)

	client, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL), gitlab.WithoutRetries())
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}

	root, err := Discover(context.Background(), client, []string{"^team/"})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(root.Children) != 1 || root.Children[0].Name != "team" {
		t.Fatalf("Discover() root.Children = %+v, want single filtered-in %q group", root.Children, "team")
	}
}

func TestDiscoverListProjectsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL), gitlab.WithoutRetries())
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}

	_, err = Discover(context.Background(), client, nil)
	if err == nil {
		t.Fatal("Discover() error = nil, want non-nil for API failure")
	}

	if !strings.Contains(err.Error(), "listing projects") {
		t.Fatalf("Discover() error = %v, want it to mention %q", err, "listing projects")
	}
}

func TestDiscoverInvalidFilterReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	t.Cleanup(server.Close)

	client, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL), gitlab.WithoutRetries())
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}

	_, err = Discover(context.Background(), client, []string{"("})
	if err == nil {
		t.Fatal("Discover() error = nil, want non-nil for invalid filter regex")
	}
}
