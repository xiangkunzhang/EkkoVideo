//go:build darwin

package main

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

type cocoaPoint struct{ X, Y float64 }
type cocoaSize struct{ Width, Height float64 }
type cocoaRect struct {
	Origin cocoaPoint
	Size   cocoaSize
}

var (
	glViewClassOnce sync.Once
	glViewClass     objc.Class
	glViewClassErr  error

	renderWakeups sync.Map // render handle -> chan struct{}
	glProcOnce    sync.Once
	glGetProcPtr  uintptr
	glWakeupPtr   uintptr
)

func initGLCallbacks() error {
	var initErr error
	glProcOnce.Do(func() {
		gl, err := purego.Dlopen("/System/Library/Frameworks/OpenGL.framework/OpenGL", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			initErr = err
			return
		}
		glGetProcPtr = purego.NewCallback(func(_ uintptr, name uintptr) uintptr {
			addr, _ := purego.Dlsym(gl, goStr(name))
			return addr
		})
		glWakeupPtr = purego.NewCallback(func(ctx uintptr) {
			if ch, ok := renderWakeups.Load(ctx); ok {
				select {
				case ch.(chan struct{}) <- struct{}{}:
				default:
				}
			}
		})
	})
	return initErr
}

// glSurface hosts an NSOpenGLView positioned BELOW MyGo's Metal surface
// so MyGo UI (controls, sidebar, danmaku) composites directly on top.
type glSurface struct {
	view          objc.ID
	glCtx         objc.ID
	render        uintptr
	api           *mpvAPI
	done          chan struct{}
	updates       chan struct{}
	framesDrawn   atomic.Uint64
	lastBounds    cocoaRect
}

func newGLSurface(nsWindow uintptr, api *mpvAPI, mpvHandle uintptr) (*glSurface, error) {
	if nsWindow == 0 {
		return nil, errors.New("nil NSWindow handle")
	}
	if err := initGLCallbacks(); err != nil {
		return nil, err
	}

	content := objc.ID(nsWindow).Send(objc.RegisterName("contentView"))
	if content == 0 {
		return nil, errors.New("nil contentView")
	}

	glViewClassOnce.Do(func() {
		glViewClass, glViewClassErr = objc.RegisterClass(
			"EkkoSpikeGLView",
			objc.GetClass("NSOpenGLView"),
			nil, nil,
			[]objc.MethodDef{
				{
					Cmd: objc.RegisterName("isOpaque"),
					Fn:  func(objc.ID, objc.SEL) bool { return true },
				},
			},
		)
	})
	if glViewClassErr != nil {
		return nil, glViewClassErr
	}

	// NSOpenGLPFAOpenGLProfile (99) = 0x3200 (3.2 Core), NSOpenGLPFAAccelerated (73), NSOpenGLPFADoubleBuffer (5)
	attrs := [...]uint32{99, 0x3200, 73, 5, 0}
	format := objc.ID(objc.GetClass("NSOpenGLPixelFormat")).
		Send(objc.RegisterName("alloc")).
		Send(objc.RegisterName("initWithAttributes:"), &attrs[0])
	if format == 0 {
		return nil, errors.New("NSOpenGLPixelFormat init failed")
	}
	defer format.Send(objc.RegisterName("release"))

	bounds := objc.Send[cocoaRect](content, objc.RegisterName("bounds"))
	view := objc.ID(glViewClass).
		Send(objc.RegisterName("alloc")).
		Send(objc.RegisterName("initWithFrame:pixelFormat:"), bounds, format)
	if view == 0 {
		return nil, errors.New("NSOpenGLView init failed")
	}
	view.Send(objc.RegisterName("setWantsBestResolutionOpenGLSurface:"), true)
	view.Send(objc.RegisterName("setAutoresizingMask:"), uintptr(2|16)) // NSViewWidthSizable | NSViewHeightSizable

	// Position NSOpenGLView BELOW MyGo's top surface view (above any vibrancy effect view if present).
	// Find MyGo's topmost subview to place GL view right below it.
	subviews := content.Send(objc.RegisterName("subviews"))
	count := objc.Send[uint64](subviews, objc.RegisterName("count"))
	const nsWindowBelow = ^uintptr(0) // -1
	if count > 0 {
		topView := subviews.Send(objc.RegisterName("objectAtIndex:"), uintptr(count-1))
		content.Send(objc.RegisterName("addSubview:positioned:relativeTo:"), view, nsWindowBelow, topView)
	} else {
		content.Send(objc.RegisterName("addSubview:"), view)
	}

	glCtx := view.Send(objc.RegisterName("openGLContext"))
	if glCtx == 0 {
		view.Send(objc.RegisterName("removeFromSuperview"))
		view.Send(objc.RegisterName("release"))
		return nil, errors.New("nil openGLContext")
	}

	glCtx.Send(objc.RegisterName("makeCurrentContext"))
	glParams := mpvGLInitParams{getProcAddress: glGetProcPtr}
	apiType := [...]byte{'o', 'p', 'e', 'n', 'g', 'l', 0}
	params := [...]mpvRenderParam{
		{kind: 1, data: unsafe.Pointer(&apiType[0])},
		{kind: 2, data: unsafe.Pointer(&glParams)},
		{},
	}
	var render uintptr
	rc := api.renderCreate(&render, mpvHandle, &params[0])
	objc.ID(objc.GetClass("NSOpenGLContext")).Send(objc.RegisterName("clearCurrentContext"))
	if rc < 0 || render == 0 {
		view.Send(objc.RegisterName("removeFromSuperview"))
		view.Send(objc.RegisterName("release"))
		return nil, fmt.Errorf("mpv_render_context_create failed: %d", rc)
	}

	s := &glSurface{
		view:       view,
		glCtx:      glCtx,
		render:     render,
		api:        api,
		done:       make(chan struct{}),
		updates:    make(chan struct{}, 1),
		lastBounds: bounds,
	}
	renderWakeups.Store(render, s.updates)
	api.renderCallback(render, glWakeupPtr, render)
	go s.renderLoop()
	return s, nil
}

func (s *glSurface) SetBounds(x, y, w, h, winH float64) {
	if s == nil || s.view == 0 {
		return
	}
	// AppKit NSView origin is bottom-left.
	r := cocoaRect{
		Origin: cocoaPoint{X: x, Y: winH - y - h},
		Size:   cocoaSize{Width: max(1, w), Height: max(1, h)},
	}
	if r == s.lastBounds {
		return
	}
	s.lastBounds = r
	s.view.Send(objc.RegisterName("setFrame:"), r)
}

func (s *glSurface) renderLoop() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	var lastW, lastH int32
	for {
		select {
		case <-s.done:
			return
		case <-s.updates:
		case <-ticker.C:
		}
		mygo.RunOnMain(func() {
			select {
			case <-s.done:
				return
			default:
			}
			if objc.Send[bool](s.view, objc.RegisterName("isHidden")) {
				lastW, lastH = 0, 0
				return
			}
			bounds := objc.Send[cocoaRect](s.view, objc.RegisterName("bounds"))
			backing := objc.Send[cocoaRect](s.view, objc.RegisterName("convertRectToBacking:"), bounds)
			w := int32(math.Round(backing.Size.Width))
			h := int32(math.Round(backing.Size.Height))
			if w <= 0 || h <= 0 {
				return
			}
			s.glCtx.Send(objc.RegisterName("makeCurrentContext"))
			defer objc.ID(objc.GetClass("NSOpenGLContext")).Send(objc.RegisterName("clearCurrentContext"))

			changed := w != lastW || h != lastH
			if s.api.renderUpdate(s.render)&1 == 0 && !changed {
				return
			}
			if changed {
				s.glCtx.Send(objc.RegisterName("update"))
			}
			fbo := mpvGLFramebuffer{width: w, height: h}
			flip := int32(1)
			params := [...]mpvRenderParam{
				{kind: 3, data: unsafe.Pointer(&fbo)},
				{kind: 4, data: unsafe.Pointer(&flip)},
				{},
			}
			if s.api.renderDraw(s.render, &params[0]) >= 0 {
				s.glCtx.Send(objc.RegisterName("flushBuffer"))
				lastW, lastH = w, h
				s.framesDrawn.Add(1)
			}
		})
	}
}

func (s *glSurface) Close() {
	if s == nil {
		return
	}
	select {
	case <-s.done:
		return
	default:
		close(s.done)
	}
	mygo.RunOnMain(func() {
		if s.render != 0 {
			s.api.renderCallback(s.render, 0, 0)
			renderWakeups.Delete(s.render)
			s.glCtx.Send(objc.RegisterName("makeCurrentContext"))
			s.api.renderFree(s.render)
			objc.ID(objc.GetClass("NSOpenGLContext")).Send(objc.RegisterName("clearCurrentContext"))
			s.render = 0
		}
		if s.view != 0 {
			s.view.Send(objc.RegisterName("removeFromSuperview"))
			s.view.Send(objc.RegisterName("release"))
			s.view = 0
		}
	})
}
