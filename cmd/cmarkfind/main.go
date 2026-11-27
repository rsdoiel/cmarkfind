// Command cmarkfind keeps a personal corpus of Markdown pages and searches it
// (cmarkfind DR-0002).
package main

import (
	"os"

	"github.com/rsdoiel/cmarkfind/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
