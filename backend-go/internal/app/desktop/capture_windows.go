//go:build windows

package desktop

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"math"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type nativeRect struct{ Left, Top, Right, Bottom int32 }
type bitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
	Color                  uint32
}

// captureWindowImage reads only this application's visible window pixels.
func captureWindowImage(hwnd uintptr) (*image.RGBA, nativeRect, error) {
	var rect nativeRect
	ok, _, _ := user32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ok == 0 {
		return nil, rect, errors.New("cannot measure native window")
	}
	width, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 || width*height > 32_000_000 {
		return nil, rect, errors.New("invalid native window capture size")
	}
	// ReleaseDC must run on the thread that acquired the DC. A bound WebView
	// callback can be scheduled on a different OS thread between Win32 calls.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	screen, _, _ := user32.NewProc("GetDC").Call(0)
	if screen == 0 {
		return nil, rect, errors.New("cannot acquire screen device context")
	}
	defer user32.NewProc("ReleaseDC").Call(0, screen)
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	dc, _, _ := gdi.NewProc("CreateCompatibleDC").Call(screen)
	if dc == 0 {
		return nil, rect, errors.New("cannot allocate native capture context")
	}
	defer gdi.NewProc("DeleteDC").Call(dc)
	bitmap, _, _ := gdi.NewProc("CreateCompatibleBitmap").Call(screen, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return nil, rect, errors.New("cannot allocate native capture bitmap")
	}
	defer gdi.NewProc("DeleteObject").Call(bitmap)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, bitmap)
	if old == 0 || old == ^uintptr(0) {
		return nil, rect, errors.New("cannot select native capture bitmap")
	}
	ok, _, _ = user32.NewProc("PrintWindow").Call(hwnd, dc, 2)
	gdi.NewProc("SelectObject").Call(dc, old)
	if ok == 0 {
		return nil, rect, errors.New("cannot copy native window pixels")
	}
	pixels := make([]byte, width*height*4)
	info := bitmapInfo{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	rows, _, _ := gdi.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), 0)
	if rows != uintptr(height) {
		return nil, rect, errors.New("cannot read native window pixels")
	}
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+2] = pixels[i+2], pixels[i]
		pixels[i+3] = 255
	}
	return &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}, rect, nil
}

func captureWindow(hwnd uintptr, path string) error {
	frame, _, err := captureWindowImage(hwnd)
	if err != nil {
		return err
	}
	output, err := os.Create(path)
	if err != nil {
		return err
	}
	defer output.Close()
	return png.Encode(output, frame)
}

type captureViewport struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type windowCapture struct {
	DataURL string `json:"dataUrl"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}
type nativePoint struct{ X, Y int32 }

type captureRequest struct {
	ID             uint64  `json:"id"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	ViewportWidth  float64 `json:"viewportWidth"`
	ViewportHeight float64 `json:"viewportHeight"`
}

func captureRegionForPage(hwnd uintptr, request captureRequest) (windowCapture, error) {
	values := []float64{request.X, request.Y, request.Width, request.Height, request.ViewportWidth, request.ViewportHeight}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return windowCapture{}, errors.New("invalid capture selection")
		}
	}
	if request.ViewportWidth <= 0 || request.ViewportHeight <= 0 || request.Width < 12 || request.Height < 12 ||
		request.X < 0 || request.Y < 0 || request.X+request.Width > request.ViewportWidth+1 || request.Y+request.Height > request.ViewportHeight+1 {
		return windowCapture{}, errors.New("invalid capture selection")
	}
	frame, outer, err := captureWindowImage(hwnd)
	if err != nil {
		return windowCapture{}, err
	}
	var client nativeRect
	ok, _, _ := user32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&client)))
	if ok == 0 {
		return windowCapture{}, errors.New("cannot measure window client area")
	}
	origin := nativePoint{}
	ok, _, _ = user32.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&origin)))
	if ok == 0 {
		return windowCapture{}, errors.New("cannot locate window client area")
	}
	viewport := captureViewport{X: int(origin.X - outer.Left), Y: int(origin.Y - outer.Top), Width: int(client.Right - client.Left), Height: int(client.Bottom - client.Top)}
	if viewport.X < 0 || viewport.Y < 0 || viewport.Width <= 0 || viewport.Height <= 0 || viewport.X+viewport.Width > frame.Rect.Dx() || viewport.Y+viewport.Height > frame.Rect.Dy() {
		return windowCapture{}, errors.New("window client area falls outside screenshot")
	}
	left := viewport.X + int(math.Round(request.X*float64(viewport.Width)/request.ViewportWidth))
	top := viewport.Y + int(math.Round(request.Y*float64(viewport.Height)/request.ViewportHeight))
	right := viewport.X + int(math.Round((request.X+request.Width)*float64(viewport.Width)/request.ViewportWidth))
	bottom := viewport.Y + int(math.Round((request.Y+request.Height)*float64(viewport.Height)/request.ViewportHeight))
	selection := image.Rect(left, top, right, bottom).Intersect(image.Rect(viewport.X, viewport.Y, viewport.X+viewport.Width, viewport.Y+viewport.Height))
	if selection.Dx() < 1 || selection.Dy() < 1 {
		return windowCapture{}, errors.New("capture selection is outside the app")
	}
	var data bytes.Buffer
	if err := png.Encode(&data, frame.SubImage(selection)); err != nil {
		return windowCapture{}, err
	}
	return windowCapture{DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes()), Width: selection.Dx(), Height: selection.Dy()}, nil
}

func hasWindowIcon(hwnd uintptr) bool {
	icon, _, _ := user32.NewProc("SendMessageW").Call(hwnd, 0x7f, 1, 0)
	if icon == 0 {
		icon, _, _ = user32.NewProc("GetClassLongPtrW").Call(hwnd, ^uintptr(13))
	}
	return icon != 0
}
