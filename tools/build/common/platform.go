package common

import (
	"os"
	"os/exec"
	"runtime"
)

// IsWindows reports whether the host OS is Windows.
func IsWindows() bool { return runtime.GOOS == "windows" }

// ExeSuffix is ".exe" on Windows, "" elsewhere.
func ExeSuffix() string {
	if IsWindows() {
		return ".exe"
	}
	return ""
}

// BinaryName appends the platform executable suffix to a tool name.
func BinaryName(name string) string { return name + ExeSuffix() }

// Which resolves a command in PATH, returning "" if it is not found.
func Which(cmd string) string {
	p, err := exec.LookPath(cmd)
	if err != nil {
		return ""
	}
	return p
}

// Exists reports whether a path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
