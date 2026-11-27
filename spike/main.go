//go:build darwin

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var (
	urlFlag   = flag.String("url", "", "media URL or local file path (defaults to lavfi testsrc2)")
	rateFlag  = flag.Int("rate", 1000, "danmaku stress rate (msgs/min)")
	smokeFlag = flag.Duration("smoke", 0, "run for duration, print M0 metrics, and quit")
)

type spikeApp struct {
	win *mygo.Window

	api       *mpvAPI
	mpvPath   string
	mpvHandle uintptr
	gl        *glSurface

	mu       sync.RWMutex
	pos      float64
	dur      float64
	paused   bool
	volume   float64
	speed    float64
	hwdec    string
	mediaURL string
	status   string

	seeking    bool
	seekTarget float64

	sidebarOpen bool
	fullscreen  bool
	danmaku     *danmakuStress

	// Frame rate tracking
	fpsCount int
	fpsStart time.Time
	fps      float64
}

func newSpikeApp(rate int) *spikeApp {
	d := newDanmakuStress()
	d.ratePerMin = rate
	return &spikeApp{
		volume:      80,
		speed:       1.0,
		sidebarOpen: true,
		danmaku:     d,
		fpsStart:    time.Now(),
		status:      "initializing",
	}
}

func (a *spikeApp) initMPV(url string) {
	api, path, err := loadMPV()
	if err != nil {
		a.mu.Lock()
		a.status = err.Error()
		a.mu.Unlock()
		return
	}
	h := api.create()
	if h == 0 {
		a.mu.Lock()
		a.status = "mpv_create failed"
		a.mu.Unlock()
		return
	}
	setOpt := func(k, v string) {
		api.setOption(h, cstr(k), cstr(v))
	}
	setOpt("config", "no")
	setOpt("vo", "libmpv")
	setOpt("ao", "coreaudio")
	setOpt("hwdec", "auto-safe")
	setOpt("keep-open", "yes")
	setOpt("cache", "yes")
	setOpt("volume", "80")
	setOpt("video-timing-offset", "0")

	if rc := api.initialize(h); rc < 0 {
		api.terminate(h)
		a.mu.Lock()
		a.status = fmt.Sprintf("mpv_initialize failed: %d", rc)
		a.mu.Unlock()
		return
	}

	// Observe properties
	api.observeProperty(h, 1, cstr("time-pos"), mpvFormatDouble)
	api.observeProperty(h, 2, cstr("duration"), mpvFormatDouble)
	api.observeProperty(h, 3, cstr("pause"), mpvFormatFlag)
	api.observeProperty(h, 4, cstr("hwdec-current"), mpvFormatString)

	gl, err := newGLSurface(a.win.NativeHandle(), api, h)
	if err != nil {
		api.terminate(h)
		a.mu.Lock()
		a.status = fmt.Sprintf("glSurface failed: %v", err)
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	a.api = api
	a.mpvPath = path
	a.mpvHandle = h
	a.gl = gl
	a.status = "ready"
	a.mu.Unlock()

	go a.eventLoop()

	if url == "" {
		url = "av://lavfi:testsrc2=size=1280x720:rate=60"
	}
	a.loadURL(url)
}

func (a *spikeApp) loadURL(url string) {
	a.mu.Lock()
	a.mediaURL = url
	a.status = "loading: " + url
	a.mu.Unlock()
	a.cmdAsync("loadfile", url, "replace")
}

func (a *spikeApp) cmdAsync(args ...string) {
	a.mu.RLock()
	api := a.api
	h := a.mpvHandle
	a.mu.RUnlock()
	if api == nil || h == 0 {
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
	api.commandAsync(h, 0, &ptrs[0])
	runtime.KeepAlive(bufs)
	runtime.KeepAlive(ptrs)
}

func (a *spikeApp) setProp(name, val string) {
	a.mu.RLock()
	api := a.api
	h := a.mpvHandle
	a.mu.RUnlock()
	if api == nil || h == 0 {
		return
	}
	api.setOption(h, cstr(name), cstr(val))
}

func (a *spikeApp) eventLoop() {
	var lastUI time.Time
	for {
		a.mu.RLock()
		api := a.api
		h := a.mpvHandle
		a.mu.RUnlock()
		if api == nil || h == 0 {
			return
		}
		ev := api.waitEvent(h, 0.25)
		if ev == nil {
			continue
		}
		if ev.EventID == mpvEventShutdown {
			return
		}
		dirty := false
		switch ev.EventID {
		case mpvEventFileLoaded:
			a.mu.Lock()
			a.status = "playing"
			a.mu.Unlock()
			dirty = true
		case mpvEventPropertyChange:
			prop := (*mpvEventProperty)(ev.Data)
			if prop == nil || prop.Data == nil {
				continue
			}
			switch ev.ReplyUserdata {
			case 1: // time-pos
				if prop.Format == mpvFormatDouble {
					v := *(*float64)(prop.Data)
					a.mu.Lock()
					if !a.seeking {
						a.pos = v
					}
					a.mu.Unlock()
					dirty = true
				}
			case 2: // duration
				if prop.Format == mpvFormatDouble {
					v := *(*float64)(prop.Data)
					a.mu.Lock()
					a.dur = v
					a.mu.Unlock()
					dirty = true
				}
			case 3: // pause
				if prop.Format == mpvFormatFlag {
					v := *(*int32)(prop.Data) != 0
					a.mu.Lock()
					a.paused = v
					a.mu.Unlock()
					dirty = true
				}
			case 4: // hwdec-current
				if prop.Format == mpvFormatString {
					s := goStr(*(*uintptr)(prop.Data))
					a.mu.Lock()
					a.hwdec = s
					a.mu.Unlock()
					dirty = true
				}
			}
		}
		if dirty && time.Since(lastUI) >= 100*time.Millisecond {
			lastUI = time.Now()
			if a.win != nil && !a.win.IsDestroyed() {
				a.win.Update(func() {})
			}
		}
	}
}

func (a *spikeApp) shutdown() {
	a.mu.Lock()
	gl := a.gl
	api := a.api
	h := a.mpvHandle
	a.gl = nil
	a.mpvHandle = 0
	a.mu.Unlock()

	if gl != nil {
		gl.Close()
	}
	if api != nil && h != 0 {
		api.terminate(h)
	}
}

func (a *spikeApp) view(c *ui.Context) {
	now := c.Now()
	a.fpsCount++
	if dt := now.Sub(a.fpsStart); dt >= time.Second {
		a.fps = float64(a.fpsCount) / dt.Seconds()
		a.fpsCount = 0
		a.fpsStart = now
	}

	c.Root().Background(ui.Transparent)
	winW, winH := c.Size()

	a.mu.RLock()
	pos := a.pos
	dur := a.dur
	paused := a.paused
	hwdec := a.hwdec
	status := a.status
	gl := a.gl
	a.mu.RUnlock()

	var videoFrames uint64
	if gl != nil {
		videoFrames = gl.framesDrawn.Load()
	}

	sideW := float64(0)
	if a.sidebarOpen && !a.fullscreen {
		sideW = 200
	}
	if gl != nil {
		gl.SetBounds(sideW, 0, max(1, float64(winW)-sideW), float64(winH), float64(winH))
	}

	a.danmaku.tick(now, paused, max(1, winW-float32(sideW)))
	if a.danmaku.enabled && !paused {
		c.Invalidate()
	}

	ui.Row(c).Fill().Children(func() {
		if sideW > 0 {
			ui.Column(c).Width(float32(sideW)).FillHeight().
				Background(ui.Hex("#16181d")).Padding(44, 12, 12, 12).Gap(8).
				Children(func() {
					ui.Text(c, "Velan M0").FontSize(16).Bold().TextColor(ui.RGB(255, 255, 255))
					ui.Text(c, "macOS 同窗合成验证").FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.6))
					ui.Box(c).Height(1).FillWidth().Background(ui.RGBA(255, 255, 255, 0.1))

					if ui.Button(c.Key("open"), "打开本地视频…").FillWidth().Clicked() {
						go a.openFile()
					}
					if ui.Button(c.Key("lavfi"), "60fps 测试源").FillWidth().Clicked() {
						a.loadURL("av://lavfi:testsrc2=size=1280x720:rate=60")
					}
					if ui.Button(c.Key("toggle-dm"), fmt.Sprintf("弹幕压测: %v", a.danmaku.enabled)).FillWidth().Clicked() {
						a.danmaku.enabled = !a.danmaku.enabled
					}
					ui.Spacer(c)
					ui.Textf(c, "UI FPS: %.1f", a.fps).FontSize(11).TextColor(ui.Hex("#8be9fd")).FontFeatures("tnum")
					ui.Textf(c, "GL Frames: %d", videoFrames).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.7)).FontFeatures("tnum")
					ui.Textf(c, "Danmaku: %d active", len(a.danmaku.items)).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.7)).FontFeatures("tnum")
					ui.Textf(c, "Rate: %d/min", a.danmaku.ratePerMin).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.7)).FontFeatures("tnum")
					ui.Textf(c, "hwdec: %s", hwdec).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.7))
					ui.Text(c, status).FontSize(10).TextColor(ui.RGBA(255, 255, 255, 0.5))
				})
		}

		// Video overlay area (transparent background so NSOpenGLView shines through!)
		ui.Box(c).Grow(1).FillHeight().Background(ui.Transparent).Children(func() {
			// Danmaku canvas layer directly over video
			ui.Box(c).Fill().PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
				a.danmaku.draw(p, r, now)
			})

			// Top drag bar + HUD pill
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(40).
				Padding(0, 14).AlignItems(ui.Center).DragWindow().Children(func() {
				if ui.Button(c.Key("side-btn"), "侧栏").Clicked() {
					a.sidebarOpen = !a.sidebarOpen
				}
				ui.Spacer(c)
				ui.Row(c).Padding(4, 10).Radius(6).Background(ui.RGBA(0, 0, 0, 0.55)).Children(func() {
					ui.Textf(c, "UI %.0f FPS · 弹幕 %d 条 · hwdec:%s", a.fps, len(a.danmaku.items), hwdec).
						FontSize(11).TextColor(ui.RGB(255, 255, 255)).FontFeatures("tnum")
				})
			})

			// Bottom transport controls
			ui.Column(c).Absolute().Bottom(16).Left(20).Right(20).
				Padding(10, 14).Radius(10).Background(ui.RGBA(12, 14, 18, 0.78)).Gap(8).
				Children(func() {
					if dur > 0 {
						seekVal := pos
						if a.seeking {
							seekVal = a.seekTarget
						}
						sl := ui.Slider(c.Key("seek"), &seekVal, 0, dur).FillWidth()
						if sl.Changed() {
							a.seeking = true
							a.seekTarget = seekVal
						}
						if !sl.Pressed() && a.seeking {
							a.seeking = false
							a.cmdAsync("seek", fmt.Sprintf("%.2f", a.seekTarget), "absolute")
						}
					}
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						playLabel := "暂停"
						if paused {
							playLabel = "播放"
						}
						if ui.Button(c.Key("play"), playLabel).Clicked() {
							a.cmdAsync("cycle", "pause")
						}
						ui.Textf(c, "%s / %s", fmtClock(pos), fmtClock(dur)).
							FontSize(12).TextColor(ui.RGB(255, 255, 255)).FontFeatures("tnum")
						ui.Spacer(c)
						if ui.Button(c.Key("speed"), fmt.Sprintf("%.1fx", a.speed)).Clicked() {
							switch a.speed {
							case 1.0:
								a.speed = 1.5
							case 1.5:
								a.speed = 2.0
							default:
								a.speed = 1.0
							}
							a.setProp("speed", fmt.Sprintf("%.2f", a.speed))
						}
						if ui.Button(c.Key("dm"), "弹").Clicked() {
							a.danmaku.enabled = !a.danmaku.enabled
						}
						if ui.Button(c.Key("fs"), "全屏").Clicked() {
							a.fullscreen = !a.fullscreen
							a.win.SetFullScreen(a.fullscreen)
						}
					})
				})
		})
	})

	c.OnShortcut(0, ui.KeySpace, func() { a.cmdAsync("cycle", "pause") })
	c.OnShortcut(0, ui.KeyD, func() { a.danmaku.enabled = !a.danmaku.enabled })
	c.OnShortcut(0, ui.KeyF, func() {
		a.fullscreen = !a.fullscreen
		a.win.SetFullScreen(a.fullscreen)
	})
	c.OnShortcut(ui.Cmd, ui.KeyO, func() { go a.openFile() })
}

func (a *spikeApp) openFile() {
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.win,
		Title:  "选择视频文件",
		Filters: []mygo.FileFilter{
			{Name: "Video", Extensions: []string{"mp4", "mkv", "mov", "avi", "flv", "webm", "ts", "m3u8"}},
		},
	})
	if err == nil && len(files) > 0 {
		a.loadURL(files[0])
	}
}

func fmtClock(sec float64) string {
	s := int(sec)
	if s < 0 {
		s = 0
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func main() {
	flag.Parse()
	app := newSpikeApp(*rateFlag)

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		app.shutdown()
	})
	mygo.App.WhenReady(func() {
		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Velan M0 Spike",
			Width:           1120,
			Height:          700,
			MinWidth:        760,
			MinHeight:       480,
			TitleBarStyle:   mygo.TitleBarHidden,
			TitleBarHeight:  40,
			BackgroundColor: "#00000000",
			Content:         ui.View(app.view),
		})
		app.win.OnEnterFullScreen(func() {
			app.win.Update(func() { app.fullscreen = true })
		})
		app.win.OnLeaveFullScreen(func() {
			app.win.Update(func() { app.fullscreen = false })
		})
		app.initMPV(*urlFlag)
		if *smokeFlag > 0 {
			go func() {
				time.Sleep(*smokeFlag)
				app.mu.RLock()
				status := app.status
				hwdec := app.hwdec
				mpvPath := app.mpvPath
				var glFrames uint64
				if app.gl != nil {
					glFrames = app.gl.framesDrawn.Load()
				}
				app.mu.RUnlock()
				fmt.Printf("M0_SMOKE status=%q mpv=%q hwdec=%q gl_frames=%d ui_fps=%.1f danmaku_spawned=%d\n",
					status, mpvPath, hwdec, glFrames, app.fps, app.danmaku.totalSpawned)
				mygo.App.Quit()
			}()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
