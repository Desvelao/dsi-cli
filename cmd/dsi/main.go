package main

import (
	"os"

	"github.com/Desvelao/dsipy/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.ExecuteEnv(version, os.Args[1:], cli.NewEnvFromOS()))
}
