package main

import (
	"os"

	"github.com/promise-language/reactor/tools/build/common"
)

var (
	repoRoot   = ""
	sourceHash = ""
)

func main() {
	// CheckStale first: the commit gate is only meaningful if it runs the
	// current logic. If the tools are out of sync, this refuses the commit and
	// points at ./make — you must rebuild (and therefore fix any broken tool)
	// before committing. That is what keeps a broken bin/precommit from ever
	// being committed into an unrecoverable state. Recovery is ./make (ungated,
	// 'go run'), never a commit, so blocking here is a speed bump, not a trap.
	common.CheckStale(repoRoot, sourceHash)
	if err := common.RunPrecommit(repoRoot); err != nil {
		os.Exit(1)
	}
}
