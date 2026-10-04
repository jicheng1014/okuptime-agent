package main

import (
	"os"

	"github.com/jicheng1014/okuptime-agent/internal/cli"
)

var version = "dev"
var updatePublicKey string

func main() {
	os.Exit(cli.RunWithBuildInfo(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version, updatePublicKey))
}
