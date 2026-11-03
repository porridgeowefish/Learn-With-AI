package workspaceservice

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func transferFileID(p string) (string, error) {
	name, e := windows.UTF16PtrFromString(p)
	if e != nil {
		return "", e
	}
	h, e := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return "", e
	}
	defer windows.CloseHandle(h)
	var i windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(h, &i); e != nil {
		return "", e
	}
	return fmt.Sprintf("%x-%x-%x-%x", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow, i.CreationTime.Nanoseconds()), nil
}
