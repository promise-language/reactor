package common

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type step struct {
	name string
	run  func(repoRoot string) error
}

// RunVerify is the commit gate: format → vet → build → test. It always prints a
// summary block (even on failure) so an agent tailing the output sees the
// result without re-running, and the process exit code is the only contract.
//
// This is an EXAMPLE pipeline. For a Go project it runs real go tooling; for
// anything else it runs harmless stubs. Replace verifySteps with your project's
// real commands.
func RunVerify(repoRoot string, args []string) error {
	steps := verifySteps(repoRoot)
	start := time.Now()

	type result struct {
		name string
		ok   bool
	}
	var results []result
	failed := false

	for _, s := range steps {
		fmt.Printf("==> %s\n", s.name)
		err := s.run(repoRoot)
		results = append(results, result{s.name, err == nil})
		if err != nil {
			failed = true
			fmt.Fprintf(os.Stderr, "    %s failed: %v\n", s.name, err)
			break // stop at the first failure
		}
	}

	fmt.Println("\n──────── verify summary ────────")
	for _, r := range results {
		status := "ok"
		if !r.ok {
			status = "FAIL"
		}
		fmt.Printf("  %-4s  %s\n", status, r.name)
	}
	fmt.Printf("  elapsed %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Println("────────────────────────────────")

	if failed {
		fmt.Println("❌ Verify FAILED: not safe to commit")
		return fmt.Errorf("verify failed")
	}
	fmt.Println("✅ OK to Commit")
	return nil
}

func verifySteps(repoRoot string) []step {
	if Exists(filepath.Join(repoRoot, "go.mod")) {
		return []step{
			{"format", func(r string) error { return RunIn(r, "gofmt", "-w", ".") }},
			{"vet", func(r string) error { return RunIn(r, "go", "vet", "./...") }},
			{"build", func(r string) error {
				// Build into a throwaway dir so a single-main-package tree
				// (e.g. just cmd/reactor) does not litter the worktree with a
				// stray binary. -o <dir> writes each main package's output
				// there and discards non-main packages; works for one or many.
				out, err := os.MkdirTemp("", "reactor-build-")
				if err != nil {
					return err
				}
				defer os.RemoveAll(out)
				return RunIn(r, "go", "build", "-o", out, "./...")
			}},
			{"test", func(r string) error { return RunIn(r, "go", "test", "./...") }},
		}
	}
	stub := func(label string) step {
		return step{label, func(r string) error {
			fmt.Printf("    (stub) wire up your %s command in tools/build/common/verify.go\n", label)
			return nil
		}}
	}
	return []step{stub("format"), stub("vet"), stub("build"), stub("test")}
}
