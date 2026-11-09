package config

import (
	"fmt"
	"os/exec"

	"github.com/ayandexyz/agent-board/internal/agents"
)

type DetectedAgent struct {
	Name    string
	Binary  string
	Path    string
	Found   bool
	Logo    string
	LogoClr string
}

func DetectAgents() []DetectedAgent {
	catalog := agents.Spawnable()
	detected := make([]DetectedAgent, len(catalog))
	for i, spec := range catalog {
		path, err := exec.LookPath(spec.Binary)
		detected[i] = DetectedAgent{
			Name:    spec.Name,
			Binary:  spec.Binary,
			Path:    path,
			Found:   err == nil,
			Logo:    spec.Logo,
			LogoClr: spec.Color,
		}
		if err == nil {
			detected[i].Path = fmt.Sprintf("%s (%s)", spec.Binary, path)
		}
	}
	return detected
}

func AgentColor(name string) string {
	spec, ok := agents.Lookup(name)
	if !ok {
		return ""
	}
	return spec.Color
}

type AgentSpec struct {
	Name    string
	Binary  string
	Logo    string
	LogoClr string
}

func Specs() []AgentSpec {
	catalog := agents.Spawnable()
	out := make([]AgentSpec, len(catalog))
	for i, spec := range catalog {
		out[i] = AgentSpec{
			Name:    spec.Name,
			Binary:  spec.Binary,
			Logo:    spec.Logo,
			LogoClr: spec.Color,
		}
	}
	return out
}
