package config

import "path/filepath"

type GitConfig struct {
	MergeStrategy string `toml:"merge_strategy"`
	WorktreeRoot  string `toml:"worktree_root"`
	BranchPrefix  string `toml:"branch_prefix"`
	BaseBranch    string `toml:"base_branch"`
}

func (g GitConfig) ResolvedWorktreeRoot(baseDir, projectName string) string {
	if g.WorktreeRoot != "" {
		return filepath.Join(g.WorktreeRoot, projectName)
	}
	return filepath.Join(baseDir, "worktrees", projectName)
}
