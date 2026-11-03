package filesystem

import "golang.org/x/sys/unix"

func PublishDirectory(from, to string) error { return unix.RenamexNp(from, to, unix.RENAME_EXCL) }
