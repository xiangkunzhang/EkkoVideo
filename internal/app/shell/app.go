// Package shell wires the EkkoVideo main window, sidebar navigation, router,
// local playback lifecycle, and the supervised EkkoGateway child process.
package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/pages/home"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/pages/list"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/pages/settings"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/player"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/theme"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/mpv"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/surface"
	gwclient "github.com/xiangkunzhang/EkkoVideo/internal/gateway/client"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/logx"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/safego"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/store"
	"github.com/xiangkunzhang/EkkoVideo/internal/provider/local"
)

type App struct {
	version string
	win     *mygo.Window
	router  *ui.Router
	logger  *logx.Logger

	registry *core.Registry
	local    *local.Provider
	gateway  *gwclient.Supervisor
	gwStatus gwclient.Status

	settingsStore *store.Store[core.Settings]
	settings      core.Settings
	historyStore  *store.Store[[]core.WatchRecord]
	history       *core.History

	section     string
	sideShown   float32
	homeList    ui.ListState
	fileBrowser list.FileBrowserState

	// Open URL modal
	urlModalOpen bool
	urlInput     string
	urlField     ui.Handle

	// Active player
	mpv         *mpv.Engine
	surf        surface.VideoSurface
	playerState player.PlayerViewState
	vodProfile  *player.VODSingleProfile
	onPlayer    bool
}

// Run starts the EkkoVideo desktop GUI.
func Run(version string, initialFiles []string) {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		dataDir = "."
	}
	logDir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		logDir = dataDir
	}
	logger := logx.Open(filepath.Join(logDir, "ekkovideo.log"))
	defer logger.Close()

	a := NewApp(version, dataDir, logger)

	mygo.App.OnSecondInstance(func(args []string, _ string) {
		a.update(func() {
			if a.win != nil {
				a.win.Show()
				a.win.Focus()
			}
			for _, arg := range args {
				if !strings.HasPrefix(arg, "-") && local.IsVideoFile(arg) {
					a.playLocalPath(arg)
					break
				}
			}
		})
	})

	mygo.App.OnOpenFile(func(path string) {
		a.update(func() {
			a.playLocalPath(path)
		})
	})

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.shutdown()
	})

	mygo.App.WhenReady(func() {
		a.installMenu()
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "EkkoVideo",
			Width:           1180,
			Height:          740,
			MinWidth:        960,
			MinHeight:       600,
			TitleBarStyle:   mygo.TitleBarHidden,
			TitleBarHeight:  theme.TitleBarHeight,
			Vibrancy:        mygo.VibrancySidebar,
			BackgroundColor: "#00000000",
			StateKey:        "main",
			Content:         ui.View(a.view),
		})
		a.win.OnEnterFullScreen(func() {
			a.update(func() { a.playerState.Fullscreen = true })
		})
		a.win.OnLeaveFullScreen(func() {
			a.update(func() { a.playerState.Fullscreen = false })
		})

		a.startGateway()
		a.loadFolder(a.settings.LastFolder)

		for _, f := range initialFiles {
			if !strings.HasPrefix(f, "-") && local.IsVideoFile(f) {
				a.playLocalPath(f)
				break
			}
		}
	})

	if err := mygo.App.Run(); err != nil {
		logger.Printf("app run error: %v", err)
	}
}

// NewApp constructs an App with persisted stores and registered providers (also used in headless UI tests).
func NewApp(version, dataDir string, logger *logx.Logger) *App {
	cfgStore, cfg := store.Open(filepath.Join(dataDir, "settings.json"), core.DefaultSettings())
	histStore, recs := store.Open(filepath.Join(dataDir, "history.json"), []core.WatchRecord(nil))

	reg := core.NewRegistry()
	loc := local.New()
	reg.Register(loc)

	a := &App{
		version:       version,
		router:        ui.NewRouter("/home"),
		logger:        logger,
		registry:      reg,
		local:         loc,
		settingsStore: cfgStore,
		settings:      cfg,
		historyStore:  histStore,
		history:       core.NewHistory(recs),
		section:       "/home",
		sideShown:     1,
		vodProfile:    player.NewVODSingleProfile(),
	}
	a.initPlayerCallbacks()
	return a
}

func (a *App) update(fn func()) {
	if a.win == nil || a.win.IsDestroyed() {
		return
	}
	a.win.Update(fn)
}

func (a *App) startGateway() {
	a.gateway = gwclient.NewSupervisor("", nil, a.logger.Printf, func(st gwclient.Status) {
		a.update(func() {
			a.gwStatus = st
		})
	})
	safego.Go("gateway-start", a.logger.Printf, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := a.gateway.Start(ctx); err != nil {
			a.logger.Printf("gateway start failed: %v", err)
			a.update(func() {
				a.gwStatus = a.gateway.Status()
				a.gwStatus.LastError = err.Error()
			})
		}
	})
}

func (a *App) shutdown() {
	a.savePlaybackProgress()
	if a.surf != nil {
		a.surf.Close()
		a.surf = nil
	}
	if a.mpv != nil {
		a.mpv.Close()
		a.mpv = nil
	}
	if a.gateway != nil {
		a.gateway.Stop()
	}
	if a.historyStore != nil {
		a.historyStore.Close()
	}
	if a.settingsStore != nil {
		a.settingsStore.Close()
	}
}

func (a *App) installMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "文件", Submenu: []*mygo.MenuItem{
			{Label: "打开视频文件…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { a.openFileDialog() }},
			{Label: "打开文件夹…", Accelerator: "CmdOrCtrl+Shift+O", Click: func(*mygo.MenuItem, *mygo.Window) { a.openFolderDialog() }},
			{Label: "打开网络 URL…", Accelerator: "CmdOrCtrl+U", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.update(func() { a.urlModalOpen = true })
			}},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	}))
}

func (a *App) view(c *ui.Context) {
	t := c.Theme()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	path := a.router.Path()
	onPlayer := path == "/play"
	if a.onPlayer && !onPlayer {
		a.leavePlayerCleanup()
	}
	a.onPlayer = onPlayer
	if !onPlayer {
		a.section = path
	}

	row := ui.Row(c).Fill().AlignItems(ui.Stretch)
	targetSide := float32(1)
	if onPlayer {
		targetSide = 0
	}
	a.sideShown = row.Animate("sidebar", targetSide, 220*time.Millisecond)
	sideW := theme.SidebarWidth * a.sideShown

	row.Children(func() {
		if sideW > 0.5 {
			ui.Row(c).Width(sideW).Shrink(0).AlignItems(ui.Stretch).Clip().Children(func() {
				side := ui.Column(c).Width(theme.SidebarWidth).Shrink(0).Margin(0, 0, 0, sideW-theme.SidebarWidth)
				if !c.Vibrancy() {
					side.Background(t.Surface)
				}
				side.Children(func() { a.sidebar(c) })
			})
			ui.Box(c).Width(1).Background(theme.Hairline(t))
		}

		main := a.router.View(c, func(r *ui.Route) {
			switch {
			case r.Match("/home"):
				r.Title("首页")
				home.Page(c, a.history.Records, &a.homeList, home.Actions{
					OnOpenFile:   a.openFileDialog,
					OnOpenFolder: a.openFolderDialog,
					OnOpenURL:    func() { a.urlModalOpen = true },
					OnBrowse:     func() { a.router.Push("/local") },
					OnResume:     a.resumeWatchRecord,
				})
			case r.Match("/local"):
				r.Title("本地视频")
				list.FileBrowserPage(c, &a.fileBrowser, list.FileBrowserActions{
					OnOpenFileDialog:   a.openFileDialog,
					OnOpenFolderDialog: a.openFolderDialog,
					OnNavigateDir:      a.loadFolder,
					OnPlayFile:         a.playLocalItem,
				})
			case r.Match("/settings"):
				r.Title("设置")
				mpvPath, hwdec := "", ""
				if a.mpv != nil {
					mpvPath = a.mpv.LibraryPath()
					hwdec = a.mpv.Snapshot().Hwdec
				}
				settings.Page(c, &a.settings, settings.Diagnostics{
					AppVersion: a.version,
					MpvPath:    mpvPath,
					Hwdec:      hwdec,
					Gateway:    a.gwStatus,
				}, settings.Actions{
					OnSave: func() { a.settingsStore.Put(a.settings) },
					OnSimulateGWCrash: func() {
						if a.gateway != nil {
							a.gateway.KillChild()
						}
					},
				})
			case r.Match("/play"):
				r.Title("播放")
				player.Stage(c, &a.playerState, a.vodProfile, a.surf, sideW)
			}
		}).Grow(1).MinWidth(0)

		if onPlayer {
			main.Background(ui.Transparent)
		} else {
			main.Background(t.Background)
		}
	})

	a.openURLModal(c)

	c.OnShortcut(ui.Cmd, ui.KeyO, a.openFileDialog)
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyO, a.openFolderDialog)
	c.OnShortcut(ui.Cmd, ui.KeyU, func() { a.urlModalOpen = true })
	c.OnShortcut(ui.Cmd, ui.KeyComma, func() { a.router.Push("/settings") })
}

func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Height(theme.TitleBarHeight).Shrink(0).DragWindow()
	selected := a.section
	sb := ui.Sidebar(c, &selected, func() {
		ui.SidebarSection(c, "媒体", nil, func() {
			ui.SidebarItem(c, "/home", theme.IconHome, "首页")
			ui.SidebarItem(c, "/local", theme.IconFilm, "本地视频")
		})
		ui.SidebarSection(c, "系统", nil, func() {
			ui.SidebarItem(c, "/settings", theme.IconSettings, "设置与诊断")
		})
	}).Grow(1).Label("导航")
	if c.Vibrancy() {
		sb.Background(ui.Transparent)
	}
	if sb.Changed() {
		a.router.Push(selected)
	}
	// Gateway status pill at bottom of sidebar
	ui.Row(c).Padding(8, 12).AlignItems(ui.Center).Gap(6).Children(func() {
		dot := ui.Hex("#4caf50")
		txt := "网关就绪"
		if !a.gwStatus.Running {
			dot = ui.Hex("#ff9800")
			txt = "网关重连中"
		}
		ui.Box(c).Size(7, 7).Radius(4).Background(dot)
		ui.Text(c, txt).FontSize(11).TextColor(t.TextMuted)
	})
}

func (a *App) openURLModal(c *ui.Context) {
	if !a.urlModalOpen {
		return
	}
	t := c.Theme()
	ui.Box(c.Key("url-backdrop")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.45)).Center().Children(func() {
		ui.Column(c.Key("url-card")).Width(460).Padding(20).Radius(12).
			Background(t.Surface).Border(1, theme.Hairline(t)).Gap(12).Children(func() {
			ui.Text(c, "打开网络流 URL").FontSize(15).FontWeight(600)
			tf := ui.TextInput(c.Key("url-input"), &a.urlInput).
				Placeholder("http:// 或 https:// 视频地址 / m3u8").
				Bind(&a.urlField).FillWidth()
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("url-cancel"), "取消").Clicked() {
					a.urlModalOpen = false
				}
				if ui.PrimaryButton(c.Key("url-play"), "播放").Clicked() || tf.Submitted() {
					raw := strings.TrimSpace(a.urlInput)
					a.urlModalOpen = false
					if raw != "" {
						a.playURL(raw)
					}
				}
			})
		})
	})
}

func (a *App) loadFolder(dir string) {
	a.fileBrowser.Loading = true
	a.fileBrowser.Err = ""
	if dir != "" {
		a.fileBrowser.CurrentDir = dir
		a.settings.LastFolder = dir
		a.settingsStore.Put(a.settings)
	}
	safego.Go("load-folder", a.logger.Printf, func() {
		page, err := a.local.List(context.Background(), core.ItemRef{
			Source: local.SourceID,
			Kind:   core.KindFolder,
			ID:     dir,
		}, "")
		a.update(func() {
			a.fileBrowser.Loading = false
			if err != nil {
				a.fileBrowser.Err = err.Error()
				return
			}
			a.fileBrowser.Items = page.Items
			if a.fileBrowser.CurrentDir == "" {
				if homeDir, err := os.UserHomeDir(); err == nil {
					movies := filepath.Join(homeDir, "Movies")
					if st, err := os.Stat(movies); err == nil && st.IsDir() {
						a.fileBrowser.CurrentDir = movies
					} else {
						a.fileBrowser.CurrentDir = homeDir
					}
				}
			}
		})
	})
}

func (a *App) openFileDialog() {
	safego.Go("open-file-dialog", a.logger.Printf, func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win,
			Title:  "选择视频文件",
			Filters: []mygo.FileFilter{
				{Name: "视频文件", Extensions: []string{"mp4", "mkv", "mov", "avi", "flv", "webm", "ts", "m2ts", "m3u8"}},
			},
		})
		if err == nil && len(files) > 0 {
			a.update(func() { a.playLocalPath(files[0]) })
		}
	})
}

func (a *App) openFolderDialog() {
	safego.Go("open-folder-dialog", a.logger.Printf, func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    a.win,
			Title:     "选择视频文件夹",
			Directory: true,
		})
		if err == nil && len(dirs) > 0 {
			a.update(func() {
				a.loadFolder(dirs[0])
				a.router.Push("/local")
			})
		}
	})
}

func (a *App) playLocalItem(it core.Item) {
	a.playLocalPath(it.Ref.ID)
}

func (a *App) playLocalPath(path string) {
	q := a.local.BuildQueue(path)
	a.startQueue(q)
}

func (a *App) playURL(raw string) {
	item := core.Item{
		Ref:      core.ItemRef{Source: "url", Kind: core.KindFile, ID: raw},
		Title:    raw,
		Playable: true,
	}
	a.startQueue(core.NewQueue([]core.Item{item}, 0))
}

func (a *App) resumeWatchRecord(rec core.WatchRecord) {
	if rec.Ref.Source == local.SourceID {
		a.playLocalPath(rec.Ref.ID)
		return
	}
	if rec.Ref.Source == "url" {
		a.playURL(rec.Ref.ID)
	}
}

func (a *App) ensurePlayerReady() error {
	if a.mpv == nil {
		eng, err := mpv.NewEngine(a.settings.Hwdec, a.settings.Volume, a.logger.Printf, func(snap mpv.Snapshot) {
			a.update(func() { a.onMPVSnapshot(snap) })
		})
		if err != nil {
			return err
		}
		a.mpv = eng
	}
	if a.surf == nil && a.win != nil {
		s := surface.New()
		if err := s.Attach(a.win.NativeHandle(), a.mpv); err != nil {
			return err
		}
		a.surf = s
	}
	return nil
}

func (a *App) startQueue(q *core.Queue) {
	a.savePlaybackProgress()
	if a.playerState.Session == nil {
		a.playerState.Session = core.NewPlaybackSession()
	}
	gen := a.playerState.Session.BeginResolve(q)
	a.playerState.Touch(time.Now())
	a.router.Push("/play")

	item, ok := q.Active()
	if !ok {
		return
	}
	if err := a.ensurePlayerReady(); err != nil {
		a.playerState.Session.HandleFailure(err.Error())
		a.playerState.Session.Phase = core.PhaseFailed
		a.playerState.Session.LastError = err.Error()
		return
	}

	safego.Go("resolve-play", a.logger.Printf, func() {
		var src *core.PlaySource
		var err error
		if item.Ref.Source == local.SourceID {
			src, err = a.local.Resolve(context.Background(), item.Ref, core.ResolveOpts{})
		} else {
			src = &core.PlaySource{Ref: item.Ref, Title: item.Title, URL: item.Ref.ID, Kind: core.StreamMP4}
		}
		a.update(func() {
			if err != nil {
				a.playerState.Session.LastError = err.Error()
				a.playerState.Session.Phase = core.PhaseFailed
				return
			}
			src.StartAt = a.history.ResumePosition(item.Ref)
			if !a.playerState.Session.SetResolved(gen, src) {
				return
			}
			if loadErr := a.mpv.Load(src); loadErr != nil {
				a.playerState.Session.LastError = loadErr.Error()
				a.playerState.Session.Phase = core.PhaseFailed
			}
		})
	})
}

func (a *App) onMPVSnapshot(snap mpv.Snapshot) {
	a.playerState.Snap = snap
	sess := a.playerState.Session
	if sess == nil || !a.onPlayer {
		return
	}
	if snap.Error != "" {
		if retry, _ := sess.HandleFailure(snap.Error); retry && sess.Source != nil {
			_ = a.mpv.Load(sess.Source)
		}
		return
	}
	if snap.Loaded {
		sess.SetPlaying()
		sess.SetBuffering(snap.Buffering)
	}
	if snap.EOF && sess.Phase == core.PhasePlaying {
		sess.Phase = core.PhaseEnded
		a.savePlaybackProgress()
		if a.settings.AutoNext && sess.Queue != nil && sess.Queue.HasNext() {
			if _, ok := sess.Queue.Next(); ok {
				a.startQueue(sess.Queue)
			}
		}
	}
}

func (a *App) leavePlayerCleanup() {
	a.savePlaybackProgress()
	if a.mpv != nil {
		a.mpv.Command("stop")
	}
	if a.surf != nil {
		a.surf.Show(false)
	}
	a.playerState.DrawerOpen = false
	a.playerState.ActiveMenu = ""
}

func (a *App) savePlaybackProgress() {
	sess := a.playerState.Session
	if sess == nil || sess.Source == nil || !a.playerState.Snap.Loaded {
		return
	}
	a.history.Record(core.WatchRecord{
		Ref:      sess.Source.Ref,
		Title:    sess.Source.Title,
		Position: a.playerState.Snap.Position,
		Duration: a.playerState.Snap.Duration,
	})
	a.historyStore.Put(append([]core.WatchRecord(nil), a.history.Records...))
}

func (a *App) initPlayerCallbacks() {
	st := &a.playerState
	st.AspectRatio = a.settings.AspectRatio
	st.OnBack = func() {
		a.router.Back()
		if a.router.Path() == "/play" {
			a.router.Push("/home")
		}
	}
	st.OnTogglePlay = func() {
		if a.mpv != nil {
			a.mpv.Command("cycle", "pause")
		}
	}
	st.OnSeek = func(sec float64) {
		if a.mpv != nil {
			a.mpv.Command("seek", fmt.Sprintf("%.2f", sec), "absolute")
		}
	}
	st.OnSeekRel = func(delta float64) {
		if a.mpv != nil {
			a.mpv.Command("seek", fmt.Sprintf("%.2f", delta), "relative")
			st.ShowOSD(time.Now(), fmt.Sprintf("%+.0f 秒", delta))
		}
	}
	st.OnVolume = func(vol float64) {
		a.settings.Volume = vol
		a.settingsStore.Put(a.settings)
		if a.mpv != nil {
			a.mpv.SetProperty("volume", fmt.Sprintf("%.0f", vol))
			st.ShowOSD(time.Now(), fmt.Sprintf("音量 %.0f%%", vol))
		}
	}
	st.OnToggleMute = func() {
		if a.mpv != nil {
			a.mpv.Command("cycle", "mute")
		}
	}
	st.OnSpeed = func(speed float64) {
		if a.mpv != nil {
			a.mpv.SetProperty("speed", fmt.Sprintf("%.2f", speed))
			st.ShowOSD(time.Now(), fmt.Sprintf("倍速 %.2gx", speed))
		}
	}
	st.OnNext = func() {
		if st.Session != nil && st.Session.Queue != nil {
			if _, ok := st.Session.Queue.Next(); ok {
				a.startQueue(st.Session.Queue)
			}
		}
	}
	st.OnPrev = func() {
		if st.Session != nil && st.Session.Queue != nil {
			if _, ok := st.Session.Queue.Prev(); ok {
				a.startQueue(st.Session.Queue)
			}
		}
	}
	st.OnJumpQueue = func(idx int) {
		if st.Session != nil && st.Session.Queue != nil {
			if _, ok := st.Session.Queue.Jump(idx); ok {
				a.startQueue(st.Session.Queue)
			}
		}
	}
	st.OnSelectTrack = func(typ string, id int64) {
		if a.mpv == nil {
			return
		}
		prop := "sid"
		if typ == "audio" {
			prop = "aid"
		}
		if id == 0 {
			a.mpv.SetProperty(prop, "no")
		} else {
			a.mpv.SetProperty(prop, fmt.Sprintf("%d", id))
		}
	}
	st.OnCycleSub = func() {
		if a.mpv != nil {
			a.mpv.Command("cycle", "sub")
			st.ShowOSD(time.Now(), "切换字幕")
		}
	}
	st.OnCycleAudio = func() {
		if a.mpv != nil {
			a.mpv.Command("cycle", "audio")
			st.ShowOSD(time.Now(), "切换音轨")
		}
	}
	st.OnAspect = func(ratio string) {
		st.AspectRatio = ratio
		a.settings.AspectRatio = ratio
		a.settingsStore.Put(a.settings)
		if a.mpv == nil {
			return
		}
		switch ratio {
		case "fill":
			a.mpv.SetProperty("panscan", "1.0")
			a.mpv.SetProperty("video-aspect-override", "-1")
		case "16:9", "4:3":
			a.mpv.SetProperty("panscan", "0.0")
			a.mpv.SetProperty("video-aspect-override", ratio)
		default:
			a.mpv.SetProperty("panscan", "0.0")
			a.mpv.SetProperty("video-aspect-override", "-1")
		}
		st.ShowOSD(time.Now(), "画面比例: "+ratio)
	}
	st.OnScreenshot = func() {
		if a.mpv == nil {
			return
		}
		homeDir, _ := os.UserHomeDir()
		dir := filepath.Join(homeDir, "Pictures", "EkkoVideo")
		_ = os.MkdirAll(dir, 0o755)
		out := filepath.Join(dir, fmt.Sprintf("shot-%s.png", time.Now().Format("20060102-150405")))
		a.mpv.Command("screenshot-to-file", out, "video")
		st.ShowOSD(time.Now(), "已截图")
	}
	st.OnToggleFullscreen = func() {
		if a.win != nil {
			st.Fullscreen = !st.Fullscreen
			a.win.SetFullScreen(st.Fullscreen)
		}
	}
	st.OnTogglePin = func() {
		if a.win != nil {
			st.AlwaysOnTop = !st.AlwaysOnTop
			a.win.SetAlwaysOnTop(st.AlwaysOnTop)
			if st.AlwaysOnTop {
				st.ShowOSD(time.Now(), "已置顶")
			} else {
				st.ShowOSD(time.Now(), "取消置顶")
			}
		}
	}
	st.OnRetry = func() {
		if st.Session != nil && st.Session.Queue != nil {
			a.startQueue(st.Session.Queue)
		}
	}
}
