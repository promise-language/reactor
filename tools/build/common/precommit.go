package common

import (
	"fmt"
	"os"
	"strings"
)

// RunPrecommit enforces fast, test-free invariants before a commit. It is the
// body of bin/precommit, which .githooks/pre-commit execs. Keep it sub-second:
// anything that needs to run tests belongs in bin/verify, which the developer
// runs explicitly — putting tests here makes commits slow and gets the hook
// disabled, which kills the whole gate.
//
// The starter check blocks staged binaries under bin/ (gitignored, built by
// ./make). Grow it with forbidden-pattern scans and ratcheted-baseline checks.
func RunPrecommit(repoRoot string) error {
	staged, err := RunOutputIn(repoRoot, "git", "diff", "--cached", "--name-only")
	if err != nil {
		return fmt.Errorf("listing staged files: %w", err)
	}
	var offenders []string
	for _, line := range strings.Split(staged, "\n") {
		f := strings.TrimSpace(line)
		if f == "bin" || strings.HasPrefix(f, "bin/") {
			offenders = append(offenders, f)
		}
	}
	if len(offenders) > 0 {
		fmt.Fprintln(os.Stderr, "❌ pre-commit: refusing to commit built binaries:")
		for _, f := range offenders {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintf(os.Stderr, "bin/ is gitignored and built by ./make. Unstage with:\n  git reset HEAD %s\n",
			strings.Join(offenders, " "))
		return fmt.Errorf("%d staged binary path(s)", len(offenders))
	}
	return nil
}
