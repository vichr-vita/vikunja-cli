package main

import (
	"context"
	"os"

	"github.com/vichr-vita/vikunja-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), cli.Options{
		Args:   os.Args[1:],
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}))
}
