package activity

import (
	"encoding/json"
	"fmt"
	"strings"
)

const detailLimit = 120

// toolFields names the one argument worth reading for each tool, in the order
// they are tried. Anything unlisted shows as the bare tool name: a raw JSON
// dump of the arguments is noise in a notification.
var toolFields = []string{"command", "file_path", "path", "url", "pattern", "description", "prompt"}

// toolDetail renders a tool call as a line a human can act on.
func toolDetail(toolName string, toolInput []byte) string {
	if len(toolInput) == 0 {
		return toolName
	}
	if toolName == "AskUserQuestion" {
		if q := questionDetail(toolInput); q != "" {
			return truncate(q, detailLimit)
		}
		return toolName
	}

	var args map[string]json.RawMessage
	if err := json.Unmarshal(toolInput, &args); err != nil {
		return toolName
	}
	for _, field := range toolFields {
		raw, ok := args[field]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
			continue
		}
		return truncate(toolName+": "+collapse(value), detailLimit)
	}
	return toolName
}

// questionDetail quotes the question itself, which is the whole point of the
// alert, and counts the rest.
func questionDetail(toolInput []byte) string {
	var args struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(toolInput, &args); err != nil || len(args.Questions) == 0 {
		return ""
	}
	first := collapse(args.Questions[0].Question)
	if first == "" {
		return ""
	}
	if rest := len(args.Questions) - 1; rest > 0 {
		return fmt.Sprintf("%s (+%d more)", first, rest)
	}
	return first
}

// collapse folds the wrapped, indented text of a tool argument onto one line.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
