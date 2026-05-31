package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func bashInput(cmd string) HookInput {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return HookInput{ToolName: "Bash", ToolInput: b}
}

func editInput(path string) HookInput {
	b, _ := json.Marshal(map[string]string{"file_path": path})
	return HookInput{ToolName: "Edit", ToolInput: b}
}

// testRepoRoot derives the repo root from the package dir (<root>/tools/build/common).
func testRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func TestDangerReason(t *testing.T) {
	for _, c := range []string{
		"rm -rf build", "RM -RF /", "foo && rm -fr bar",
		"git reset --hard HEAD~1", "git push --force", "git push -f origin main",
	} {
		if dangerReason(c) == "" {
			t.Errorf("expected %q to be blocked", c)
		}
	}
	for _, c := range []string{"ls -la", "go build ./...", "rm file.txt", "git push origin main"} {
		if r := dangerReason(c); r != "" {
			t.Errorf("expected %q allowed, got %q", c, r)
		}
	}
}

func TestIsRecoveryCommand(t *testing.T) {
	for _, c := range []string{"./make", "make", "./make -force", "go run -C tools/build ./cmd/make"} {
		if !isRecoveryCommand(c) {
			t.Errorf("expected recovery: %q", c)
		}
	}
	for _, c := range []string{"", "ls", "./make && rm -rf /", "echo $(./make)", "./maker"} {
		if isRecoveryCommand(c) {
			t.Errorf("expected not-recovery: %q", c)
		}
	}
}

func TestIsToolCommand(t *testing.T) {
	const root = "/repo"
	for _, c := range []string{"bin/verify", "./bin/test --fast", "/repo/bin/build"} {
		if !isToolCommand(root, c) {
			t.Errorf("expected tool cmd: %q", c)
		}
	}
	for _, c := range []string{"binx/foo", "bin/verify && rm -rf /", "ls bin/"} {
		if isToolCommand(root, c) {
			t.Errorf("expected not tool cmd: %q", c)
		}
	}
}

func TestIsRecoveryPath(t *testing.T) {
	const root = "/repo"
	for _, p := range []string{"tools/build/common/verify.go", "/repo/tools/build/go.mod", "make", "make.cmd"} {
		if !isRecoveryPath(root, p) {
			t.Errorf("expected recovery path: %q", p)
		}
	}
	for _, p := range []string{"", "src/app.go", "/repo/README.md", "tools/other/x.go"} {
		if isRecoveryPath(root, p) {
			t.Errorf("expected not recovery path: %q", p)
		}
	}
}

// When stale (empty compiledHash ⇒ StaleReason non-empty), only the recovery
// surface is allowed and dangerous commands are blocked regardless.
func TestGuardLockdown(t *testing.T) {
	const root = "/repo"
	cases := []struct {
		name    string
		in      HookInput
		allowed bool
	}{
		{"make allowed", bashInput("./make"), true},
		{"old bin tool allowed", bashInput("bin/verify"), true},
		{"arbitrary bash blocked", bashInput("ls"), false},
		{"dangerous blocked", bashInput("rm -rf x"), false},
		{"tools edit allowed", editInput("/repo/tools/build/common/x.go"), true},
		{"project edit blocked", editInput("/repo/src/app.go"), false},
	}
	for _, c := range cases {
		if got := Guard(root, "", c.in); got.Allowed != c.allowed {
			t.Errorf("%s: allowed=%v want %v (reason=%q)", c.name, got.Allowed, c.allowed, got.Reason)
		}
	}
}

// When current (compiledHash matches the real source hash) safe actions are
// allowed and only dangerous commands are blocked.
func TestGuardCurrentAllows(t *testing.T) {
	root := testRepoRoot(t)
	hash, err := ToolsSourceHash(root)
	if err != nil {
		t.Skipf("cannot hash tools source from %s: %v", root, err)
	}
	if d := Guard(root, hash, bashInput("ls -la")); !d.Allowed {
		t.Errorf("safe command should be allowed when current: %q", d.Reason)
	}
	if d := Guard(root, hash, editInput(filepath.Join(root, "src/app.go"))); !d.Allowed {
		t.Errorf("project edit should be allowed when current: %q", d.Reason)
	}
	if d := Guard(root, hash, bashInput("rm -rf /")); d.Allowed {
		t.Error("dangerous command must be blocked even when current")
	}
}
