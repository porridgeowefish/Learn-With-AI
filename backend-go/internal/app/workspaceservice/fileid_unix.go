//go:build linux || darwin

package workspaceservice

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func transferFileID(p string) (string, error) {
	var st unix.Stat_t
	if e := unix.Lstat(p, &st); e != nil {
		return "", e
	}
	return fmt.Sprintf("%x-%x", st.Dev, st.Ino), nil
}
