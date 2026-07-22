package gitlabtree

import (
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
