package common

import "strings"

// NormalizeArgs collapses long flags (--foo) to short form (-foo) so callers
// can accept either spelling. Values after = are preserved.
func NormalizeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.HasPrefix(a, "--") {
			out[i] = a[1:]
		} else {
			out[i] = a
		}
	}
	return out
}
