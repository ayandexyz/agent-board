package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetDefaultsGit(t *testing.T) {
	cfg := SetDefaults()

	if cfg.Git.MergeStrategy != "local" {
		t.Errorf("Git.MergeStrategy = %q, want %q", cfg.Git.MergeStrategy, "local")
	}
	if cfg.Git.BranchPrefix != "agent/" {
		t.Errorf("Git.BranchPrefix = %q, want %q", cfg.Git.BranchPrefix, "agent/")
	}
	if cfg.Git.WorktreeRoot != "" {
		t.Errorf("Git.WorktreeRoot = %q, want empty", cfg.Git.WorktreeRoot)
	}
	if cfg.Git.BaseBranch != "" {
		t.Errorf("Git.BaseBranch = %q, want empty", cfg.Git.BaseBranch)
	}
}

func TestLoadFromFileGit(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := []byte(`
[git]
merge_strategy = "pr"
worktree_root = "/data/worktrees"
branch_prefix = "ticket/"
base_branch = "develop"
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := SetDefaults()
	if err := loadFromFile(cfg, cfgPath); err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}

	if cfg.Git.MergeStrategy != "pr" {
		t.Errorf("Git.MergeStrategy = %q, want %q", cfg.Git.MergeStrategy, "pr")
	}
	if cfg.Git.WorktreeRoot != "/data/worktrees" {
		t.Errorf("Git.WorktreeRoot = %q, want %q", cfg.Git.WorktreeRoot, "/data/worktrees")
	}
	if cfg.Git.BranchPrefix != "ticket/" {
		t.Errorf("Git.BranchPrefix = %q, want %q", cfg.Git.BranchPrefix, "ticket/")
	}
	if cfg.Git.BaseBranch != "develop" {
		t.Errorf("Git.BaseBranch = %q, want %q", cfg.Git.BaseBranch, "develop")
	}
}

func TestLoadFromFileGitPartial(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := []byte(`
[git]
merge_strategy = "pr"
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := SetDefaults()
	if err := loadFromFile(cfg, cfgPath); err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}

	if cfg.Git.MergeStrategy != "pr" {
		t.Errorf("Git.MergeStrategy = %q, want %q", cfg.Git.MergeStrategy, "pr")
	}
	if cfg.Git.BranchPrefix != "agent/" {
		t.Errorf("Git.BranchPrefix should retain default %q, got %q", "agent/", cfg.Git.BranchPrefix)
	}
}

func TestApplyEnvVarsGit(t *testing.T) {
	t.Setenv("AGENTBOARD_GIT_MERGE_STRATEGY", "pr")
	t.Setenv("AGENTBOARD_GIT_WORKTREE_ROOT", "/env/worktrees")
	t.Setenv("AGENTBOARD_GIT_BRANCH_PREFIX", "bot/")
	t.Setenv("AGENTBOARD_GIT_BASE_BRANCH", "trunk")

	cfg := SetDefaults()
	applyEnvVars(cfg)

	if cfg.Git.MergeStrategy != "pr" {
		t.Errorf("Git.MergeStrategy = %q, want %q", cfg.Git.MergeStrategy, "pr")
	}
	if cfg.Git.WorktreeRoot != "/env/worktrees" {
		t.Errorf("Git.WorktreeRoot = %q, want %q", cfg.Git.WorktreeRoot, "/env/worktrees")
	}
	if cfg.Git.BranchPrefix != "bot/" {
		t.Errorf("Git.BranchPrefix = %q, want %q", cfg.Git.BranchPrefix, "bot/")
	}
	if cfg.Git.BaseBranch != "trunk" {
		t.Errorf("Git.BaseBranch = %q, want %q", cfg.Git.BaseBranch, "trunk")
	}
}

func TestResolvedWorktreeRoot(t *testing.T) {
	tests := []struct {
		name    string
		cfg     GitConfig
		baseDir string
		project string
		want    string
	}{
		{
			name:    "default derives from base dir and project",
			cfg:     GitConfig{},
			baseDir: "/home/u/.agentboard",
			project: "my-app",
			want:    filepath.Join("/home/u/.agentboard", "worktrees", "my-app"),
		},
		{
			name:    "explicit root is scoped by project",
			cfg:     GitConfig{WorktreeRoot: "/data/wt"},
			baseDir: "/home/u/.agentboard",
			project: "my-app",
			want:    filepath.Join("/data/wt", "my-app"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.ResolvedWorktreeRoot(tt.baseDir, tt.project)
			if got != tt.want {
				t.Errorf("ResolvedWorktreeRoot = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadFromFileGitSetup(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := []byte(`
[git]
setup_script = "pnpm install"
copy_files = [".env", "config/*.local.json"]
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := SetDefaults()
	if err := loadFromFile(cfg, cfgPath); err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}

	if cfg.Git.SetupScript != "pnpm install" {
		t.Errorf("Git.SetupScript = %q, want %q", cfg.Git.SetupScript, "pnpm install")
	}
	want := []string{".env", "config/*.local.json"}
	if len(cfg.Git.CopyFiles) != len(want) || cfg.Git.CopyFiles[0] != want[0] || cfg.Git.CopyFiles[1] != want[1] {
		t.Errorf("Git.CopyFiles = %v, want %v", cfg.Git.CopyFiles, want)
	}
}
