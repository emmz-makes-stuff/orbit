// Command orbit creates or attaches to an isolated OrbStack VM for the current workspace directory.
package main

import (
	"os"

	"github.com/emmz-makes-stuff/orbit/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
