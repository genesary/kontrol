package gitlabtree

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
	"go.uber.org/zap"
)

const listPageSize = 100

// instanceRootName is the display name of the synthetic root node
// representing the whole GitLab instance.
const instanceRootName = "GitLab instance"

// Discover fetches every project visible to client, optionally keeps only
// those whose full path matches at least one of filters, and builds the
// group/subgroup/project tree by splitting each remaining project's
// namespaced path. Groups and subgroups are never fetched from GitLab:
// they are inferred purely from the path segments of the projects
// themselves. The returned tree has no scores attached yet.
func Discover(ctx context.Context, client *gitlab.Client, filters []string) (*Node, error) {
	zap.L().Debug("Discovering GitLab projects")

	projects, err := listAllProjects(ctx, client)
	if err != nil {
		return nil, err
	}

	zap.L().Info("Fetched projects from GitLab", zap.Int("projects", len(projects)))

	projects, err = filterProjects(projects, filters)
	if err != nil {
		return nil, err
	}

	if len(filters) > 0 {
		zap.L().Info("Filtered projects by gitlab.filters", zap.Int("projects", len(projects)))
	}

	root := buildTree(projects)

	sortTree(root)

	return root, nil
}

// filterProjects keeps only the projects whose full path (namespace/project)
// matches at least one of the given regular expressions. filters are OR'd
// together: each project is tested against them in order and kept as soon
// as one matches. An empty filters list keeps every project unchanged.
func filterProjects(projects []*gitlab.Project, filters []string) ([]*gitlab.Project, error) {
	if len(filters) == 0 {
		return projects, nil
	}

	patterns := make([]*regexp.Regexp, len(filters))

	for index, filter := range filters {
		pattern, err := regexp.Compile(filter)
		if err != nil {
			return nil, fmt.Errorf("compiling gitlab filter %q: %w", filter, err)
		}

		patterns[index] = pattern
	}

	filtered := make([]*gitlab.Project, 0, len(projects))

	for _, project := range projects {
		for _, pattern := range patterns {
			if pattern.MatchString(project.PathWithNamespace) {
				filtered = append(filtered, project)

				break
			}
		}
	}

	return filtered, nil
}

func listAllProjects(ctx context.Context, client *gitlab.Client) ([]*gitlab.Project, error) {
	var allProjects []*gitlab.Project

	opts := &gitlab.ListProjectsOptions{
		ListOptions: gitlab.ListOptions{PerPage: listPageSize, Page: 1},
	}

	for {
		zap.L().Debug("Listing projects page", zap.Int64("page", opts.Page))

		projects, resp, err := client.Projects.ListProjects(opts, gitlab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("listing projects: %w", err)
		}

		allProjects = append(allProjects, projects...)

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return allProjects, nil
}

// buildTree infers the group/subgroup hierarchy from each project's
// namespaced path (e.g. "team/backend/service" implies groups "team" and
// "team/backend") and attaches every project as a leaf under its inferred
// parent group.
func buildTree(projects []*gitlab.Project) *Node {
	root := &Node{Kind: KindGroup, Name: instanceRootName}
	groupsByPath := map[string]*Node{"": root}

	for _, project := range projects {
		segments := strings.Split(project.PathWithNamespace, "/")
		parent := ensureGroup(groupsByPath, root, segments[:len(segments)-1])

		parent.Children = append(parent.Children, &Node{
			Kind:          KindProject,
			Name:          project.Path,
			FullPath:      project.PathWithNamespace,
			WebURL:        project.WebURL,
			DefaultBranch: project.DefaultBranch,
			ProjectCount:  1,
		})
	}

	return root
}

// ensureGroup walks segments from root, creating any missing intermediate
// group nodes along the way, and returns the node for the final segment.
func ensureGroup(groupsByPath map[string]*Node, root *Node, segments []string) *Node {
	parent := root
	path := ""

	for _, segment := range segments {
		if path == "" {
			path = segment
		} else {
			path += "/" + segment
		}

		group, ok := groupsByPath[path]
		if !ok {
			group = &Node{Kind: KindGroup, Name: segment, FullPath: path}
			groupsByPath[path] = group
			parent.Children = append(parent.Children, group)
		}

		parent = group
	}

	return parent
}

// sortTree orders each group's children alphabetically (groups before
// projects, then by name) so the report renders in a stable, readable order.
func sortTree(node *Node) {
	sort.Slice(node.Children, func(i, j int) bool {
		left, right := node.Children[i], node.Children[j]
		if left.Kind != right.Kind {
			return left.Kind == KindGroup
		}

		return left.Name < right.Name
	})

	for _, child := range node.Children {
		if child.Kind == KindGroup {
			sortTree(child)
		}
	}
}
