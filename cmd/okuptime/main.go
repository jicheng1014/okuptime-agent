package main

import (
	"os"

	"github.com/jicheng1014/okuptime-agent/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
