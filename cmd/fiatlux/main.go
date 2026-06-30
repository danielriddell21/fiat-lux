package main

import (
	"fmt"
	"os"

	"github.com/danielriddell21/fiat-lux/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
