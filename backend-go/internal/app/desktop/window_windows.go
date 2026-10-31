//go:build windows

package desktop

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"github.com/xmz14/lll/backend-go/internal/platform/browser"
	"golang.org/x/sys/windows"
)

const AppID = "learn-with-ai.Desktop"

var user32 = windows.NewLazySystemDLL("user32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")
var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

func windowProperty(workspace string) *uint16 {
	sum := sha256.Sum256([]byte(filepath.Clean(workspace)))
	return windows.StringToUTF16Ptr(fmt.Sprintf("learn-with-ai.workspace.%x", sum))
}

// FocusExisting brings the running native window for workspace to the front.
// The running window records "<hwnd> <pid>" in the workspace; the record is
// trusted only while that window still exists and belongs to that process.
// Windows tagged with the legacy workspace property remain a fallback.
func FocusExisting(workspace string) bool {
	record := filepath.Join(workspace, windowRecordName)
	property := windowProperty(workspace)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(record); err == nil {
			var hwnd uintptr
			var pid uint32
			if _, err := fmt.Sscanf(string(data), "%d %d", &hwnd, &pid); err == nil && ownsWindow(hwnd, pid) {
				bringToFront(hwnd)
				return true
			}
		}
		var hwnd uintptr
		for {
			hwnd, _, _ = user32.NewProc("FindWindowExW").Call(0, hwnd, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("webview"))), 0)
			if hwnd == 0 {
				break
			}
			if tag, _, _ := user32.NewProc("GetPropW").Call(hwnd, uintptr(unsafe.Pointer(property))); tag != 0 {
				bringToFront(hwnd)
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func ownsWindow(hwnd uintptr, pid uint32) bool {
	if hwnd == 0 || pid == 0 {
		return false
	}
	if ok, _, _ := user32.NewProc("IsWindow").Call(hwnd); ok == 0 {
		return false
	}
	var owner uint32
	user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
	return owner == pid
}

func bringToFront(hwnd uintptr) {
	const swRestore, swShow = 9, 5
	if iconic, _, _ := user32.NewProc("IsIconic").Call(hwnd); iconic != 0 {
		user32.NewProc("ShowWindow").Call(hwnd, swRestore)
	} else {
		user32.NewProc("ShowWindow").Call(hwnd, swShow)
	}
	user32.NewProc("SetForegroundWindow").Call(hwnd)
}

func ShowError(message string) {
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(message))), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Learn-With-AI"))), 0x10)
}

// RunWindow owns the UI thread until close. Smoke mode proves a mounted,
// visible React application using the same WebView2 rendering path as users.
func RunWindow(engine *Engine, smokeResult string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3))
	shell32.NewProc("SetCurrentProcessExplicitAppUserModelID").Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(AppID))))
	dpi, _, _ := user32.NewProc("GetDpiForSystem").Call()
	if dpi < 96 {
		dpi = 96
	}
	scale := float64(dpi) / 96
	screenWidth, _, _ := user32.NewProc("GetSystemMetrics").Call(0)
	screenHeight, _, _ := user32.NewProc("GetSystemMetrics").Call(1)
	width, height := uint(1280*scale), uint(860*scale)
	if width > uint(screenWidth)-64 {
		width = uint(screenWidth) - 64
	}
	if height > uint(screenHeight)-64 {
		height = uint(screenHeight) - 64
	}
	view := webview.NewWithOptions(webview.WebViewOptions{AutoFocus: true, DataPath: filepath.Join(engine.Workspace, "webview2"), WindowOptions: webview.WindowOptions{Title: "Learn-With-AI", Width: width, Height: height, IconId: 1, Center: true}})
	if view == nil {
		return errors.New("Microsoft Edge WebView2 Runtime is required. Install the Evergreen Runtime from https://developer.microsoft.com/microsoft-edge/webview2/ and start Learn-With-AI again.")
	}
	defer view.Destroy()
	property := windowProperty(engine.Workspace)
	user32.NewProc("SetPropW").Call(uintptr(view.Window()), uintptr(unsafe.Pointer(property)), 1)
	defer user32.NewProc("RemovePropW").Call(uintptr(view.Window()), uintptr(unsafe.Pointer(property)))
	applyWindowIcon(uintptr(view.Window()))
	tray, err := attachWindowTray(uintptr(view.Window()), engine.Close)
	if err != nil {
		return err
	}
	defer tray.close()
	// Let a second launch find this window; removed when the window closes.
	record := filepath.Join(engine.Workspace, windowRecordName)
	if err := os.WriteFile(record, []byte(fmt.Sprintf("%d %d", uintptr(view.Window()), os.Getpid())), 0600); err == nil {
		defer os.Remove(record)
	}
	view.SetSize(min(int(900*scale), int(width)), min(int(600*scale), int(height)), webview.HintMin)
	if err := view.Bind("lllOpenExternal", func(raw string) error {
		if !AllowedExternalURL(raw) {
			return errors.New("unsupported external link")
		}
		return browser.Open(raw)
	}); err != nil {
		return err
	}
	if err := view.Bind("lllCaptureWindow", func() (windowCapture, error) {
		return captureWindowForPage(uintptr(view.Window()))
	}); err != nil {
		return err
	}
	// Untrusted links never receive a filesystem or arbitrary process bridge.
	origin, _ := json.Marshal(engine.URL)
	view.Init(fmt.Sprintf(`(()=>{const origin=%s;document.addEventListener('click',event=>{const a=event.target.closest?.('a[href]');if(!a)return;let u;try{u=new URL(a.href,location.href)}catch{return}if(u.origin!==origin){event.preventDefault();event.stopImmediatePropagation();if(u.protocol==='https:'||u.protocol==='http:')window.lllOpenExternal(u.href).catch(()=>{});}else if(a.target==='_blank'&&!a.download){event.preventDefault();location.href=u.href}},true);window.open=(url)=>{const u=new URL(url,location.href);if(u.origin===origin){location.href=u.href}else if(u.protocol==='https:'||u.protocol==='http:'){window.lllOpenExternal(u.href).catch(()=>{})}return null}})();`, origin))
	var smokeErr error
	var smokeOnce sync.Once
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	if smokeResult != "" {
		if err := view.Bind("lllSmokeReady", func(payload string) error {
			smokeOnce.Do(func() {
				go func() {
					time.Sleep(700 * time.Millisecond)
					view.Dispatch(func() {
						var evidence map[string]any
						smokeErr = json.Unmarshal([]byte(payload), &evidence)
						if smokeErr == nil && evidence["ok"] != true {
							smokeErr = fmt.Errorf("native desktop React rendering not ready: %s", payload)
						}
						if smokeErr == nil {
							hwnd := uintptr(view.Window())
							evidence["trayAdded"] = tray.added
							user32.NewProc("SendMessageW").Call(hwnd, 0x10, 0, 0)
							visible, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd)
							evidence["closeHidesWindow"] = visible == 0
							select {
							case <-engine.Done:
								evidence["engineAliveAfterClose"] = false
							default:
								evidence["engineAliveAfterClose"] = true
							}
							user32.NewProc("SendMessageW").Call(hwnd, trayMessage, 1, 0x203)
							visible, _, _ = user32.NewProc("IsWindowVisible").Call(hwnd)
							evidence["trayRestoresWindow"] = visible != 0
							evidence["hasWindowIcon"] = hasWindowIcon(uintptr(view.Window()))
							evidence["appUserModelID"] = AppID
							evidence["screenshot"] = smokeResult + ".png"
							smokeErr = captureWindow(uintptr(view.Window()), smokeResult+".png")
						}
						if smokeErr == nil {
							user32.NewProc("SendMessageW").Call(uintptr(view.Window()), 0x111, trayExit, 0)
							evidence["trayExitRequested"] = tray.exiting
							var data []byte
							data, smokeErr = json.Marshal(evidence)
							if smokeErr == nil {
								smokeErr = os.WriteFile(smokeResult, data, 0600)
							}
						}
						if smokeErr != nil {
							tray.exiting = true
							view.Destroy()
						}
					})
				}()
			})
			return smokeErr
		}); err != nil {
			return err
		}
		// Timers only: requestAnimationFrame is suspended while a window is occluded (CI desktops).
		view.Init(`window.addEventListener('DOMContentLoaded',()=>{const deadline=Date.now()+40000;const state=()=>{const nav=document.querySelector('aside[aria-label="应用导航"]');const root=document.getElementById('root');const main=root&&root.querySelector('main');return {nav:!!nav,navHeight:nav?nav.getBoundingClientRect().height:0,brand:!!root&&root.textContent.includes('Learn-With-AI'),heading:!!(main&&main.querySelector('h1')),mainHeight:main?main.getBoundingClientRect().height:0,visibility:document.visibilityState,textLength:root?root.textContent.length:0,title:document.title,url:location.href};};const check=()=>{const s=state();if(s.nav&&s.navHeight>0&&s.brand&&s.heading&&s.mainHeight>0){setTimeout(()=>window.lllCaptureWindow().then(c=>window.lllSmokeReady(JSON.stringify({ok:c.dataUrl?.startsWith('data:image/png;base64,')&&c.dataUrl.length>1000&&c.viewport?.width>0&&c.viewport?.height>0,...s,captureViewport:c.viewport}))).catch(e=>window.lllSmokeReady(JSON.stringify({ok:false,...s,captureError:String(e)}))),300);}else if(Date.now()<deadline){setTimeout(check,100);}else{window.lllSmokeReady(JSON.stringify({ok:false,...s}));}};check();});`)
		go func() {
			select {
			case <-timer.C:
				view.Dispatch(func() {
					smokeOnce.Do(func() { smokeErr = errors.New("native desktop React rendering timed out") })
					tray.exiting = true
					view.Destroy()
				})
			case <-engine.Done:
			}
		}()
	}
	go func() { <-engine.Done; view.Dispatch(func() { tray.exiting = true; view.Destroy() }) }()
	view.Navigate(engine.URL)
	view.Run()
	if smokeResult != "" {
		smokeOnce.Do(func() { smokeErr = errors.New("native window closed before React rendering was verified") })
		if _, err := os.Stat(smokeResult); err != nil && smokeErr == nil {
			smokeErr = errors.New("native window closed before smoke evidence was written")
		}
	}
	return smokeErr
}

// applyWindowIcon sets crisp title-bar and taskbar icons from the embedded
// group icon (resource ID 1) instead of relying on the scaled class icon.
func applyWindowIcon(hwnd uintptr) {
	module, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	if module == 0 {
		return
	}
	metrics := user32.NewProc("GetSystemMetrics")
	load := user32.NewProc("LoadImageW")
	send := user32.NewProc("SendMessageW")
	// WM_SETICON: ICON_SMALL uses SM_CXSMICON/SM_CYSMICON, ICON_BIG uses SM_CXICON/SM_CYICON.
	for _, icon := range []struct{ kind, width, height uintptr }{{0, 49, 50}, {1, 11, 12}} {
		width, _, _ := metrics.Call(icon.width)
		height, _, _ := metrics.Call(icon.height)
		handle, _, _ := load.Call(module, 1, 1, width, height, 0x8000)
		if handle != 0 {
			send.Call(hwnd, 0x80, icon.kind, handle)
		}
	}
}
