package common

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// HookInput is the subset of the Claude Code PreToolUse payload the guard reads.
type HookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// GuardDecision is the guard's verdict: Allowed==true means proceed, otherwise
// Reason explains the denial (fed back to the agent).
type GuardDecision struct {
	Allowed bool
	Reason  string
}

// Guard decides whether a tool call may proceed. Two layers:
//
//  1. Dangerous commands (rm -rf, git reset --hard, force-push) are blocked in
//     every state — the "crazy things" that must never run unattended.
//  2. Staleness lockdown: when the tools are out of sync with their source, the
//     agent is funnelled toward recovery. Allowed are `./make` (rebuild),
//     running the already-built bin/ tools (the old binaries — safe, and each
//     self-checks via CheckStale), and editing the tool source (tools/build/ or
//     the make trampolines, to fix a broken build). Everything else is blocked
//     until `./make` succeeds. Read/search tools are not matched by the hook,
//     so the agent can still inspect the broken tools while locked down.
//
// The guard never gates itself via CheckStale: a stale guard must keep running
// to enforce the lockdown and permit recovery. It reads its own freshness with
// StaleReason and switches policy. The escape is always open — `./make` runs
// via 'go run' and works no matter how stale the compiled tools are — so this
// funnels the agent to recovery without ever trapping it.
func Guard(repoRoot, compiledHash string, in HookInput) GuardDecision {
	if in.ToolName == "Bash" {
		if reason := dangerReason(bashCommand(in.ToolInput)); reason != "" {
			return GuardDecision{Reason: reason}
		}
	}
	if StaleReason(repoRoot, compiledHash) == "" {
		return GuardDecision{Allowed: true} // tools current — nothing more to enforce
	}
	switch in.ToolName {
	case "Bash":
		cmd := bashCommand(in.ToolInput)
		if isRecoveryCommand(cmd) || isToolCommand(repoRoot, cmd) {
			return GuardDecision{Allowed: true}
		}
		return GuardDecision{Reason: lockMsg("only " + MakeCmd() + " and the existing bin/ tools may run")}
	case "Edit", "Write", "NotebookEdit":
		if isRecoveryPath(repoRoot, editPath(in.ToolInput)) {
			return GuardDecision{Allowed: true}
		}
		return GuardDecision{Reason: lockMsg("only edits under tools/build/ (to fix the tools) are allowed")}
	default:
		return GuardDecision{Reason: lockMsg("this action is blocked until the tools are rebuilt")}
	}
}

func lockMsg(detail string) string {
	return "tools are out of sync — " + detail + ". Run " + MakeCmd() + " to recover."
}

func bashCommand(raw json.RawMessage) string {
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &ti)
	return ti.Command
}

func editPath(raw json.RawMessage) string {
	var ti struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	_ = json.Unmarshal(raw, &ti)
	if ti.FilePath != "" {
		return ti.FilePath
	}
	return ti.NotebookPath
}

// hasShellControl reports whether a command contains chaining or redirection,
// which would let an allowed prefix smuggle in other commands.
func hasShellControl(cmd string) bool {
	return strings.ContainsAny(cmd, ";&|<>`\n") || strings.Contains(cmd, "$(")
}

// dangerReason blocks a small set of destructive commands in every state. This
// is a starter list — extend it for your project (mass deletes, force pushes,
// history rewrites, credential exfiltration, …).
func dangerReason(cmd string) string {
	c := strings.ToLower(cmd)
	switch {
	case strings.Contains(c, "rm -rf"), strings.Contains(c, "rm -fr"),
		strings.Contains(c, "rm -r -f"), strings.Contains(c, "rm -f -r"):
		return "blocked: recursive force-delete (rm -rf) is not allowed"
	case strings.Contains(c, "git reset --hard"):
		return "blocked: git reset --hard discards work and is not allowed"
	case strings.Contains(c, "git push") && (strings.Contains(c, "--force") || strings.Contains(c, " -f")):
		return "blocked: force-push is not allowed"
	}
	return ""
}

// isRecoveryCommand reports whether a Bash command is a bare ./make invocation —
// the one command that rebuilds the tools. The meta-builder runs via 'go run',
// so it works regardless of how stale the compiled tools are. Shell chaining or
// redirection disqualifies it, so 'make' cannot be a trojan for other commands.
func isRecoveryCommand(cmd string) bool {
	c := strings.TrimSpace(cmd)
	if c == "" || hasShellControl(c) {
		return false
	}
	fields := strings.Fields(c)
	tok := fields[0]
	if (tok == "bash" || tok == "sh") && len(fields) > 1 {
		tok = fields[1]
	}
	switch tok {
	case "./make", "make", ".\\make.cmd":
		return true
	}
	return strings.HasPrefix(c, "go run") && strings.Contains(c, "cmd/make")
}

// isToolCommand reports whether a Bash command invokes one of the already-built
// tools under bin/. Those stay runnable while stale — each self-checks via
// CheckStale and points at ./make if it is itself out of date — so the lockdown
// blocks novel work, not the use of the last-known-good tools.
func isToolCommand(repoRoot, cmd string) bool {
	c := strings.TrimSpace(cmd)
	if c == "" || hasShellControl(c) {
		return false
	}
	first := strings.Fields(c)[0]
	if strings.HasPrefix(strings.TrimPrefix(first, "./"), "bin/") {
		return true
	}
	binDir := filepath.Clean(filepath.Join(repoRoot, "bin")) + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(first), binDir)
}

// isRecoveryPath reports whether an Edit/Write target is part of the tooling a
// developer must change to fix a broken build: anything under tools/build/, or
// the make trampolines at the repo root.
func isRecoveryPath(repoRoot, path string) bool {
	if path == "" {
		return false
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(repoRoot, path)
	}
	abs = filepath.Clean(abs)
	toolsDir := filepath.Clean(filepath.Join(repoRoot, "tools", "build"))
	if abs == toolsDir || strings.HasPrefix(abs, toolsDir+string(filepath.Separator)) {
		return true
	}
	base := filepath.Base(abs)
	return (base == "make" || base == "make.cmd") && filepath.Dir(abs) == filepath.Clean(repoRoot)
}
