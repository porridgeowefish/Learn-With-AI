package filesystem

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes content to path atomically by writing to a sibling
// temp file then renaming. On Windows, os.Rename is atomic only when source
// and destination are on the same volume, which is why the temp file is a
// sibling (not in os.TempDir()).
//
// perm is the desired file mode for the final file. Temp file gets 0600.
func AtomicWriteFile(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("atomic write mkdir: %w", err)
	}
	var randBuf [8]byte
	if _, err := rand.Read(randBuf[:]); err != nil {
		return fmt.Errorf("atomic write rand: %w", err)
	}
	tmp := filepath.Join(dir, ".tmp-"+hex.EncodeToString(randBuf[:]))
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomic write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomic write rename: %w", err)
	}
	if perm != 0 {
		_ = os.Chmod(path, perm)
	}
	return nil
}
