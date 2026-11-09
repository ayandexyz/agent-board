package activity

import (
	"strings"
	"testing"
)

func TestToolDetailReadsTheInterestingField(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{
			name:  "a question is quoted as asked",
			tool:  "AskUserQuestion",
			input: `{"questions":[{"question":"In Go, what does a nil map do when you write to it?","header":"Go maps","options":[{"label":"panics"},{"label":"grows"}]}]}`,
			want:  "In Go, what does a nil map do when you write to it?",
		},
		{
			name:  "several questions say how many",
			tool:  "AskUserQuestion",
			input: `{"questions":[{"question":"First?"},{"question":"Second?"},{"question":"Third?"}]}`,
			want:  "First? (+2 more)",
		},
		{
			name:  "a command is the command",
			tool:  "Bash",
			input: `{"command":"rm -rf build","description":"clean"}`,
			want:  "Bash: rm -rf build",
		},
		{
			name:  "a file edit is the path",
			tool:  "Edit",
			input: `{"file_path":"/repo/main.go","old_string":"a","new_string":"b"}`,
			want:  "Edit: /repo/main.go",
		},
		{
			name:  "a camelCase path is the path",
			tool:  "edit",
			input: `{"filePath":"/repo/main.go","oldString":"a","newString":"b"}`,
			want:  "edit: /repo/main.go",
		},
		{
			name:  "a fetch is the url",
			tool:  "WebFetch",
			input: `{"url":"https://example.com/x","prompt":"summarise"}`,
			want:  "WebFetch: https://example.com/x",
		},
		{
			name:  "an unknown tool falls back to its name",
			tool:  "SomeMcpTool",
			input: `{"weird":{"nested":true}}`,
			want:  "SomeMcpTool",
		},
		{
			name:  "no input at all is still the name",
			tool:  "Bash",
			input: ``,
			want:  "Bash",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolDetail(tt.tool, []byte(tt.input)); got != tt.want {
				t.Errorf("toolDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolDetailTruncatesOnRunes(t *testing.T) {
	long := strings.Repeat("é", 400)
	got := toolDetail("Bash", []byte(`{"command":"`+long+`"}`))
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long detail was not truncated: %q", got)
	}
	if n := len([]rune(got)); n > 121 {
		t.Errorf("detail is %d runes, want ~120", n)
	}
	if !strings.Contains(got, "é") || strings.Contains(got, "�") {
		t.Errorf("truncation split a multi-byte rune: %q", got)
	}
}

// A question is not a permission dialog: the pane is waiting on an answer, and
// telling the user it "needs approval" points them at the wrong thing.
func TestAskUserQuestionIsWaitingNotBlocked(t *testing.T) {
	r, base, alerts := newRunning(t)

	post(t, base, "s1", "", `{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which one?"}]}}`)

	if got := r.StateOf("s1"); got != StateWaitingInput {
		t.Errorf("state = %q, want waiting_input", got)
	}
	select {
	case a := <-alerts:
		if !strings.Contains(a[0], "needs your answer") {
			t.Errorf("title = %q, want it to ask for an answer", a[0])
		}
		if a[1] != "Which one?" {
			t.Errorf("body = %q, want the question", a[1])
		}
	default:
		t.Fatal("no alert raised")
	}
}
