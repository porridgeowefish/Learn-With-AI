package filesystem

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
)

func PublishDirectory(from, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return fmt.Errorf("无法在此位置执行禁止覆盖的重命名。请选择支持此操作的本地文件系统：%w", err)
	}
	return err
}
