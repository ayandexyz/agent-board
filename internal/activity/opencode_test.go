package activity

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeConfigWritesPluginAndConfig(t *testing.T) {
	dir := t.TempDir()
	path, err := OpenCodeConfig(dir)
	if err != nil {
		t.Fatalf("OpenCodeConfig() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("config is not json: %v", err)
	}
	if len(cfg.Plugin) != 1 || !strings.HasPrefix(cfg.Plugin[0], "file://") {
		t.Fatalf("config plugin = %v, want one file:// entry", cfg.Plugin)
	}
	js := strings.TrimPrefix(cfg.Plugin[0], "file://")
	if filepath.Dir(js) != dir {
		t.Errorf("plugin written to %s, want inside %s", js, dir)
	}
	if _, err := os.Stat(js); err != nil {
		t.Fatalf("plugin file missing: %v", err)
	}
}

func TestOpenCodeConfigIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenCodeConfig(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := OpenCodeConfig(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Errorf("path changed between calls: %s then %s", first, second)
	}
}

// The plugin is shipped as source, so a syntax error would only surface in a
// user's pane. Parse it here with whatever JS runtime is on PATH.
func TestOpenCodePluginParses(t *testing.T) {
	bin, err := exec.LookPath("bun")
	if err != nil {
		if bin, err = exec.LookPath("node"); err != nil {
			t.Skip("no bun or node on PATH")
		}
	}
	dir := t.TempDir()
	if _, err := OpenCodeConfig(dir); err != nil {
		t.Fatalf("OpenCodeConfig() error = %v", err)
	}
	out, err := exec.Command(bin, "--check", filepath.Join(dir, openCodePluginFile)).CombinedOutput()
	if err != nil {
		t.Fatalf("%s --check failed: %v\n%s", bin, err, out)
	}
}
