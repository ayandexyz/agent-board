package main

import (
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"testing"
)

func TestPreflightNamesMissingTmux(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "tmux" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + name, nil
	}

	err := preflight(lookPath)

	if err == nil || !strings.Contains(err.Error(), "tmux") {
		t.Fatalf("err = %v, want one naming tmux", err)
	}
}

func TestPreflightPassesWithTmux(t *testing.T) {
	lookPath := func(name string) (string, error) { return "/usr/bin/" + name, nil }

	if err := preflight(lookPath); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateFailureHintsAtPermissions(t *testing.T) {
	err := updateFailure(fs.ErrPermission, "/usr/local/bin/agentboard")

	if !strings.Contains(err, "/usr/local/bin") || !strings.Contains(err, "sudo") {
		t.Fatalf("message = %q, want the install dir and a sudo hint", err)
	}
	if plain := updateFailure(errors.New("boom"), "/x/agentboard"); strings.Contains(plain, "sudo") {
		t.Fatalf("message = %q, want no sudo hint for other failures", plain)
	}
}
