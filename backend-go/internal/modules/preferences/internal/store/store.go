// Package preferencestore owns the single workspace-global learner preference
// file. It is user-authored context: AI services may read a snapshot, but no AI
// execution path is allowed to write the canonical file.
package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Filename = "preferences.md"
const MaxBytes = 256 << 10

const defaultContent = `# 全局学习偏好

这份文件由你维护，LLL 的教师与助教只读。

可以记录你长期稳定的偏好，例如：

- 希望解释先给结论，再展开细节；
- 偏好的例子、语言、公式密度或代码风格；
- 希望教师如何提问、验证理解和安排练习。

不要在这里保存 API Key、密码或其他敏感信息。
`

type Snapshot struct {
	Path    string `json:"-"`
	Exists  bool   `json:"exists"`
	Content string `json:"-"`
}

func Path() string { return filepath.Join(filesystem.WORKSPACE, Filename) }

// Read returns one bounded snapshot without causing writes. This is the only
// operation available to AI-facing services.
func Read() (Snapshot, error) {
	snapshot, err := readFile(Path())
	if err != nil || snapshot.Exists {
		return snapshot, err
	}
	backup, err := backupPath()
	if err != nil {
		return Snapshot{}, err
	}
	recovered, err := readFile(backup)
	if err != nil || !recovered.Exists {
		return snapshot, err
	}
	recovered.Path = Path()
	return recovered, nil
}

// Ensure is reserved for the explicit learner-facing editor. Opening that
// surface initializes the documented template when no preference file exists.
func Ensure() (Snapshot, error) {
	snapshot, err := readFile(Path())
	if err != nil {
		return snapshot, err
	}
	if snapshot.Exists {
		if snapshot.Content == "" {
			recovered, ok, err := restoreNonEmptyBackup()
			if err != nil {
				return Snapshot{}, err
			}
			if ok {
				return recovered, nil
			}
		}
		if err := writeBackup(snapshot.Content); err != nil {
			return Snapshot{}, err
		}
		return snapshot, nil
	}
	backup, err := backupPath()
	if err != nil {
		return Snapshot{}, err
	}
	recovered, err := readFile(backup)
	if err != nil {
		return Snapshot{}, err
	}
	if recovered.Exists {
		if err := filesystem.AtomicWriteFile(Path(), []byte(recovered.Content), 0o644); err != nil {
			return Snapshot{}, fmt.Errorf("restore preferences backup: %w", err)
		}
		return Snapshot{Path: Path(), Exists: true, Content: recovered.Content}, nil
	}
	return Write(defaultContent)
}

// BackupExisting refreshes the recovery mirror when the canonical preference
// file exists. Startup calls this without creating a new preference file.
func BackupExisting() error {
	snapshot, err := readFile(Path())
	if err != nil || !snapshot.Exists {
		return err
	}
	if snapshot.Content == "" {
		_, recovered, err := restoreNonEmptyBackup()
		if err != nil || recovered {
			return err
		}
	}
	return writeBackup(snapshot.Content)
}

// Write is called only by the explicit learner-facing preferences endpoint.
func Write(content string) (Snapshot, error) {
	if err := validate(content); err != nil {
		return Snapshot{}, err
	}
	if err := writeBackup(content); err != nil {
		return Snapshot{}, err
	}
	if err := filesystem.AtomicWriteFile(Path(), []byte(content), 0o644); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Path: Path(), Exists: true, Content: content}, nil
}

func readFile(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Path: path}, nil
	} else if err != nil {
		return Snapshot{}, err
	}
	content := string(data)
	if err := validate(content); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Path: path, Exists: true, Content: content}, nil
}

func validate(content string) error {
	if len([]byte(content)) > MaxBytes {
		return errors.New("preferences file exceeds 256 KiB")
	}
	if strings.IndexByte(content, 0) >= 0 {
		return errors.New("preferences file contains NUL")
	}
	return nil
}

func writeBackup(content string) error {
	backup, err := backupPath()
	if err != nil {
		return err
	}
	if err := filesystem.AtomicWriteFile(backup, []byte(content), 0o600); err != nil {
		return fmt.Errorf("backup preferences: %w", err)
	}
	return nil
}

func restoreNonEmptyBackup() (Snapshot, bool, error) {
	backup, err := backupPath()
	if err != nil {
		return Snapshot{}, false, err
	}
	recovered, err := readFile(backup)
	if err != nil || !recovered.Exists || recovered.Content == "" {
		return Snapshot{}, false, err
	}
	if err := filesystem.AtomicWriteFile(Path(), []byte(recovered.Content), 0o644); err != nil {
		return Snapshot{}, false, fmt.Errorf("restore preferences backup: %w", err)
	}
	return Snapshot{Path: Path(), Exists: true, Content: recovered.Content}, true, nil
}

func backupPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve preferences backup directory: %w", err)
	}
	workspaceRoot, err := filepath.Abs(filesystem.WORKSPACE)
	if err != nil {
		return "", fmt.Errorf("resolve workspace for preferences backup: %w", err)
	}
	workspaceRoot = filepath.Clean(workspaceRoot)
	if runtime.GOOS == "windows" {
		workspaceRoot = strings.ToLower(workspaceRoot)
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(workspaceRoot)))[:16]
	return filepath.Join(configDir, "LifeLongLearn", "backups", id, Filename), nil
}
