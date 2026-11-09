package main

import (
	"io"
	"os"

	"github.com/ayandexyz/agent-board/internal/cli"
	"github.com/ayandexyz/agent-board/internal/version"
)

func main() {
	app := &cli.App{
		Name:    "agentboard",
		Version: version.Version,
		Commands: []cli.Command{
			updateCommand(),
		},
		DefaultRun: func(out io.Writer) int {
			return runTUI()
		},
	}
	os.Exit(app.Execute(os.Stdout, os.Args[1:]))
}
