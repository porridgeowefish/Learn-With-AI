package filesystem

import (
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
	// A scanner can briefly hold a new file without FILE_SHARE_DELETE.
	// Every retry retains MoveFile's no-replace guarantee.
	deadline := time.Now().Add(time.Second)
	for {
		err = windows.MoveFile(oldName, newName)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || (err != windows.ERROR_ACCESS_DENIED && err != windows.ERROR_SHARING_VIOLATION) {
			return &os.LinkError{Op: "publish", Old: from, New: to, Err: err}
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
