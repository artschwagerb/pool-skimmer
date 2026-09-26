package main

import (
	"fmt"
	"os"

	"pool-skimmer/internal/cli"
)

var version = "dev"

func main() {
	command := cli.NewRootCommand(version)
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "pool-skimmer: error: %v\n", err)
		os.Exit(1)
	}
}
