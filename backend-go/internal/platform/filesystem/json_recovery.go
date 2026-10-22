package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupCorruptFile copies an unreadable JSON file to a sibling .corrupt file.
// The original is left in place so a human can inspect or restore it; callers
// can then fall back to an empty in-memory value without losing evidence.
func BackupCorruptFile(path string, cause error) (string, error) {
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", readErr
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	backup := strings.TrimSuffix(path, filepath.Ext(path)) + ".corrupt-" + stamp + filepath.Ext(path)
	body := append([]byte(fmt.Sprintf("// corrupt backup of %s: %v\n", filepath.Base(path), cause)), raw...)
	if err := AtomicWriteFile(backup, body, 0o644); err != nil {
		return "", err
	}
	return backup, nil
}
