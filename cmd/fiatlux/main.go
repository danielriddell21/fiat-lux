// Command fiatlux is the entrypoint for the fiat-lux CLI/TUI.
//
// fiat-lux drops an AI agent into an empty world and gives it tools to
// create. The `run` subcommand launches the TUI. Bare invocation
// prints a banner.
package main

import (
	"fmt"
	"os"

	"github.com/danielriddell21/fiat-lux/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
