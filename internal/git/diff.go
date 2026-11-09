package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type FileChange struct {
	Path      string
	Added     int
	Deleted   int
	Binary    bool
	Untracked bool
}

func (g *Git) MergeBase(ctx context.Context, dir, a, b string) (string, error) {
	out, err := g.run(ctx, dir, "git", "merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("git.mergeBase: %w", err)
	}
	return out, nil
}

func (g *Git) Diff(ctx context.Context, dir, from string) (string, error) {
	out, err := g.run(ctx, dir, "git", "diff", "--no-color", "--no-ext-diff", from)
	if err != nil {
		return "", fmt.Errorf("git.diff: %w", err)
	}
	return out, nil
}

func (g *Git) DiffNumstat(ctx context.Context, dir, from string) ([]FileChange, error) {
	out, err := g.run(ctx, dir, "git", "diff", "--numstat", "--no-renames", from)
	if err != nil {
		return nil, fmt.Errorf("git.diffNumstat: %w", err)
	}
	var changes []FileChange
	for _, line := range nonEmptyLines(out) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		c := FileChange{Path: parts[2]}
		if parts[0] == "-" && parts[1] == "-" {
			c.Binary = true
		} else {
			c.Added, _ = strconv.Atoi(parts[0])
			c.Deleted, _ = strconv.Atoi(parts[1])
		}
		changes = append(changes, c)
	}
	return changes, nil
}

func (g *Git) UntrackedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := g.run(ctx, dir, "git", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git.untrackedFiles: %w", err)
	}
	return nonEmptyLines(out), nil
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
