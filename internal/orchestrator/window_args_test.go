package orchestrator

import (
	"reflect"
	"testing"
)

func TestNewWindowArgs(t *testing.T) {
	tests := []struct {
		name    string
		workDir string
		env     []string
		want    []string
	}{
		{
			name:    "without workdir",
			workDir: "",
			want:    []string{"new-window", "-t", "proj", "-d", "-a", "-P", "-F", "#{window_id}:#{pane_id}", "-n", "agent-abc"},
		},
		{
			name:    "with workdir",
			workDir: "/wt/AGT-01",
			want:    []string{"new-window", "-t", "proj", "-d", "-a", "-P", "-F", "#{window_id}:#{pane_id}", "-n", "agent-abc", "-c", "/wt/AGT-01"},
		},
		{
			name:    "with env",
			workDir: "/wt/AGT-01",
			env:     []string{"AGENTBOARD_SESSION_ID=SES-01", "FREECODE_HOOK_URL=http://127.0.0.1:9/hooks/SES-01"},
			want: []string{"new-window", "-t", "proj", "-d", "-a", "-P", "-F", "#{window_id}:#{pane_id}", "-n", "agent-abc", "-c", "/wt/AGT-01",
				"-e", "AGENTBOARD_SESSION_ID=SES-01", "-e", "FREECODE_HOOK_URL=http://127.0.0.1:9/hooks/SES-01"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newWindowArgs("proj", "agent-abc", tt.workDir, tt.env)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newWindowArgs = %v, want %v", got, tt.want)
			}
		})
	}
}
