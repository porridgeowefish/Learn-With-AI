package filesystem

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func PublishDirectory(from, to string) error {
	oldName, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	newName, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	err = retrySharingViolation(func() error { return windows.MoveFile(oldName, newName) })
	if err != nil {
		return &os.LinkError{Op: "publish", Old: from, New: to, Err: err}
	}
	return nil
}

func replaceFile(from, to string) error {
	return retrySharingViolation(func() error { return os.Rename(from, to) })
}

// A scanner can briefly hold a file without FILE_SHARE_DELETE.
func retrySharingViolation(move func() error) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := move()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || (!errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION)) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
func AvailableBytes(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	err = windows.GetDiskFreeSpaceEx(p, &free, nil, nil)
	return free, err
}
