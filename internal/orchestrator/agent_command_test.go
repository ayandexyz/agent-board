package orchestrator

import (
	"os"
	"testing"
)

func TestAgentCommand(t *testing.T) {
	tests := []struct {
		name string
		bin  string
		args []string
		want string
	}{
		{name: "no args", bin: "claude", want: "claude"},
		{
			name: "settings path is one shell word",
			bin:  "claude",
			args: []string{"--settings", "/home/u/.agentboard/cache/settings-SES-01.json"},
			want: `claude --settings '/home/u/.agentboard/cache/settings-SES-01.json'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentCommand(tt.bin, tt.args); got != tt.want {
				t.Errorf("agentCommand = %s, want %s", got, tt.want)
			}
		})
	}
}

// The settings document is cached to a file rather than typed into the pane:
// inline it is well over a kilobyte of JSON in the user's scrollback.
func TestLaunchArgsWritesSettingsFile(t *testing.T) {
	dir := t.TempDir()

	args, err := launchArgs(dir, "SES-01", `{"hooks":{}}`)
	if err != nil {
		t.Fatalf("launchArgs() error = %v", err)
	}
	if len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("args = %v, want --settings <path>", args)
	}
	body, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	if string(body) != `{"hooks":{}}` {
		t.Errorf("settings file = %s", body)
	}
	if got := agentCommand("claude", args); len(got) > 120 {
		t.Errorf("launch line is %d chars, too long for a pane: %s", len(got), got)
	}
}

func TestLaunchArgsWithoutSettings(t *testing.T) {
	args, err := launchArgs(t.TempDir(), "SES-01", "")
	if err != nil {
		t.Fatalf("launchArgs() error = %v", err)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}
