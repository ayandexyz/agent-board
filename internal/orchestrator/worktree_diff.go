package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ayandexyz/agent-board/internal/git"
	"github.com/ayandexyz/agent-board/internal/store"
)

const maxInlineUntracked = 256 << 10

type TicketDiff struct {
	Base  string
	Files []git.FileChange
	Patch string
}

type Differ interface {
	Diff(ctx context.Context, t store.Ticket) (TicketDiff, error)
}

func (m *WorktreeManager) Diff(ctx context.Context, t store.Ticket) (TicketDiff, error) {
	dir, from := m.repoRoot, "HEAD"
	if t.UsesWorktree() {
		if t.WorktreePath == "" {
			return TicketDiff{}, nil
		}
		if _, err := os.Stat(t.WorktreePath); err != nil {
			return TicketDiff{}, nil
		}
		dir = t.WorktreePath
		base, err := m.base(ctx)
		if err != nil {
			return TicketDiff{}, fmt.Errorf("orchestrator.diff: %w", err)
		}
		from, err = m.git.MergeBase(ctx, dir, base, "HEAD")
		if err != nil {
			return TicketDiff{}, fmt.Errorf("orchestrator.diff: %w", err)
		}
	}

	files, err := m.git.DiffNumstat(ctx, dir, from)
	if err != nil {
		return TicketDiff{}, fmt.Errorf("orchestrator.diff: %w", err)
	}
	patch, err := m.git.Diff(ctx, dir, from)
	if err != nil {
		return TicketDiff{}, fmt.Errorf("orchestrator.diff: %w", err)
	}
	untracked, err := m.git.UntrackedFiles(ctx, dir)
	if err != nil {
		return TicketDiff{}, fmt.Errorf("orchestrator.diff: %w", err)
	}

	var b strings.Builder
	b.WriteString(patch)
	for _, path := range untracked {
		change, text := untrackedPatch(dir, path)
		files = append(files, change)
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(text)
	}
	return TicketDiff{Base: from, Files: files, Patch: b.String()}, nil
}

func untrackedPatch(dir, path string) (git.FileChange, string) {
	change := git.FileChange{Path: path, Untracked: true}
	header := fmt.Sprintf("diff --git a/%s b/%s\nnew file\n", path, path)
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return change, header + fmt.Sprintf("Unreadable file %s: %v", path, err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		change.Binary = true
		return change, header + fmt.Sprintf("Binary file %s", path)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	change.Added = len(lines)
	if len(data) > maxInlineUntracked {
		return change, header + fmt.Sprintf("File %s too large to show (%d bytes)", path, len(data))
	}
	var b strings.Builder
	b.WriteString(header)
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@", path, len(lines))
	for _, l := range lines {
		b.WriteString("\n+")
		b.WriteString(l)
	}
	return change, b.String()
}

var ErrNoRepository = errors.New("orchestrator: not in a git repository")

func (s *Service) TicketDiff(ctx context.Context, ticketID string) (TicketDiff, error) {
	if s.differ == nil {
		return TicketDiff{}, ErrNoRepository
	}
	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return TicketDiff{}, err
	}
	return s.differ.Diff(ctx, ticket)
}
