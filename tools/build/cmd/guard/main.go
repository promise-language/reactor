package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/promise-language/reactor/tools/build/common"
)

// Injected by the meta-builder via -ldflags. The guard reads these to detect
// its own staleness — but, unlike pipeline tools, it never calls CheckStale: a
// stale guard must keep running to enforce the lockdown and permit recovery.
var (
	repoRoot   = ""
	sourceHash = ""
)

func main() {
	// Fail open on any input trouble: a guard that can't read its stdin must
	// not wedge the agent. Real policy lives in common.Guard.
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(0)
	}
	var in common.HookInput
	if err := json.Unmarshal(data, &in); err != nil {
		os.Exit(0)
	}
	decision := common.Guard(repoRoot, sourceHash, in)
	if decision.Allowed {
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, decision.Reason)
	os.Exit(2) // exit 2 = block the tool call; stderr is fed back to the agent.
}
