package common

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// PromiseProjects returns the Promise project directories under cmd/, sorted —
// one per deployable (reactor, and later runner and governor). A directory
// counts as a project when it carries its own promise.toml; that manifest is
// what makes it a module with its own main(), so this list is exactly the set
// of binaries the repo produces.
func PromiseProjects(repoRoot string) []string {
	entries, err := os.ReadDir(filepath.Join(repoRoot, "cmd"))
	if err != nil {
		return nil
	}
	var projects []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.Join("cmd", e.Name())
		if Exists(filepath.Join(repoRoot, rel, "promise.toml")) {
			projects = append(projects, rel)
		}
	}
	sort.Strings(projects)
	return projects
}

func verifySteps(repoRoot string) []step {
	projects := PromiseProjects(repoRoot)
	if len(projects) == 0 {
		stub := func(label string) step {
			return step{label, func(r string) error {
				fmt.Printf("    (stub) no Promise projects under cmd/ — nothing to %s\n", label)
				return nil
			}}
		}
		return []step{stub("format"), stub("build"), stub("test")}
	}

	// Each project is built to a throwaway dir so verify never litters the
	// worktree with a stray binary; bin/ is for deliberate builds only.
	eachProject := func(fn func(r, project string) error) func(string) error {
		return func(r string) error {
			for _, p := range projects {
				if err := fn(r, p); err != nil {
					return fmt.Errorf("%s: %w", p, err)
				}
			}
			return nil
		}
	}

	return []step{
		{"format", eachProject(func(r, p string) error {
			return RunIn(r, "promise", "format", p)
		})},
		{"build", eachProject(func(r, p string) error {
			out, err := os.MkdirTemp("", "reactor-build-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(out)
			return RunIn(r, "promise", "build", "-o", filepath.Join(out, filepath.Base(p)), p)
		})},
		// `promise test` exits 0 and prints "no test files found" when a project
		// has none, so this is a no-op until tests exist rather than a failure.
		{"test", eachProject(func(r, p string) error {
			return RunIn(r, "promise", "test", p)
		})},
	}
}
