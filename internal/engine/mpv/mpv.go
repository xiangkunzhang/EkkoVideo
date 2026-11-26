// Package mpv wraps libmpv via purego without CGO.
package mpv

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/safego"
)

type Track struct {
	ID       int64
	Type     string // "audio" | "sub" | "video"
	Title    string
	Lang     string
	Selected bool
	External bool
}

type Snapshot struct {
	Loaded      bool
	Paused      bool
	Buffering   bool
	EOF         bool
	Muted       bool
	Position    float64
	Duration    float64
	Volume      float64
	Speed       float64
	VideoWidth  int64
	VideoHeight int64
	Hwdec       string
	Tracks      []Track
	Error       string
}

type RenderParam struct {
	Kind int32
	Data unsafe.Pointer
}

type GLInitParams struct {
	GetProcAddress uintptr
	Context        uintptr
}

type GLFramebuffer struct {
	ID, Width, Height, Format int32
}

type RenderAPI struct {
	Create   func(*uintptr, uintptr, *RenderParam) int32
	Callback func(uintptr, uintptr, uintptr)
	Update   func(uintptr) uint64
	Draw     func(uintptr, *RenderParam) int32
	Free     func(uintptr)
}

type rawAPI struct {
	create          func() uintptr
	setOption       func(uintptr, *byte, *byte) int32
	setPropertyStr  func(uintptr, *byte, *byte) int32
	initialize      func(uintptr) int32
	commandAsync    func(uintptr, uint64, **byte) int32
	observeProperty func(uintptr, uint64, *byte, int32) int32
	waitEvent       func(uintptr, float64) *mpvEvent
	getProperty     func(uintptr, *byte, int32, unsafe.Pointer) int32
	freeNode        func(*mpvNode)
	wakeup          func(uintptr)
	terminate       func(uintptr)
	errorString     func(int32) uintptr
	render          RenderAPI
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

type mpvEventEndFile struct {
	Reason int32
	Error  int32
}

type mpvNode struct {
	Value  unsafe.Pointer
	Format int32
	_      int32
}

type mpvNodeList struct {
	Num    int32
	_      int32
	Values unsafe.Pointer
	Keys   unsafe.Pointer
}

const (
	fmtString = 1
	fmtFlag   = 3
	fmtInt64  = 4
	fmtDouble = 5
	fmtNode   = 6
	fmtArray  = 7
	fmtMap    = 8

	evShutdown       = 1
	evEndFile        = 7
	evFileLoaded     = 8
	evPropertyChange = 22
)

var (
	loadOnce sync.Once
	loaded   *rawAPI
	loadErr  error
	libPath  string
)

func loadAPI() (*rawAPI, string, error) {
	loadOnce.Do(func() {
		p := FindLibrary()
		if p == "" {
			loadErr = errors.New("未找到 libmpv 动态库，请安装 mpv 或将其放入应用资源目录")
			return
		}
		libPath = p
		lookup, err := openLibrary(p)
		if err != nil {
			loadErr = fmt.Errorf("加载 libmpv (%s) 失败: %w", p, err)
			return
		}
		api := &rawAPI{}
		syms := []struct {
			name string
			fn   any
		}{
			{"mpv_create", &api.create},
			{"mpv_set_option_string", &api.setOption},
			{"mpv_set_property_string", &api.setPropertyStr},
			{"mpv_initialize", &api.initialize},
			{"mpv_command_async", &api.commandAsync},
			{"mpv_observe_property", &api.observeProperty},
			{"mpv_wait_event", &api.waitEvent},
			{"mpv_get_property", &api.getProperty},
			{"mpv_free_node_contents", &api.freeNode},
			{"mpv_wakeup", &api.wakeup},
			{"mpv_terminate_destroy", &api.terminate},
			{"mpv_error_string", &api.errorString},
			{"mpv_render_context_create", &api.render.Create},
			{"mpv_render_context_set_update_callback", &api.render.Callback},
			{"mpv_render_context_update", &api.render.Update},
			{"mpv_render_context_render", &api.render.Draw},
			{"mpv_render_context_free", &api.render.Free},
		}
		for _, s := range syms {
			addr, err := lookup(s.name)
			if err != nil {
				loadErr = fmt.Errorf("libmpv 缺少符号 %s: %w", s.name, err)
				return
			}
			purego.RegisterFunc(s.fn, addr)
		}
		loaded = api
	})
	return loaded, libPath, loadErr
}

// Engine manages one libmpv instance and its event loop.
type Engine struct {
	api      *rawAPI
	path     string
	handle   uintptr
	logf     func(string, ...any)
	onChange func(Snapshot)

	mu       sync.RWMutex
	snap     Snapshot
	closed   bool
	tracksMu bool
}

func NewEngine(hwdec string, volume float64, logf func(string, ...any), onChange func(Snapshot)) (*Engine, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	api, path, err := loadAPI()
	if err != nil {
		return nil, err
	}
	h := api.create()
	if h == 0 {
		return nil, errors.New("mpv_create failed")
	}
	setOpt := func(k, v string) {
		api.setOption(h, cstr(k), cstr(v))
	}
	setOpt("config", "no")
	setOpt("hwdec", hwdec)
	setOpt("audio-channels", "auto-safe")
	setOpt("keep-open", "yes")
	setOpt("cache", "yes")
	setOpt("volume", fmt.Sprintf("%.0f", volume))
	for _, kv := range platformOptions() {
		setOpt(kv[0], kv[1])
	}
	if rc := api.initialize(h); rc < 0 {
		api.terminate(h)
		return nil, fmt.Errorf("mpv_initialize failed (%d): %s", rc, goStr(api.errorString(rc)))
	}

	e := &Engine{
		api:      api,
		path:     path,
		handle:   h,
		logf:     logf,
		onChange: onChange,
		snap:     Snapshot{Volume: volume, Speed: 1.0},
	}

	props := []struct {
		id   uint64
		name string
		fmt  int32
	}{
		{1, "time-pos", fmtDouble},
		{2, "duration", fmtDouble},
		{3, "pause", fmtFlag},
		{4, "paused-for-cache", fmtFlag},
		{5, "eof-reached", fmtFlag},
		{6, "volume", fmtDouble},
		{7, "mute", fmtFlag},
		{8, "speed", fmtDouble},
		{9, "hwdec-current", fmtString},
		{10, "video-params/w", fmtInt64},
		{11, "video-params/h", fmtInt64},
		{12, "track-list", fmtNode},
	}
	for _, p := range props {
		api.observeProperty(h, p.id, cstr(p.name), p.fmt)
	}

	safego.Go("mpv-events", logf, e.eventLoop)
	return e, nil
}

func (e *Engine) Handle() uintptr      { return e.handle }
func (e *Engine) RenderAPI() RenderAPI { return e.api.render }
func (e *Engine) LibraryPath() string  { return e.path }

func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snap
}

func (e *Engine) Load(src *core.PlaySource) error {
	if src == nil || src.URL == "" {
		return errors.New("empty play source")
	}
	if len(src.Headers) > 0 {
		var fields []string
		for k, v := range src.Headers {
			if strings.EqualFold(k, "User-Agent") {
				e.SetProperty("user-agent", v)
				continue
			}
			fields = append(fields, k+": "+v)
		}
		if len(fields) > 0 {
			e.SetProperty("http-header-fields", strings.Join(fields, ","))
		}
	}
	if src.StartAt > 0 {
		e.SetProperty("start", fmt.Sprintf("%.2f", src.StartAt.Seconds()))
	} else {
		e.SetProperty("start", "none")
	}
	e.mu.Lock()
	e.snap.Loaded = false
	e.snap.EOF = false
	e.snap.Error = ""
	e.snap.Position = src.StartAt.Seconds()
	e.mu.Unlock()

	e.Command("loadfile", src.URL, "replace")
	for _, sub := range src.Subtitles {
		e.Command("sub-add", sub.URL, "auto", sub.Label, sub.Language)
	}
	return nil
}

func (e *Engine) Command(args ...string) {
	if e == nil || e.handle == 0 || len(args) == 0 {
		return
	}
	ptrs := make([]*byte, len(args)+1)
	bufs := make([][]byte, len(args))
	for i, s := range args {
		b := make([]byte, len(s)+1)
		copy(b, s)
		bufs[i] = b
		ptrs[i] = &b[0]
	}
	e.api.commandAsync(e.handle, 0, &ptrs[0])
	runtime.KeepAlive(bufs)
	runtime.KeepAlive(ptrs)
}

func (e *Engine) SetProperty(name, val string) {
	if e == nil || e.handle == 0 {
		return
	}
	e.api.setPropertyStr(e.handle, cstr(name), cstr(val))
}

func (e *Engine) Close() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	h := e.handle
	e.handle = 0
	e.mu.Unlock()

	if h != 0 {
		e.api.terminate(h)
	}
}

func (e *Engine) eventLoop() {
	var lastNotify time.Time
	for {
		e.mu.RLock()
		h := e.handle
		closed := e.closed
		e.mu.RUnlock()
		if closed || h == 0 {
			return
		}
		ev := e.api.waitEvent(h, 0.25)
		if ev == nil {
			continue
		}
		if ev.EventID == evShutdown {
			return
		}
		dirty := false
		urgent := false
		switch ev.EventID {
		case evFileLoaded:
			e.mu.Lock()
			e.snap.Loaded = true
			e.snap.EOF = false
			e.snap.Error = ""
			e.mu.Unlock()
			dirty, urgent = true, true
		case evEndFile:
			ef := (*mpvEventEndFile)(ev.Data)
			if ef != nil && ef.Error < 0 {
				e.mu.Lock()
				e.snap.Error = goStr(e.api.errorString(ef.Error))
				e.mu.Unlock()
				dirty, urgent = true, true
			}
		case evPropertyChange:
			prop := (*mpvEventProperty)(ev.Data)
			if prop == nil || prop.Data == nil {
				continue
			}
			e.applyProperty(ev.ReplyUserdata, prop)
			dirty = true
			if ev.ReplyUserdata >= 3 {
				urgent = true
			}
		}
		if dirty && (urgent || time.Since(lastNotify) >= 100*time.Millisecond) {
			lastNotify = time.Now()
			if e.onChange != nil {
				e.onChange(e.Snapshot())
			}
		}
	}
}

func (e *Engine) applyProperty(id uint64, prop *mpvEventProperty) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch id {
	case 1:
		if prop.Format == fmtDouble {
			e.snap.Position = *(*float64)(prop.Data)
		}
	case 2:
		if prop.Format == fmtDouble {
			e.snap.Duration = *(*float64)(prop.Data)
		}
	case 3:
		if prop.Format == fmtFlag {
			e.snap.Paused = *(*int32)(prop.Data) != 0
		}
	case 4:
		if prop.Format == fmtFlag {
			e.snap.Buffering = *(*int32)(prop.Data) != 0
		}
	case 5:
		if prop.Format == fmtFlag {
			e.snap.EOF = *(*int32)(prop.Data) != 0
		}
	case 6:
		if prop.Format == fmtDouble {
			e.snap.Volume = *(*float64)(prop.Data)
		}
	case 7:
		if prop.Format == fmtFlag {
			e.snap.Muted = *(*int32)(prop.Data) != 0
		}
	case 8:
		if prop.Format == fmtDouble {
			e.snap.Speed = *(*float64)(prop.Data)
		}
	case 9:
		if prop.Format == fmtString {
			e.snap.Hwdec = goStr(*(*uintptr)(prop.Data))
		}
	case 10:
		if prop.Format == fmtInt64 {
			e.snap.VideoWidth = *(*int64)(prop.Data)
		}
	case 11:
		if prop.Format == fmtInt64 {
			e.snap.VideoHeight = *(*int64)(prop.Data)
		}
	case 12:
		if prop.Format == fmtNode {
			node := (*mpvNode)(prop.Data)
			e.snap.Tracks = parseTrackList(node)
		}
	}
}

func parseTrackList(node *mpvNode) []Track {
	if node == nil || node.Format != fmtArray || node.Value == nil {
		return nil
	}
	list := (*mpvNodeList)(node.Value)
	if list.Num <= 0 || list.Values == nil {
		return nil
	}
	items := unsafe.Slice((*mpvNode)(list.Values), list.Num)
	out := make([]Track, 0, len(items))
	for i := range items {
		if items[i].Format != fmtMap || items[i].Value == nil {
			continue
		}
		m := (*mpvNodeList)(items[i].Value)
		if m.Num <= 0 || m.Values == nil || m.Keys == nil {
			continue
		}
		vals := unsafe.Slice((*mpvNode)(m.Values), m.Num)
		keys := unsafe.Slice((*uintptr)(m.Keys), m.Num)
		var tr Track
		for j := 0; j < int(m.Num); j++ {
			k := goStr(keys[j])
			v := vals[j]
			switch k {
			case "id":
				if v.Format == fmtInt64 {
					tr.ID = int64(uintptr(v.Value))
				}
			case "type":
				if v.Format == fmtString {
					tr.Type = goStr(uintptr(v.Value))
				}
			case "title":
				if v.Format == fmtString {
					tr.Title = goStr(uintptr(v.Value))
				}
			case "lang":
				if v.Format == fmtString {
					tr.Lang = goStr(uintptr(v.Value))
				}
			case "selected":
				if v.Format == fmtFlag {
					tr.Selected = uint32(uintptr(v.Value)) != 0
				}
			case "external":
				if v.Format == fmtFlag {
					tr.External = uint32(uintptr(v.Value)) != 0
				}
			}
		}
		if tr.Type == "audio" || tr.Type == "sub" {
			out = append(out, tr)
		}
	}
	return out
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
