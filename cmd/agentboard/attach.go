package main

import "fmt"

// attach replaces this process with the tmux client. Waiting on tmux as a
// child would keep a second copy of the board resident for the whole session
// doing nothing.
func attach(argv, env []string, lookPath func(string) (string, error),
	execve func(string, []string, []string) error) error {
	path, err := lookPath(argv[0])
	if err != nil {
		return fmt.Errorf("attach: %w", err)
	}
	if err := execve(path, argv, env); err != nil {
		return fmt.Errorf("attach: %w", err)
	}
	return nil
}
