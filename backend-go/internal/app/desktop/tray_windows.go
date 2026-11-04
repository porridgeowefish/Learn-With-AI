//go:build windows

package desktop

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	trayMessage = 0x8001
	trayOpen    = 0x7101
	trayExit    = 0x7102
)

var comctl32 = windows.NewLazySystemDLL("comctl32.dll")

type notifyIconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

type windowTray struct {
	hwnd, callback uintptr
	icon           notifyIconData
	added, exiting bool
}

// attachWindowTray runs on the WebView UI thread. Only an explicit exit or
// engine shutdown lets WM_CLOSE reach WebView2's original window procedure.
func attachWindowTray(hwnd uintptr, exit func()) (*windowTray, error) {
	t := &windowTray{hwnd: hwnd}
	icon, _, _ := user32.NewProc("SendMessageW").Call(hwnd, 0x7f, 0, 0) // WM_GETICON
	if icon == 0 {
		icon, _, _ = user32.NewProc("LoadIconW").Call(0, 32512)
	}
	t.icon = notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Window: hwnd, ID: 1, Flags: 7, Callback: trayMessage, Icon: icon}
	copy(t.icon.Tip[:], windows.StringToUTF16("Learn-With-AI"))
	taskbarCreated, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	t.callback = windows.NewCallback(func(window, message, wparam, lparam, id, data uintptr) uintptr {
		switch message {
		case 0x11: // WM_QUERYENDSESSION: Restart Manager is asking, not committing.
			return 1
		case 0x16: // WM_ENDSESSION: stop the engine only if shutdown was confirmed.
			if wparam != 0 && !t.exiting {
				t.exiting = true
				go exit()
			}
			return 0
		case 0x10: // WM_CLOSE, including Alt+F4
			if !t.exiting && t.added {
				user32.NewProc("ShowWindow").Call(window, 0)
				return 0
			}
		case trayMessage:
			switch lparam {
			case 0x202, 0x203:
				bringToFront(window) // click / double click
			case 0x205, 0x7b:
				t.showMenu() // right click / context menu
			}
			return 0
		case 0x111: // WM_COMMAND
			switch wparam & 0xffff {
			case trayOpen:
				bringToFront(window)
				return 0
			case trayExit:
				if !t.exiting {
					t.exiting = true
					go exit()
				}
				return 0
			}
		case 0x82: // WM_NCDESTROY
			t.close()
		}
		if taskbarCreated != 0 && message == taskbarCreated {
			t.added = false
			if err := t.add(); err != nil {
				bringToFront(window)
			}
			return 0
		}
		result, _, _ := comctl32.NewProc("DefSubclassProc").Call(window, message, wparam, lparam)
		return result
	})
	ok, _, err := comctl32.NewProc("SetWindowSubclass").Call(hwnd, t.callback, 1, 0)
	if ok == 0 {
		return nil, fmt.Errorf("install tray window handler: %w", err)
	}
	if err := t.add(); err != nil {
		t.close()
		return nil, err
	}
	return t, nil
}

func (t *windowTray) add() error {
	ok, _, err := shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&t.icon)))
	if ok == 0 {
		return fmt.Errorf("add notification-area icon: %w", err)
	}
	t.added = true
	return nil
}

func (t *windowTray) close() {
	if t.added {
		shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&t.icon)))
		t.added = false
	}
	comctl32.NewProc("RemoveWindowSubclass").Call(t.hwnd, t.callback, 1)
}

func (t *windowTray) showMenu() {
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	user32.NewProc("AppendMenuW").Call(menu, 0, trayOpen, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("打开 Learn-With-AI"))))
	user32.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0)
	user32.NewProc("AppendMenuW").Call(menu, 0, trayExit, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("退出"))))
	var point struct{ X, Y int32 }
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
	user32.NewProc("SetForegroundWindow").Call(t.hwnd)
	command, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x100|0x2, uintptr(point.X), uintptr(point.Y), 0, t.hwnd, 0)
	user32.NewProc("PostMessageW").Call(t.hwnd, 0, 0, 0)
	if command != 0 {
		user32.NewProc("SendMessageW").Call(t.hwnd, 0x111, command, 0)
	}
}
