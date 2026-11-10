package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const pinConfigKey = "agentboard.project"

type Project struct {
	Key       string
	Name      string
	LegacyKey string
}

type GitRunner func(dir string, args ...string) (string, error)

func ExecGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func ResolveProject(run GitRunner, cwd string) Project {
	remote, err := run(cwd, "remote", "get-url", "origin")
	if err != nil {
		remote = ""
	}
	remote = strings.TrimSpace(remote)
	legacy := ExtractProjectName(remote, filepath.Base(cwd))

	root, ok := mainRepoRoot(run, cwd)
	if !ok {
		dir := canonicalPath(cwd)
		return Project{Key: pathKey(dir), Name: slugify(filepath.Base(dir)), LegacyKey: legacy}
	}

	name := ExtractProjectName(remote, filepath.Base(root))

	if pinned, err := run(cwd, "config", "--local", "--get", pinConfigKey); err == nil {
		if key := slugify(pinned); key != "" {
			return Project{Key: key, Name: name, LegacyKey: legacy}
		}
	}

	key := remoteKey(remote)
	if key == "" {
		key = pathKey(root)
	}
	_, _ = run(cwd, "config", "--local", pinConfigKey, key)

	return Project{Key: key, Name: name, LegacyKey: legacy}
}

func mainRepoRoot(run GitRunner, cwd string) (string, bool) {
	common, err := run(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	common = strings.TrimSpace(common)
	if err != nil || common == "" {
		return "", false
	}
	common = canonicalPath(common)
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), true
	}
	return common, true
}

func canonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

func remoteKey(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}

	path := remote
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil {
			path = u.Path
		}
	} else if i := strings.Index(remote, ":"); i > 0 && !strings.Contains(remote[:i], "/") {
		path = remote[i+1:]
	}

	var segments []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) == 0 {
		return ""
	}
	segments[len(segments)-1] = strings.TrimSuffix(segments[len(segments)-1], ".git")
	if len(segments) > 2 {
		segments = segments[len(segments)-2:]
	}
	return slugify(strings.Join(segments, "-"))
}

func pathKey(path string) string {
	sum := sha256.Sum256([]byte(path))
	name := slugify(filepath.Base(path))
	if name == "" {
		name = "project"
	}
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}

func MigrateLegacyProject(baseDir string, p Project) error {
	if p.LegacyKey == "" || p.LegacyKey == p.Key {
		return nil
	}
	dst := filepath.Join(baseDir, "projects", p.Key)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	src := filepath.Join(baseDir, "projects", p.LegacyKey)
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("config.migrateProject: %s -> %s: %w", src, dst, err)
	}
	return nil
}

// RepoRoot returns the root of the main repository containing dir, so a
// subdirectory or a linked worktree resolves to the checkout it belongs to.
func RepoRoot(run GitRunner, dir string) (string, bool) {
	return mainRepoRoot(run, dir)
}
