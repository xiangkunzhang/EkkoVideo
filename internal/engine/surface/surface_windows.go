//go:build windows

package surface

import (
	"errors"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/engine/mpv"
	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procShowWindow       = user32.NewProc("ShowWindow")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
)

const (
	wsChild        = 0x40000000
	wsClipsiblings = 0x04000000
	wsClipchildren = 0x02000000
	wsDisabled     = 0x08000000

	swHide   = 0
	swShowNA = 8

	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010
	swpNoOwnerZOrder   = 0x0200
	blackBrushStockObj = 4
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

var (
	winClassOnce sync.Once
	winClassErr  error
	winClassPtr  *uint16
)

func ensureWinClass() error {
	winClassOnce.Do(func() {
		name, err := windows.UTF16PtrFromString("VelanVideoSurface")
		if err != nil {
			winClassErr = err
			return
		}
		hInst, _, _ := procGetModuleHandleW.Call(0)
		brush, _, _ := procGetStockObject.Call(blackBrushStockObj)
		wc := wndClassExW{
			cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			style:         0x0023, // CS_VREDRAW | CS_HREDRAW | CS_OWNDC
			lpfnWndProc:   syscall.NewCallback(defWndProc),
			hInstance:     hInst,
			hbrBackground: brush,
			lpszClassName: name,
		}
		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			winClassErr = callErr
			return
		}
		winClassPtr = name
	})
	return winClassErr
}

func defWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

type winSurface struct {
	parent uintptr
	child  uintptr
	engine *mpv.Engine
}

func New() VideoSurface { return &winSurface{} }

func (s *winSurface) OverlayMode() OverlayMode { return OverlayChildWindow }

func (s *winSurface) Attach(parentHWND uintptr, engine *mpv.Engine) error {
	if parentHWND == 0 || engine == nil {
		return errors.New("surface: invalid window or mpv engine")
	}
	if err := ensureWinClass(); err != nil {
		return err
	}
	hInst, _, _ := procGetModuleHandleW.Call(0)
	child, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(winClassPtr)),
		0,
		wsChild|wsClipsiblings|wsClipchildren|wsDisabled,
		0, 0, 1, 1,
		parentHWND,
		0,
		hInst,
		0,
	)
	if child == 0 {
		return callErr
	}
	s.parent = parentHWND
	s.child = child
	s.engine = engine
	engine.SetProperty("wid", strconv.FormatInt(int64(child), 10))
	return nil
}

func (s *winSurface) SetBounds(r ui.Rect, _ float64) {
	if s.child == 0 {
		return
	}
	scale := 1.0
	if s.parent != 0 {
		if err := procGetDpiForWindow.Find(); err == nil {
			dpi, _, _ := procGetDpiForWindow.Call(s.parent)
			if dpi > 0 {
				scale = float64(dpi) / 96.0
			}
		}
	}
	x := int32(float64(r.X) * scale)
	y := int32(float64(r.Y) * scale)
	w := max(1, int32(float64(r.W)*scale))
	h := max(1, int32(float64(r.H)*scale))
	procSetWindowPos.Call(s.child, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoZOrder|swpNoActivate|swpNoOwnerZOrder)
}

func (s *winSurface) Show(visible bool) {
	if s.child == 0 {
		return
	}
	cmd := uintptr(swHide)
	if visible {
		cmd = swShowNA
	}
	procShowWindow.Call(s.child, cmd)
}

func (s *winSurface) Close() {
	if s.child != 0 {
		procDestroyWindow.Call(s.child)
		s.child = 0
	}
}
