package workspaceservice

import (
	"fmt"
	"strconv"
	"strings"

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
	return fmt.Sprintf("%x-%x-%x", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow), nil
}

func matchesTransferFileID(id, recorded string) bool {
	if id == "" || id == recorded {
		return id != ""
	}
	// NTFS can give a renamed file the previous file's creation time when
	// the destination name is reused. Older journals appended that time.
	legacyTime, ok := strings.CutPrefix(recorded, id+"-")
	if !ok {
		return false
	}
	_, err := strconv.ParseInt(legacyTime, 16, 64)
	return err == nil
}
