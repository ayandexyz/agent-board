package main

import (
	"fmt"
)

// preflight checks for the tools the board cannot start without, so a
// fresh install fails with what to do instead of an exec error.
func preflight(lookPath func(string) (string, error)) error {
	if _, err := lookPath("tmux"); err != nil {
		return fmt.Errorf(`agentboard needs tmux, which is not on your PATH.
Install it, then run agentboard again:
  Debian/Ubuntu:  sudo apt install tmux
  Arch:           sudo pacman -S tmux
  Fedora:         sudo dnf install tmux
  macOS:          brew install tmux
  Termux:         pkg install tmux`)
	}
	return nil
}
