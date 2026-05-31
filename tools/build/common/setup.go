package common

// RunSetup wires git to use the in-repo .githooks directory. Idempotent and
// fast, so the meta-builder calls it on every run; a fresh clone gets its
// pre-commit hook on the first ./make.
func RunSetup(repoRoot string) error {
	return RunIn(repoRoot, "git", "config", "core.hooksPath", ".githooks")
}
