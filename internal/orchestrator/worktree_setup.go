package orchestrator

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ayandexyz/agent-board/internal/git"
)

const setupTimeout = 10 * time.Minute

type WorktreeSetup struct {
	CopyFiles []string
	Script    string
	Run       git.Runner
}

func (m *WorktreeManager) SetSetup(s WorktreeSetup) {
	m.setup = s
}

func (m *WorktreeManager) prepare(ctx context.Context, path string) error {
	for _, pattern := range m.setup.CopyFiles {
		if err := copyMatches(m.repoRoot, path, pattern); err != nil {
			return err
		}
	}
	if m.setup.Script == "" || m.setup.Run == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	out, err := m.setup.Run(ctx, path, "sh", "-c", m.setup.Script)
	if err != nil {
		return fmt.Errorf("setup script %q: %w\n%s", m.setup.Script, err, tail(out, 20))
	}
	return nil
}

func copyMatches(srcRoot, dstRoot, pattern string) error {
	clean := filepath.Clean(pattern)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("copy_files %q: must be a path inside the repository", pattern)
	}
	matches, err := filepath.Glob(filepath.Join(srcRoot, clean))
	if err != nil {
		return fmt.Errorf("copy_files %q: %w", pattern, err)
	}
	for _, src := range matches {
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return fmt.Errorf("copy_files %q: %w", pattern, err)
		}
		if err := copyTree(src, filepath.Join(dstRoot, rel)); err != nil {
			return fmt.Errorf("copy_files %q: %w", pattern, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
