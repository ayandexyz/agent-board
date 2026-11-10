package main

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestAttachReplacesTheProcessWithTmux(t *testing.T) {
	lookPath := func(name string) (string, error) { return "/usr/bin/" + name, nil }
	var gotPath string
	var gotArgv, gotEnv []string
	execve := func(path string, argv, env []string) error {
		gotPath, gotArgv, gotEnv = path, argv, env
		return nil
	}

	err := attach([]string{"tmux", "attach-session", "-t", "proj"}, []string{"A=1"}, lookPath, execve)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotPath != "/usr/bin/tmux" {
		t.Errorf("path = %q, want the resolved tmux", gotPath)
	}
	if want := []string{"tmux", "attach-session", "-t", "proj"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("argv = %v, want %v", gotArgv, want)
	}
	if !reflect.DeepEqual(gotEnv, []string{"A=1"}) {
		t.Errorf("env = %v, want the caller's environment", gotEnv)
	}
}

func TestAttachReportsMissingTmux(t *testing.T) {
	lookPath := func(string) (string, error) { return "", exec.ErrNotFound }
	execve := func(string, []string, []string) error {
		t.Fatal("exec called without a tmux binary")
		return nil
	}

	if err := attach([]string{"tmux", "attach-session"}, nil, lookPath, execve); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want exec.ErrNotFound", err)
	}
}
