package agents

import (
	"strings"
	"testing"

	"github.com/ayan-de/agent-board/internal/activity"
)

func TestCatalogIsCoherent(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range All() {
		if seen[d.Binary] {
			t.Errorf("%s: binary %q claimed twice", d.Name, d.Binary)
		}
		seen[d.Binary] = true
		if d.Supports(CapPermissionRemote) != (d.Decisions != nil) {
			t.Errorf("%s: %s and decision codec disagree", d.Name, CapPermissionRemote)
		}
		if d.Provision != nil && !d.Supports(CapActivityReports) {
			t.Errorf("%s: provisions reporting without claiming %s", d.Name, CapActivityReports)
		}
		if d.Spawnable && d.Logo == "" {
			t.Errorf("%s: offered in the picker with no logo", d.Name)
		}
	}
}

func TestRegisterRejectsContradictions(t *testing.T) {
	tests := []struct {
		name string
		desc Descriptor
	}{
		{"no name", Descriptor{Binary: "x"}},
		{"no binary", Descriptor{Name: "x"}},
		{"already registered", Descriptor{Name: "opencode", Binary: "opencode"}},
		{"claims remote decisions but supplies no codec", Descriptor{
			Name: "x", Binary: "x", Capabilities: []Capability{CapPermissionRemote}}},
		{"supplies a codec it never claimed", Descriptor{
			Name: "x", Binary: "x", Decisions: activity.BareDecision}},
		{"provisions reporting it never claimed", Descriptor{
			Name: "x", Binary: "x", Provision: func(Provision) (Launch, error) { return Launch{}, nil }}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register accepted a contradictory descriptor")
				}
			}()
			Register(tt.desc)
		})
	}
}

func TestLookupResolvesAliases(t *testing.T) {
	d, ok := Lookup("claudecode")
	if !ok || d.Name != "claude-code" {
		t.Fatalf("Lookup(claudecode) = %q, %v", d.Name, ok)
	}
	if _, ok := Lookup("nonesuch"); ok {
		t.Fatal("unknown agent resolved")
	}
	if Supports("nonesuch", CapActivityReports) {
		t.Fatal("unknown agent claims a capability")
	}
}

func TestSpawnableExcludesRecognisedOnlyAgents(t *testing.T) {
	for _, d := range Spawnable() {
		if d.Name == "gemini" {
			t.Fatal("gemini is recognised for resume only, not offered")
		}
	}
	if _, ok := Lookup("gemini"); !ok {
		t.Fatal("gemini dropped from the catalog entirely")
	}
}

func TestResumePatternsCoverEveryAgentThatPrintsOne(t *testing.T) {
	lines := map[string]string{
		"claude-code": "claude --resume abc123",
		"opencode":    "opencode -s ses_42",
		"freecode":    "freecode --resume abc123",
		"codex":       "codex --resume abc123",
		"gemini":      "gemini --resume abc123",
	}
	for name, line := range lines {
		d, ok := Lookup(name)
		if !ok || d.Resume == nil {
			t.Fatalf("%s has no resume pattern", name)
		}
		if m := d.Resume.FindStringSubmatch(line); len(m) < 2 || m[1] != line {
			t.Errorf("%s did not recover %q", name, line)
		}
	}
}

func TestProvisionWiresEachAgentToTheReceiver(t *testing.T) {
	p := Provision{SessionID: "SES-01", LaunchID: "L1", HookBase: "http://127.0.0.1:9", CacheDir: t.TempDir()}

	claude, _ := Lookup("claude-code")
	got, err := claude.Provision(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != 0 {
		t.Errorf("claude-code exported env it does not read: %v", got.Env)
	}
	if !strings.Contains(got.Settings, p.HookURL()) {
		t.Errorf("claude-code settings do not point at %s", p.HookURL())
	}

	free, _ := Lookup("freecode")
	got, err = free.Provision(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := "FREECODE_HOOK_URL=" + p.HookURL(); len(got.Env) != 1 || got.Env[0] != want {
		t.Errorf("freecode env = %v, want [%s]", got.Env, want)
	}

	open, _ := Lookup("opencode")
	got, err = open.Provision(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != 1 || !strings.HasPrefix(got.Env[0], "OPENCODE_CONFIG=") {
		t.Errorf("opencode env = %v, want an OPENCODE_CONFIG path", got.Env)
	}
}

func TestNonReportingAgentsProvisionNothing(t *testing.T) {
	for _, name := range []string{"codex", "cursor", "gemini"} {
		d, _ := Lookup(name)
		if d.Supports(CapActivityReports) {
			t.Errorf("%s claims to report activity; give it a Provision", name)
		}
		if d.Provision != nil {
			t.Errorf("%s provisions reporting it cannot do", name)
		}
	}
}
