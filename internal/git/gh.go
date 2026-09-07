package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type GH struct {
	run Runner
}

func NewGH(run Runner) *GH {
	return &GH{run: run}
}

type PRInfo struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

func (g *GH) PRCreate(ctx context.Context, dir, base, head, title, body string) (string, error) {
	out, err := g.run(ctx, dir, "gh", "pr", "create", "--base", base, "--head", head, "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("gh.prCreate: %s: %w", out, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	return url, nil
}

func (g *GH) PRView(ctx context.Context, dir, branch string) (PRInfo, error) {
	out, err := g.run(ctx, dir, "gh", "pr", "view", branch, "--json", "url,state")
	if err != nil {
		return PRInfo{}, fmt.Errorf("gh.prView: %s: %w", out, err)
	}
	var info PRInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return PRInfo{}, fmt.Errorf("gh.prView: parsing output: %w", err)
	}
	return info, nil
}
