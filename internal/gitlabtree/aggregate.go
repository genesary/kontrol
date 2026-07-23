package gitlabtree

// Aggregate walks the tree bottom-up, rolling up each group's ProjectCount,
// Score and Checks from its children. Project (leaf) nodes are assumed to
// already carry the Score/Checks set by the scan step and are left as-is.
func Aggregate(node *Node) {
	if node.Kind == KindProject {
		if node.ProjectCount == 0 {
			node.ProjectCount = 1
		}

		return
	}

	var projectCount int

	scores := make([]*ScoreStat, 0, len(node.Children))
	checkScores := make(map[string][]*ScoreStat)

	for _, child := range node.Children {
		Aggregate(child)

		projectCount += child.ProjectCount
		scores = append(scores, child.Score)

		for name, stat := range child.Checks {
			checkScores[name] = append(checkScores[name], stat)
		}
	}

	node.ProjectCount = projectCount
	node.Score = Combine(scores)
	node.Checks = combineChecks(checkScores)
}

// combineChecks rolls up each check by name across children. A check keeps
// its key even when every child was inconclusive for it (Combine returns nil
// in that case), so it still renders as "N/A" instead of silently
// disappearing from the report at this level of the tree.
func combineChecks(checkScores map[string][]*ScoreStat) map[string]*ScoreStat {
	if len(checkScores) == 0 {
		return nil
	}

	combined := make(map[string]*ScoreStat, len(checkScores))

	for name, stats := range checkScores {
		combined[name] = Combine(stats)
	}

	return combined
}
