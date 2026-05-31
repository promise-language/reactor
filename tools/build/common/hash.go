package common

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ToolsSourceHash computes an FNV-128a hash over every .go/go.mod/go.sum file
// under <repoRoot>/tools/build. It is stable across runs and platforms, and is
// what the meta-builder bakes into each binary to drive the staleness check.
// The per-file size delimiter prevents file-boundary collisions.
func ToolsSourceHash(repoRoot string) (string, error) {
	base := filepath.Join(repoRoot, "tools", "build")
	var files []string
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)

	h := fnv.New128a()
	for _, path := range files {
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%d\n", filepath.ToSlash(rel), len(data))
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
