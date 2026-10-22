package conversationstore

import (
	"errors"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"

	"os"
	"path/filepath"
	"unicode/utf8"
)

const MaxSystemPromptBytes = 32 * 1024

func (s *Store) SystemPrompt() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(s.root, "conversation", "system-prompt.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(data) > MaxSystemPromptBytes || !utf8.Valid(data) {
		return "", errors.New("invalid conversation instructions")
	}
	return string(data), nil
}

func (s *Store) SetSystemPrompt(value string) error {
	if len(value) > MaxSystemPromptBytes || !utf8.ValidString(value) {
		return errors.New("invalid conversation instructions: maximum 32768 UTF-8 bytes")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return filesystem.AtomicWriteFile(filepath.Join(s.root, "conversation", "system-prompt.txt"), []byte(value), 0o644)
}
