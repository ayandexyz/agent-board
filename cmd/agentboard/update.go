package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ayandexyz/agent-board/internal/cli"
	"github.com/ayandexyz/agent-board/internal/selfupdate"
	"github.com/ayandexyz/agent-board/internal/version"
)

const installScriptURL = "https://agentboard.ayande.xyz/install.sh"

func updateCommand() cli.Command {
	return cli.Command{
		Name:  "update",
		Short: "Update agentboard to the latest release",
		Run:   runUpdate,
	}
}

func runUpdate(out io.Writer, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(out)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(out, "error locating current executable: %v\n", err)
		return 1
	}

	fetcher := selfupdate.NewHTTPFetcher(selfupdate.DefaultRepo)
	fmt.Fprintln(out, "checking for updates...")

	rel, needsUpdate, err := selfupdate.Check(fetcher, version.Version)
	if err != nil {
		fmt.Fprintf(out, "error checking latest version: %v\n", err)
		return 1
	}
	if !needsUpdate {
		fmt.Fprintf(out, "agentboard is already up to date (%s)\n", version.Version)
		return 0
	}

	fmt.Fprintf(out, "updating agentboard %s -> %s...\n", version.Version, rel.TagName)
	if err := selfupdate.Apply(fetcher, rel, execPath, runtime.GOOS, runtime.GOARCH); err != nil {
		fmt.Fprintln(out, updateFailure(err, execPath))
		return 1
	}

	fmt.Fprintf(out, "updated to %s\n", rel.TagName)
	return 0
}

func updateFailure(err error, execPath string) string {
	msg := fmt.Sprintf("update failed: %v", err)
	if errors.Is(err, fs.ErrPermission) {
		msg += fmt.Sprintf("\n%s is not writable by you; re-run with sudo, or reinstall to ~/.local/bin:\n  curl -fsSL %s | bash",
			filepath.Dir(execPath), installScriptURL)
	}
	return msg
}
