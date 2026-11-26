//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type mpvAPI struct {
	create          func() uintptr
	setOption       func(uintptr, *byte, *byte) int32
	initialize      func(uintptr) int32
	commandAsync    func(uintptr, uint64, **byte) int32
	observeProperty func(uintptr, uint64, *byte, int32) int32
	waitEvent       func(uintptr, float64) *mpvEvent
	getProperty     func(uintptr, *byte, int32, unsafe.Pointer) int32
	getPropertyStr  func(uintptr, *byte) uintptr
	free            func(uintptr)
	wakeup          func(uintptr)
	terminate       func(uintptr)
	errorString     func(int32) uintptr

	renderCreate   func(*uintptr, uintptr, *mpvRenderParam) int32
	renderCallback func(uintptr, uintptr, uintptr)
	renderUpdate   func(uintptr) uint64
	renderDraw     func(uintptr, *mpvRenderParam) int32
	renderFree     func(uintptr)
}

type mpvEvent struct {
	EventID       int32
	Error         int32
	ReplyUserdata uint64
	Data          unsafe.Pointer
}

type mpvEventProperty struct {
	Name   uintptr
	Format int32
	Data   unsafe.Pointer
}

type mpvRenderParam struct {
	kind int32
	data unsafe.Pointer
}

type mpvGLInitParams struct {
	getProcAddress uintptr
	context        uintptr
}

type mpvGLFramebuffer struct {
	id, width, height, format int32
}

const (
	mpvFormatString = 1
	mpvFormatFlag   = 3
	mpvFormatInt64  = 4
	mpvFormatDouble = 5

	mpvEventShutdown       = 1
	mpvEventFileLoaded     = 8
	mpvEventPropertyChange = 22
)

var (
	mpvOnce sync.Once
	mpvLib  *mpvAPI
	mpvErr  error
)

func findLibMPV() string {
	if p := os.Getenv("EKKO_LIBMPV"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "..", "Resources", "player", "libmpv.dylib"),
		filepath.Join(exeDir, "libmpv.dylib"),
		"/Applications/IINA.app/Contents/Frameworks/libmpv.2.dylib",
		"/opt/homebrew/opt/mpv/lib/libmpv.dylib",
		"/usr/local/opt/mpv/lib/libmpv.dylib",
		"/opt/homebrew/lib/libmpv.dylib",
		"/usr/local/lib/libmpv.dylib",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func loadMPV() (*mpvAPI, string, error) {
	path := findLibMPV()
	if path == "" {
		return nil, "", errors.New("libmpv.dylib not found (install via: brew install mpv)")
	}
	mpvOnce.Do(func() {
		h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			mpvErr = fmt.Errorf("dlopen %s: %w", path, err)
			return
		}
		api := &mpvAPI{}
		syms := []struct {
			name string
			fn   any
		}{
			{"mpv_create", &api.create},
			{"mpv_set_option_string", &api.setOption},
			{"mpv_initialize", &api.initialize},
			{"mpv_command_async", &api.commandAsync},
			{"mpv_observe_property", &api.observeProperty},
			{"mpv_wait_event", &api.waitEvent},
			{"mpv_get_property", &api.getProperty},
			{"mpv_get_property_string", &api.getPropertyStr},
			{"mpv_free", &api.free},
			{"mpv_wakeup", &api.wakeup},
			{"mpv_terminate_destroy", &api.terminate},
			{"mpv_error_string", &api.errorString},
			{"mpv_render_context_create", &api.renderCreate},
			{"mpv_render_context_set_update_callback", &api.renderCallback},
			{"mpv_render_context_update", &api.renderUpdate},
			{"mpv_render_context_render", &api.renderDraw},
			{"mpv_render_context_free", &api.renderFree},
		}
		for _, s := range syms {
			addr, err := purego.Dlsym(h, s.name)
			if err != nil {
				mpvErr = fmt.Errorf("dlsym %s: %w", s.name, err)
				return
			}
			purego.RegisterFunc(s.fn, addr)
		}
		mpvLib = api
	})
	return mpvLib, path, mpvErr
}

func cstr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

func goStr(addr uintptr) string {
	if addr == 0 {
		return ""
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}
