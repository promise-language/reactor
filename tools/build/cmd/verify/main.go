package main

import (
	"fmt"
	"os"

	"github.com/promise-language/reactor/tools/build/common"
)

// Injected by the meta-builder via -ldflags at build time; empty otherwise.
var (
	repoRoot   = ""
	sourceHash = ""
)

func main() {
	common.CheckStale(repoRoot, sourceHash)
	if err := common.RunVerify(repoRoot, common.NormalizeArgs(os.Args[1:])); err != nil {
		fmt.Fprintln(os.Stderr, "verify failed:", err)
		os.Exit(1)
	}
}
