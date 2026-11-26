//go:build darwin

package surface

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/mpv"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/safego"
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

	renderWakeups sync.Map
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
			addr, _ := purego.Dlsym(gl, cString(name))
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

type DarwinSurface struct {
	view       objc.ID
	glCtx      objc.ID
	render     uintptr
	api        mpv.RenderAPI
	done       chan struct{}
	updates    chan struct{}
	lastBounds cocoaRect
	visible    bool
}

func New() VideoSurface { return &DarwinSurface{visible: true} }

func (s *DarwinSurface) OverlayMode() OverlayMode { return OverlaySameCanvas }

func (s *DarwinSurface) Attach(nsWindow uintptr, engine *mpv.Engine) error {
	if nsWindow == 0 || engine == nil || engine.Handle() == 0 {
		return errors.New("invalid window or mpv engine")
	}
	if err := initGLCallbacks(); err != nil {
		return err
	}
	content := objc.ID(nsWindow).Send(objc.RegisterName("contentView"))
	if content == 0 {
		return errors.New("nil NSWindow contentView")
	}

	glViewClassOnce.Do(func() {
		glViewClass, glViewClassErr = objc.RegisterClass(
			"EkkoVideoGLView",
			objc.GetClass("NSOpenGLView"),
			nil, nil,
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("isOpaque"), Fn: func(objc.ID, objc.SEL) bool { return true }},
			},
		)
	})
	if glViewClassErr != nil {
		return glViewClassErr
	}

	attrs := [...]uint32{99, 0x3200, 73, 5, 0}
	format := objc.ID(objc.GetClass("NSOpenGLPixelFormat")).
		Send(objc.RegisterName("alloc")).
		Send(objc.RegisterName("initWithAttributes:"), &attrs[0])
	if format == 0 {
		return errors.New("NSOpenGLPixelFormat init failed")
	}
	defer format.Send(objc.RegisterName("release"))

	bounds := objc.Send[cocoaRect](content, objc.RegisterName("bounds"))
	view := objc.ID(glViewClass).
		Send(objc.RegisterName("alloc")).
		Send(objc.RegisterName("initWithFrame:pixelFormat:"), bounds, format)
	if view == 0 {
		return errors.New("NSOpenGLView init failed")
	}
	view.Send(objc.RegisterName("setWantsBestResolutionOpenGLSurface:"), true)
	view.Send(objc.RegisterName("setAutoresizingMask:"), uintptr(2|16))

	subviews := content.Send(objc.RegisterName("subviews"))
	count := objc.Send[uint64](subviews, objc.RegisterName("count"))
	const nsWindowBelow = ^uintptr(0)
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
		return errors.New("nil openGLContext")
	}

	s.api = engine.RenderAPI()
	glCtx.Send(objc.RegisterName("makeCurrentContext"))
	glParams := mpv.GLInitParams{GetProcAddress: glGetProcPtr}
	apiType := [...]byte{'o', 'p', 'e', 'n', 'g', 'l', 0}
	params := [...]mpv.RenderParam{
		{Kind: 1, Data: unsafe.Pointer(&apiType[0])},
		{Kind: 2, Data: unsafe.Pointer(&glParams)},
		{},
	}
	var render uintptr
	rc := s.api.Create(&render, engine.Handle(), &params[0])
	objc.ID(objc.GetClass("NSOpenGLContext")).Send(objc.RegisterName("clearCurrentContext"))
	if rc < 0 || render == 0 {
		view.Send(objc.RegisterName("removeFromSuperview"))
		view.Send(objc.RegisterName("release"))
		return fmt.Errorf("mpv_render_context_create failed: %d", rc)
	}

	s.view = view
	s.glCtx = glCtx
	s.render = render
	s.done = make(chan struct{})
	s.updates = make(chan struct{}, 1)
	s.lastBounds = bounds

	renderWakeups.Store(render, s.updates)
	s.api.Callback(render, glWakeupPtr, render)
	safego.Go("darwin-gl-render", nil, s.renderLoop)
	return nil
}

func (s *DarwinSurface) SetBounds(r ui.Rect, winH float64) {
	if s == nil || s.view == 0 {
		return
	}
	cr := cocoaRect{
		Origin: cocoaPoint{X: float64(r.X), Y: winH - float64(r.Y) - float64(r.H)},
		Size:   cocoaSize{Width: max(1, float64(r.W)), Height: max(1, float64(r.H))},
	}
	if cr == s.lastBounds {
		return
	}
	s.lastBounds = cr
	s.view.Send(objc.RegisterName("setFrame:"), cr)
}

func (s *DarwinSurface) Show(visible bool) {
	if s == nil || s.view == 0 || s.visible == visible {
		return
	}
	s.visible = visible
	s.view.Send(objc.RegisterName("setHidden:"), !visible)
}

func (s *DarwinSurface) renderLoop() {
	var lastW, lastH int32
	for {
		select {
		case <-s.done:
			return
		case <-s.updates:
		}
		mygo.RunOnMain(func() {
			select {
			case <-s.done:
				return
			default:
			}
			if s.view == 0 || objc.Send[bool](s.view, objc.RegisterName("isHidden")) {
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
			if s.api.Update(s.render)&1 == 0 && !changed {
				return
			}
			if changed {
				s.glCtx.Send(objc.RegisterName("update"))
			}
			fbo := mpv.GLFramebuffer{Width: w, Height: h}
			flip := int32(1)
			params := [...]mpv.RenderParam{
				{Kind: 3, Data: unsafe.Pointer(&fbo)},
				{Kind: 4, Data: unsafe.Pointer(&flip)},
				{},
			}
			if s.api.Draw(s.render, &params[0]) >= 0 {
				s.glCtx.Send(objc.RegisterName("flushBuffer"))
				lastW, lastH = w, h
			}
		})
	}
}

func (s *DarwinSurface) Close() {
	if s == nil || s.done == nil {
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
			s.api.Callback(s.render, 0, 0)
			renderWakeups.Delete(s.render)
			s.glCtx.Send(objc.RegisterName("makeCurrentContext"))
			s.api.Free(s.render)
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

func cString(addr uintptr) string {
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
