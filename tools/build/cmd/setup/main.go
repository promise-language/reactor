package main

import (
	"fmt"
	"os"

	"github.com/promise-language/reactor/tools/build/common"
)

var (
	repoRoot   = ""
	sourceHash = ""
)

func main() {
	common.CheckStale(repoRoot, sourceHash)
	if err := common.RunSetup(repoRoot); err != nil {
		fmt.Fprintln(os.Stderr, "setup failed:", err)
		os.Exit(1)
	}
	fmt.Println("git hooks configured (core.hooksPath = .githooks)")
}
