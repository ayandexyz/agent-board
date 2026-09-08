package orchestrator

import "testing"

func TestAgentCommand(t *testing.T) {
	tests := []struct {
		name string
		bin  string
		args []string
		want string
	}{
		{name: "no args", bin: "claude", want: "claude"},
		{
			name: "settings json is one shell word",
			bin:  "claude",
			args: []string{"--settings", `{"hooks":{"Stop":[]}}`},
			want: `claude --settings '{"hooks":{"Stop":[]}}'`,
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
