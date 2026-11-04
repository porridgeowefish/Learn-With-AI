// The installer calls this only after confirmation. Match the installed image
// path, never a process name: portable instances belong to other installations.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func sameImage(actual, expected string) bool {
	return strings.EqualFold(filepath.Clean(actual), filepath.Clean(expected))
}

func stopInstalled(folder string) error {
	if !filepath.IsAbs(folder) {
		return errors.New("installation directory must be absolute")
	}
	expected := filepath.Join(folder, "learn-with-ai.exe")
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "learn-with-ai.exe") {
			continue
		}
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		if openErr != nil {
			continue
		}
		buffer := make([]uint16, 32768)
		size := uint32(len(buffer))
		queryErr := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size)
		if queryErr == nil && sameImage(windows.UTF16ToString(buffer[:size]), expected) {
			stopErr := stopProcess(handle, entry.ProcessID)
			windows.CloseHandle(handle)
			if stopErr != nil {
				return stopErr
			}
		} else {
			windows.CloseHandle(handle)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	return nil
}

func stopProcess(handle windows.Handle, pid uint32) error {
	user := windows.NewLazySystemDLL("user32.dll")
	callback := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		user.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			var result uintptr
			// Bounded messages also work when a window has been hidden to its tray.
			user.NewProc("SendMessageTimeoutW").Call(hwnd, 0x11, 0, 1, 2, 1000, uintptr(unsafe.Pointer(&result)))
			user.NewProc("SendMessageTimeoutW").Call(hwnd, 0x16, 1, 1, 2, 1000, uintptr(unsafe.Pointer(&result)))
		}
		return 1
	})
	user.NewProc("EnumWindows").Call(callback, 0)
	status, err := windows.WaitForSingleObject(handle, uint32((10*time.Second)/time.Millisecond))
	if err != nil {
		return err
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		// Older releases only hide their window. The held process handle prevents
		// PID reuse from terminating an unrelated process during this fallback.
		if err := windows.TerminateProcess(handle, 0); err != nil {
			return err
		}
		status, err = windows.WaitForSingleObject(handle, 5000)
	}
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("installed application did not stop: %d", status)
	}
	return nil
}

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}
	if err := stopInstalled(os.Args[1]); err != nil {
		os.Exit(1)
	}
}
