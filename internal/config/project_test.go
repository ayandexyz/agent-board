package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGit struct {
	responses map[string]string
	calls     []string
}

func (f *fakeGit) run(dir string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if out, ok := f.responses[key]; ok {
		return out, nil
	}
	return "", errors.New("exit status 1")
}

func (f *fakeGit) called(key string) bool {
	for _, c := range f.calls {
		if c == key {
			return true
		}
	}
	return false
}

const (
	commonDirArgs = "rev-parse --path-format=absolute --git-common-dir"
	remoteArgs    = "remote get-url origin"
	pinGetArgs    = "config --local --get agentboard.project"
)

func TestRemoteKey(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"https://github.com/ayandexyz/agent-board.git", "ayandexyz-agent-board"},
		{"git@github.com:ayandexyz/agent-board.git", "ayandexyz-agent-board"},
		{"https://github.com/User/My-Project", "user-my-project"},
		{"https://github.com/user/repo/", "user-repo"},
		{"ssh://git@gitlab.com:2222/group/sub/project.git", "sub-project"},
		{"git@gitlab.com:group/subgroup/project.git", "subgroup-project"},
		{"/srv/git/api.git", "git-api"},
		{"api.git", "api"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.remote, func(t *testing.T) {
			if got := remoteKey(tt.remote); got != tt.want {
				t.Errorf("remoteKey(%q) = %q, want %q", tt.remote, got, tt.want)
			}
		})
	}
}

func TestRemoteKeySeparatesSameRepoNameAcrossOwners(t *testing.T) {
	a := remoteKey("git@github.com:alice/api.git")
	b := remoteKey("git@github.com:bob/api.git")
	if a == b {
		t.Fatalf("alice/api and bob/api share key %q", a)
	}
}

func TestPathKeyIsReadableAndDistinct(t *testing.T) {
	a := pathKey("/home/me/work/api")
	b := pathKey("/home/me/oss/api")
	if a == b {
		t.Fatalf("different paths share key %q", a)
	}
	if !strings.HasPrefix(a, "api-") || len(a) != len("api-")+8 {
		t.Errorf("pathKey = %q, want api-<8 hex>", a)
	}
	if pathKey("/home/me/work/api") != a {
		t.Error("pathKey is not deterministic")
	}
}

func TestResolveProjectUsesRemoteAndPinsIt(t *testing.T) {
	g := &fakeGit{responses: map[string]string{
		commonDirArgs: "/home/me/code/agent-board/.git",
		remoteArgs:    "git@github.com:ayandexyz/agent-board.git",
		"config --local agentboard.project ayandexyz-agent-board": "",
	}}

	p := ResolveProject(g.run, "/home/me/code/agent-board/internal")

	if p.Key != "ayandexyz-agent-board" {
		t.Errorf("Key = %q, want ayandexyz-agent-board", p.Key)
	}
	if p.Name != "agent-board" {
		t.Errorf("Name = %q, want agent-board", p.Name)
	}
	if p.LegacyKey != "agent-board" {
		t.Errorf("LegacyKey = %q, want agent-board", p.LegacyKey)
	}
	if !g.called("config --local agentboard.project ayandexyz-agent-board") {
		t.Errorf("key was not pinned; calls: %v", g.calls)
	}
}

func TestResolveProjectPrefersPinnedKey(t *testing.T) {
	g := &fakeGit{responses: map[string]string{
		commonDirArgs: "/moved/somewhere/agent-board/.git",
		remoteArgs:    "git@github.com:renamed-owner/agent-board.git",
		pinGetArgs:    "ayandexyz-agent-board\n",
	}}

	p := ResolveProject(g.run, "/moved/somewhere/agent-board")

	if p.Key != "ayandexyz-agent-board" {
		t.Errorf("Key = %q, want pinned ayandexyz-agent-board", p.Key)
	}
	for _, c := range g.calls {
		if strings.HasPrefix(c, "config --local agentboard.project ") {
			t.Errorf("pinned key was rewritten: %q", c)
		}
	}
}

func TestResolveProjectSlugifiesPinnedKey(t *testing.T) {
	g := &fakeGit{responses: map[string]string{
		commonDirArgs: "/r/.git",
		pinGetArgs:    "My Shared Board",
	}}

	if p := ResolveProject(g.run, "/r"); p.Key != "my-shared-board" {
		t.Errorf("Key = %q, want my-shared-board", p.Key)
	}
}

func TestResolveProjectWithoutRemoteUsesMainRepoRoot(t *testing.T) {
	root := "/home/me/scratch/notes"
	g := &fakeGit{responses: map[string]string{
		commonDirArgs: root + "/.git",
	}}

	fromRoot := ResolveProject(g.run, root)
	fromWorktree := ResolveProject(g.run, "/home/me/.agentboard/worktrees/x/AGT-03")

	if fromRoot.Key != pathKey(root) {
		t.Errorf("Key = %q, want %q", fromRoot.Key, pathKey(root))
	}
	if fromWorktree.Key != fromRoot.Key {
		t.Errorf("worktree key %q differs from root key %q", fromWorktree.Key, fromRoot.Key)
	}
	if fromRoot.Name != "notes" {
		t.Errorf("Name = %q, want notes", fromRoot.Name)
	}
	if !g.called("config --local agentboard.project " + fromRoot.Key) {
		t.Errorf("key was not pinned; calls: %v", g.calls)
	}
}

func TestResolveProjectBareRepoUsesCommonDir(t *testing.T) {
	g := &fakeGit{responses: map[string]string{
		commonDirArgs: "/srv/repos/site.git",
	}}

	p := ResolveProject(g.run, "/srv/repos/site.git")

	if p.Key != pathKey("/srv/repos/site.git") {
		t.Errorf("Key = %q, want %q", p.Key, pathKey("/srv/repos/site.git"))
	}
}

func TestResolveProjectOutsideGit(t *testing.T) {
	g := &fakeGit{responses: map[string]string{}}

	p := ResolveProject(g.run, "/home/me/My Folder")

	if p.Key != pathKey("/home/me/My Folder") {
		t.Errorf("Key = %q, want %q", p.Key, pathKey("/home/me/My Folder"))
	}
	if p.Name != "my-folder" || p.LegacyKey != "my-folder" {
		t.Errorf("Name/LegacyKey = %q/%q, want my-folder", p.Name, p.LegacyKey)
	}
	for _, c := range g.calls {
		if strings.HasPrefix(c, "config") {
			t.Errorf("config touched outside a repo: %q", c)
		}
	}
}

func TestMigrateLegacyProjectRenamesOldDir(t *testing.T) {
	base := t.TempDir()
	old := filepath.Join(base, "projects", "agent-board")
	if err := os.MkdirAll(old, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "board.db"), []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}

	p := Project{Key: "ayandexyz-agent-board", Name: "agent-board", LegacyKey: "agent-board"}
	if err := MigrateLegacyProject(base, p); err != nil {
		t.Fatalf("MigrateLegacyProject: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(base, "projects", p.Key, "board.db"))
	if err != nil || string(got) != "db" {
		t.Fatalf("board.db not moved: %v %q", err, got)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("legacy dir still present: %v", err)
	}
}

func TestMigrateLegacyProjectNeverOverwrites(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"agent-board", "ayandexyz-agent-board"} {
		dir := filepath.Join(base, "projects", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "board.db"), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	p := Project{Key: "ayandexyz-agent-board", LegacyKey: "agent-board"}
	if err := MigrateLegacyProject(base, p); err != nil {
		t.Fatalf("MigrateLegacyProject: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(base, "projects", p.Key, "board.db"))
	if string(got) != "ayandexyz-agent-board" {
		t.Errorf("new board overwritten with %q", got)
	}
	if _, err := os.Stat(filepath.Join(base, "projects", "agent-board")); err != nil {
		t.Errorf("legacy dir touched: %v", err)
	}
}

func TestMigrateLegacyProjectNoLegacyDir(t *testing.T) {
	base := t.TempDir()
	p := Project{Key: "a-b", LegacyKey: "b"}
	if err := MigrateLegacyProject(base, p); err != nil {
		t.Fatalf("MigrateLegacyProject: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "projects", "a-b")); !os.IsNotExist(err) {
		t.Errorf("migration created the new dir: %v", err)
	}
}

func TestLoadFromDirSeparatesKeyFromName(t *testing.T) {
	base := t.TempDir()
	p := Project{Key: "ayandexyz-agent-board", Name: "agent-board"}

	cfg, err := LoadFromDir(base, p)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if want := filepath.Join(base, "projects", p.Key, "board.db"); cfg.DB.Path != want {
		t.Errorf("DB.Path = %q, want %q", cfg.DB.Path, want)
	}
	if cfg.Board.Prefix != "AGE-" {
		t.Errorf("Prefix = %q, want AGE- (from name, not key)", cfg.Board.Prefix)
	}
	if cfg.ProjectName != "agent-board" || cfg.ProjectKey != p.Key {
		t.Errorf("ProjectName/Key = %q/%q", cfg.ProjectName, cfg.ProjectKey)
	}
	if cfg.Project() != p {
		t.Errorf("Project() = %+v, want %+v", cfg.Project(), p)
	}
}

func TestRepoRootResolvesALinkedWorktreeToItsMainRepository(t *testing.T) {
	main := t.TempDir()
	g := &fakeGit{responses: map[string]string{commonDirArgs: filepath.Join(main, ".git")}}

	root, ok := RepoRoot(g.run, filepath.Join(t.TempDir(), "worktrees", "FRE-01"))
	if !ok {
		t.Fatal("RepoRoot reported no repository")
	}
	if want := canonicalPath(main); root != want {
		t.Errorf("RepoRoot = %q, want %q", root, want)
	}
}

func TestRepoRootOutsideGitIsNotARepository(t *testing.T) {
	g := &fakeGit{responses: map[string]string{}}
	if _, ok := RepoRoot(g.run, t.TempDir()); ok {
		t.Error("RepoRoot reported a repository outside git")
	}
}
