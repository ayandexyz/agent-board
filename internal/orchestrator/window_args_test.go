package orchestrator

import (
	"reflect"
	"testing"
)

func TestNewWindowArgs(t *testing.T) {
	tests := []struct {
		name    string
		workDir string
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newWindowArgs("proj", "agent-abc", tt.workDir)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newWindowArgs = %v, want %v", got, tt.want)
			}
		})
	}
}
