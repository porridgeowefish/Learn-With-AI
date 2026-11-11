// Package envname reads Learn-With-AI settings across the namespace migration.
package envname

import (
	"os"
	"strings"
)

func Value(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	if suffix, ok := strings.CutPrefix(name, "LWA_"); ok {
		return os.Getenv("LLL_" + suffix)
	}
	if suffix, ok := strings.CutPrefix(name, "LLL_"); ok {
		return os.Getenv("LWA_" + suffix)
	}
	return ""
}
