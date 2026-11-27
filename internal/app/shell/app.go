// Package shell wires the Velan main window, sidebar navigation, router,
// local & online playback lifecycle, and the supervised VelanGateway child process.
package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/pages/detail"
	"github.com/xiangkunzhang/Velan/internal/app/pages/home"
	"github.com/xiangkunzhang/Velan/internal/app/pages/list"
	"github.com/xiangkunzhang/Velan/internal/app/pages/settings"
	"github.com/xiangkunzhang/Velan/internal/app/player"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/danmaku"
	"github.com/xiangkunzhang/Velan/internal/engine/mpv"
	"github.com/xiangkunzhang/Velan/internal/engine/surface"
	gwclient "github.com/xiangkunzhang/Velan/internal/gateway/client"
	"github.com/xiangkunzhang/Velan/internal/infra/imageloader"
	"github.com/xiangkunzhang/Velan/internal/infra/logx"
	"github.com/xiangkunzhang/Velan/internal/infra/mediakeys"
	"github.com/xiangkunzhang/Velan/internal/infra/safego"
	"github.com/xiangkunzhang/Velan/internal/infra/store"
	"github.com/xiangkunzhang/Velan/internal/provider/iptv"
	"github.com/xiangkunzhang/Velan/internal/provider/local"
)

type App struct {
	version string
	dataDir string
	win     *mygo.Window
	router  *ui.Router
	logger  *logx.Logger

	registry *core.Registry
	local    *local.Provider
	iptvMgr  *iptv.Manager
	gateway  *gwclient.Supervisor
	gwStatus gwclient.Status

	settingsStore  *store.Store[core.Settings]
	settings       core.Settings
	historyStore   *store.Store[[]core.WatchRecord]
	history        *core.History
	favoritesStore *store.Store[[]core.FavoriteItem]
	favorites      *core.Favorites
	sourcesStore   *store.Store[[]iptv.Subscription]
	img            *imageloader.Loader

	section     string
	sideShown   float32
	homeList    ui.ListState
	fileBrowser list.FileBrowserState
	iptvList    list.ChannelListState

	// Online platforms & poster grids
	onlinePlatforms []core.ProviderInfo
	posterGrids     map[string]*list.PosterGridState
	favGrid         list.PosterGridState
	detailState     detail.State
	detailCancel    context.CancelFunc

	// Open URL / IPTV subscription modals
	urlModalOpen  bool
	urlInput      string
	urlField      ui.Handle
	iptvModalOpen bool
	iptvSubName   string
	iptvSubURL    string
	iptvSubEPG    string

	// Active player
	mpv             *mpv.Engine
	surf            surface.VideoSurface
	playerState     player.PlayerViewState
	vodProfile      *player.VODSingleProfile
	seriesProfile   *player.VODSeriesProfile
	iptvProfile     *player.IPTVProfile
	iptvPrevChannel string
	iptvCurChannel  string
	onPlayer        bool
}

// Run starts the Velan desktop GUI.
func Run(version string, initialFiles []string) {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		dataDir = "."
	}
	cacheDir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		cacheDir = filepath.Join(dataDir, "cache")
	}
	logDir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		logDir = dataDir
	}
	logger := logx.Open(filepath.Join(logDir, "velan.log"))
	defer logger.Close()

	a := NewApp(version, dataDir, cacheDir, logger)

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
			Title:           "Velan",
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
		if len(a.iptvMgr.Subscriptions()) > 0 {
			a.refreshIPTV()
		}

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
func NewApp(version, dataDir, cacheDir string, logger *logx.Logger) *App {
	cfgStore, cfg := store.Open(filepath.Join(dataDir, "settings.json"), core.DefaultSettings())
	histStore, recs := store.Open(filepath.Join(dataDir, "history.json"), []core.WatchRecord(nil))
	favStore, favs := store.Open(filepath.Join(dataDir, "favorites.json"), []core.FavoriteItem(nil))
	srcStore, subs := store.Open(filepath.Join(dataDir, "sources.json"), []iptv.Subscription(nil))

	reg := core.NewRegistry()
	loc := local.New()
	iptvMgr := iptv.NewManager(filepath.Join(cacheDir, "epg"), subs)
	reg.Register(loc)
	reg.Register(iptvMgr)

	a := &App{
		version:        version,
		dataDir:        dataDir,
		router:         ui.NewRouter("/home"),
		logger:         logger,
		registry:       reg,
		local:          loc,
		iptvMgr:        iptvMgr,
		settingsStore:  cfgStore,
		settings:       cfg,
		historyStore:   histStore,
		history:        core.NewHistory(recs),
		favoritesStore: favStore,
		favorites:      core.NewFavorites(favs),
		sourcesStore:   srcStore,
		section:        "/home",
		sideShown:      1,
		posterGrids:    make(map[string]*list.PosterGridState),
		favGrid: list.PosterGridState{
			SourceID: "favorites",
			Title:    "我的收藏",
			Started:  true,
		},
		onlinePlatforms: []core.ProviderInfo{
			{ID: "online:hongguo", Name: "红果短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
			{ID: "online:yeguo", Name: "野果短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
			{ID: "online:huangdou", Name: "黄豆短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
			{ID: "online:huangju", Name: "黄桔短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
			{ID: "online:dsd", Name: "短剧岛", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
		},
		vodProfile:    player.NewVODSingleProfile(),
		seriesProfile: player.NewVODSeriesProfile(),
		iptvProfile:   player.NewIPTVProfile(),
	}
	a.img = imageloader.New(filepath.Join(cacheDir, "covers"), a.update)
	a.seriesProfile.Images = a.img
	a.seriesProfile.OnOpenRelated = func(it core.Item) {
		a.openDetail(it)
	}
	a.seriesProfile.OnRandomNext = func() {
		if a.playerState.Session != nil && a.playerState.Session.Queue != nil {
			if it, ok := a.playerState.Session.Queue.Active(); ok {
				a.playNextRecommendedSeries(it.Ref.Source, it.Extra["seriesId"])
			}
		}
	}
	a.initIPTVCallbacks()
	a.initPlayerCallbacks()
	mediakeys.Init(mediakeys.Handlers{
		OnPlay: func() {
			a.update(func() {
				if a.onPlayer && a.mpv != nil && a.playerState.Snap.Paused {
					a.mpv.SetProperty("pause", "no")
				}
			})
		},
		OnPause: func() {
			a.update(func() {
				if a.onPlayer && a.mpv != nil && !a.playerState.Snap.Paused {
					a.mpv.SetProperty("pause", "yes")
				}
			})
		},
		OnToggle: func() {
			a.update(func() {
				if a.onPlayer && a.playerState.OnTogglePlay != nil {
					a.playerState.OnTogglePlay()
				}
			})
		},
		OnNext: func() {
			a.update(func() {
				if a.onPlayer && a.playerState.OnNext != nil {
					a.playerState.OnNext()
				}
			})
		},
		OnPrev: func() {
			a.update(func() {
				if a.onPlayer && a.playerState.OnPrev != nil {
					a.playerState.OnPrev()
				}
			})
		},
	})
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
			if st.Running {
				a.syncOnlineProviders()
			}
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

func (a *App) syncOnlineProviders() {
	if a.gateway == nil {
		return
	}
	safego.Go("gw-providers", a.logger.Printf, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		provs, err := a.gateway.Providers(ctx)
		if err != nil || len(provs) == 0 {
			return
		}
		a.update(func() {
			a.onlinePlatforms = provs
		})
	})
}

func (a *App) shutdown() {
	a.savePlaybackProgress()
	if a.detailCancel != nil {
		a.detailCancel()
	}
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
	if a.sourcesStore != nil {
		a.sourcesStore.Close()
	}
	if a.favoritesStore != nil {
		a.favoritesStore.Close()
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
	onDetail := path == "/detail"
	if a.onPlayer && !onPlayer {
		a.leavePlayerCleanup()
	}
	if !a.onPlayer && onPlayer {
		if a.img != nil {
			// Release UI poster bitmaps during playback to cap memory.
			a.img.ClearMemory()
		}
		// Scheme B: Forward navigation back to /play resumes the previous queue from history.
		if a.playerState.Session != nil && a.playerState.Session.Phase == core.PhaseIdle && a.playerState.Session.Queue != nil {
			q := a.playerState.Session.Queue
			a.update(func() {
				if a.router.Path() == "/play" && a.playerState.Session != nil && a.playerState.Session.Phase == core.PhaseIdle {
					a.startQueue(q)
				}
			})
		}
	}
	a.onPlayer = onPlayer
	if !onPlayer && !onDetail {
		a.section = path
	}

	row := ui.Row(c).Fill().AlignItems(ui.Stretch)
	targetSide := float32(1)
	if onPlayer || onDetail {
		targetSide = 0
	}
	a.sideShown = row.Animate("sidebar", targetSide, 200*time.Millisecond)
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
			case r.Match("/favorites"):
				r.Title("收藏")
				a.syncFavGrid()
				list.PosterGridPage(c, &a.favGrid, a.img, list.PosterGridActions{
					OnOpenDetail: a.openDetail,
					OnToggleFav:  a.toggleFav,
					IsFav:        a.favorites.Has,
				})
			case r.Match("/iptv"):
				r.Title("IPTV 电视直播")
				list.ChannelListPage(c, &a.iptvList, list.ChannelListActions{
					Images:        a.img,
					OnAddSub:      func() { a.iptvModalOpen = true },
					OnRefresh:     a.refreshIPTV,
					OnSelectGroup: func(g string) { a.iptvList.ActiveGroup = g },
					OnPlayChannel: a.playIPTVChannel,
					OnToggleFav:   a.toggleFav,
					IsFav:         a.favorites.Has,
				})
			case r.Match("/online/{id}"):
				id := "online:" + r.Param("id")
				pg := a.getPosterGrid(id)
				r.Title(pg.Title)
				list.PosterGridPage(c, pg, a.img, list.PosterGridActions{
					OnLoadMore:   func() { a.loadPosterPage(pg) },
					OnSelectMode: func(m string) { pg.Mode = m; pg.ResetFeed(); a.loadPosterPage(pg) },
					OnSelectCat:  func(cat string) { pg.Category = cat; pg.ResetFeed(); a.loadPosterPage(pg) },
					OnSearch:     func(q string) { pg.SearchQuery = q; pg.Mode = "search"; pg.ResetFeed(); a.loadPosterPage(pg) },
					OnOpenDetail: a.openDetail,
					OnToggleFav:  a.toggleFav,
					IsFav:        a.favorites.Has,
				})
			case r.Match("/detail"):
				r.Title(a.detailState.Item.Title)
				resumeEp := a.resumeEpisodeIndex(a.detailState.Item)
				detail.Page(c, &a.detailState, a.img, a.favorites.Has(a.detailState.Item.Ref), resumeEp, detail.Actions{
					OnBack: func() {
						a.router.Back()
						if a.router.Path() == "/detail" {
							a.router.Push("/home")
						}
					},
					OnPlayEpisode: a.playDetailEpisode,
					OnToggleFav:   func() { a.toggleFav(a.detailState.Item) },
					OnOpenRelated: a.openDetail,
					OnRetry:       func() { a.openDetail(a.detailState.Item) },
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
				var prof player.PlayerProfile = a.vodProfile
				switch {
				case a.isIPTVPlayback():
					prof = a.iptvProfile
				case a.isOnlinePlayback():
					prof = a.seriesProfile
				}
				player.Stage(c, &a.playerState, prof, a.surf, sideW)
			}
		}).Grow(1).MinWidth(0)

		if onPlayer {
			main.Background(ui.Transparent)
		} else if onDetail {
			main.Background(ui.Hex("#0b0c10"))
		} else {
			main.Background(t.Background)
		}
	})

	a.openURLModal(c)
	a.openIPTVModal(c)

	c.OnShortcut(ui.Cmd, ui.KeyO, a.openFileDialog)
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyO, a.openFolderDialog)
	c.OnShortcut(ui.Cmd, ui.KeyU, func() { a.urlModalOpen = true })
	c.OnShortcut(ui.Cmd, ui.KeyComma, func() { a.router.Push("/settings") })
}

func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(theme.TitleBarHeight).Padding(0, 14, 0, 82).AlignItems(ui.Center).Gap(8).Shrink(0).DragWindow().Children(func() {
		if badge := theme.BrandBadge(); badge != nil {
			ui.Box(c).Size(20, 20).Radius(5).Clip().Children(func() {
				ui.Image(c, badge).Size(20, 20)
			})
		}
		ui.Text(c, "Velan").FontSize(13.5).FontWeight(600)
		ui.Text(c, "微澜").FontSize(11).TextColor(t.TextMuted)
	})
	selected := a.section
	sb := ui.Sidebar(c, &selected, func() {
		ui.SidebarSection(c, "媒体", nil, func() {
			ui.SidebarItem(c, "/home", theme.IconHome, "首页")
			ui.SidebarItem(c, "/local", theme.IconFilm, "本地视频")
			ui.SidebarItem(c, "/iptv", theme.IconTV, "IPTV 直播")
			ui.SidebarItem(c, "/favorites", theme.IconStar, "收藏")
		})
		ui.SidebarSection(c, "在线平台", nil, func() {
			for _, p := range a.onlinePlatforms {
				short := strings.TrimPrefix(p.ID, "online:")
				ui.SidebarItem(c.Key("sb-"+p.ID), "/online/"+short, theme.IconGlobe, p.Name)
			}
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

func (a *App) getPosterGrid(sourceID string) *list.PosterGridState {
	if pg := a.posterGrids[sourceID]; pg != nil {
		return pg
	}
	title := sourceID
	for _, p := range a.onlinePlatforms {
		if p.ID == sourceID {
			title = p.Name
			break
		}
	}
	pg := &list.PosterGridState{
		SourceID: sourceID,
		Title:    title,
		Mode:     "recommend",
		Modes:    []core.Section{{ID: "recommend", Label: "推荐"}, {ID: "rank", Label: "榜单"}},
	}
	a.posterGrids[sourceID] = pg

	if a.gateway != nil && a.gwStatus.Running {
		safego.Go("gw-sections", a.logger.Printf, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sec, err := a.gateway.Sections(ctx, sourceID)
			if err != nil {
				return
			}
			a.update(func() {
				if len(sec.Modes) > 0 {
					pg.Modes = sec.Modes
				}
				pg.Categories = sec.Categories
				if pg.Category == "" && len(sec.Categories) > 0 {
					pg.Category = sec.Categories[0].ID
				}
			})
		})
	}
	return pg
}

func (a *App) loadPosterPage(pg *list.PosterGridState) {
	if pg.Loading || a.gateway == nil {
		return
	}
	pg.Started = true
	pg.Loading = true
	pg.Err = ""
	cursor := pg.NextCursor
	mode := pg.Mode
	cat := pg.Category
	q := strings.TrimSpace(pg.SearchQuery)
	src := pg.SourceID

	safego.Go("gw-browse", a.logger.Printf, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var page core.Page
		var err error
		if mode == "search" && q != "" {
			page, err = a.gateway.Search(ctx, src, q, cursor)
		} else {
			page, err = a.gateway.Browse(ctx, src, core.Query{
				Section:  mode,
				Category: cat,
				Cursor:   cursor,
			})
		}
		a.update(func() {
			pg.Loading = false
			if err != nil {
				pg.Err = err.Error()
				pg.ErrAt = time.Now()
				return
			}
			pg.AppendPage(page)
		})
	})
}

func (a *App) syncFavGrid() {
	items := make([]core.Item, 0, len(a.favorites.Items))
	for _, f := range a.favorites.Items {
		items = append(items, f.Item)
	}
	a.favGrid.Items = items
}

func (a *App) toggleFav(it core.Item) {
	a.favorites.Toggle(it)
	a.favoritesStore.Put(append([]core.FavoriteItem(nil), a.favorites.Items...))
}

func (a *App) openDetail(it core.Item) {
	if a.detailCancel != nil {
		a.detailCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	a.detailCancel = cancel

	a.detailState = detail.State{
		Item:    it,
		Loading: true,
	}
	a.seriesProfile.Related = nil
	a.seriesProfile.DrawerTab = 0
	a.router.Push("/detail")

	if a.gateway == nil {
		a.detailState.Loading = false
		a.detailState.Err = "网关未就绪"
		return
	}
	safego.Go("gw-detail", a.logger.Printf, func() {
		d, err := a.gateway.Detail(ctx, it.Ref.Source, it.Ref.ID)
		var rel []core.Item
		if err == nil && d != nil {
			rel, _ = a.gateway.Related(ctx, it.Ref.Source, it.Ref.ID, d.Item.Title)
		}
		a.update(func() {
			if ctx.Err() != nil {
				return
			}
			a.detailState.Loading = false
			if err != nil {
				a.detailState.Err = err.Error()
				return
			}
			a.detailState.Detail = d
			for i := range d.Episodes {
				if d.Episodes[i].Extra == nil {
					d.Episodes[i].Extra = map[string]string{}
				}
				if d.Item.Title != "" {
					d.Episodes[i].Extra["seriesTitle"] = d.Item.Title
				}
				if d.Episodes[i].Poster == "" {
					d.Episodes[i].Poster = d.Item.Poster
				}
			}
			a.detailState.Related = rel
			a.seriesProfile.Related = rel
			if ep := a.resumeEpisodeIndex(a.detailState.Item); ep > 100 {
				a.detailState.RangeIdx = (ep - 1) / 100
			}
		})
	})
}

func (a *App) playDetailEpisode(idx int) {
	if a.detailState.Detail == nil || len(a.detailState.Detail.Episodes) == 0 {
		return
	}
	a.seriesProfile.Related = a.detailState.Related
	q := core.NewQueue(a.detailState.Detail.Episodes, idx)
	a.startQueue(q)
}

func (a *App) resumeEpisodeIndex(series core.Item) int {
	if a.detailState.Detail == nil {
		return 0
	}
	eps := a.detailState.Detail.Episodes
	epByID := make(map[string]int, len(eps))
	for i, ep := range eps {
		epByID[ep.Ref.ID] = i + 1
	}
	// History.Records is ordered most-recent-first, so the first match is the
	// latest episode the user actually watched for this series.
	for _, rec := range a.history.Records {
		if rec.Ref.Source != series.Ref.Source {
			continue
		}
		if rec.SeriesID == series.Ref.ID || rec.Ref.ID == series.Ref.ID {
			if idx, ok := epByID[rec.Ref.ID]; ok {
				return idx
			}
			if rec.Episode > 0 && rec.Episode <= len(eps) {
				return rec.Episode
			}
		}
		if idx, ok := epByID[rec.Ref.ID]; ok {
			return idx
		}
	}
	return 0
}

func (a *App) isOnlinePlayback() bool {
	if a.playerState.Session == nil || a.playerState.Session.Queue == nil {
		return false
	}
	if it, ok := a.playerState.Session.Queue.Active(); ok {
		return strings.HasPrefix(it.Ref.Source, "online:")
	}
	return false
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

func (a *App) openIPTVModal(c *ui.Context) {
	if !a.iptvModalOpen {
		return
	}
	t := c.Theme()
	ui.Box(c.Key("iptv-backdrop")).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.45)).Center().Children(func() {
		ui.Column(c.Key("iptv-card")).Width(500).Padding(20).Radius(12).
			Background(t.Surface).Border(1, theme.Hairline(t)).Gap(12).Children(func() {
			ui.Text(c, "IPTV 订阅源管理").FontSize(15).FontWeight(600)

			subs := a.iptvMgr.Subscriptions()
			if len(subs) > 0 {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					for i, sub := range subs {
						idx := i
						label := sub.Name
						if label == "" {
							label = sub.URL
						}
						ui.Row(c.Key("iptv-sub-"+sub.ID)).FillWidth().Padding(6, 10).Radius(6).
							Background(t.Text.Alpha(0.04)).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, label).FontSize(12).MaxLines(1)
							ui.Spacer(c)
							if ui.Button(c.Key("iptv-sub-del-"+sub.ID), "删除").Clicked() {
								next := make([]iptv.Subscription, 0, len(subs)-1)
								for j, s := range subs {
									if j != idx {
										next = append(next, s)
									}
								}
								a.iptvMgr.SetSubscriptions(next)
								a.sourcesStore.Put(next)
								a.refreshIPTV()
							}
						})
					}
				})
			}

			ui.TextInput(c.Key("iptv-sub-name"), &a.iptvSubName).
				Placeholder("订阅名称（如 家庭直播）").FillWidth()
			tf := ui.TextInput(c.Key("iptv-sub-url"), &a.iptvSubURL).
				Placeholder("M3U / M3U8 订阅链接或本地文件路径").FillWidth()
			ui.TextInput(c.Key("iptv-sub-epg"), &a.iptvSubEPG).
				Placeholder("XMLTV EPG 链接（可选，支持 .xml / .xml.gz）").FillWidth()

			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("iptv-cancel"), "关闭").Clicked() {
					a.iptvModalOpen = false
				}
				if ui.PrimaryButton(c.Key("iptv-save"), "添加并刷新").Clicked() || tf.Submitted() {
					rawURL := strings.TrimSpace(a.iptvSubURL)
					if rawURL != "" {
						name := strings.TrimSpace(a.iptvSubName)
						if name == "" {
							name = "IPTV 订阅"
						}
						sub := iptv.Subscription{
							ID:      fmt.Sprintf("sub-%d", time.Now().UnixNano()),
							Name:    name,
							URL:     rawURL,
							EPGURL:  strings.TrimSpace(a.iptvSubEPG),
							Enabled: true,
						}
						next := append(a.iptvMgr.Subscriptions(), sub)
						a.iptvMgr.SetSubscriptions(next)
						a.sourcesStore.Put(next)
						a.iptvSubName = ""
						a.iptvSubURL = ""
						a.iptvSubEPG = ""
						a.iptvModalOpen = false
						a.refreshIPTV()
					}
				}
			})
		})
	})
}

func (a *App) refreshIPTV() {
	if a.iptvMgr == nil {
		return
	}
	a.iptvList.Loading = true
	a.iptvList.Err = ""
	safego.Go("iptv-refresh", a.logger.Printf, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.iptvMgr.Refresh(ctx)
		items, chErr := a.iptvMgr.Channels(ctx)
		_, groups, _ := a.iptvMgr.Snapshot()
		a.update(func() {
			a.iptvList.Loading = false
			if err != nil {
				a.iptvList.Err = err.Error()
			} else if chErr != nil {
				a.iptvList.Err = chErr.Error()
			}
			a.iptvList.Channels = items
			a.iptvList.Groups = groups
		})
	})
}

func (a *App) playIPTVChannel(it core.Item) {
	if a.iptvMgr != nil {
		if q := a.iptvMgr.BuildQueue(it.Ref.ID); q != nil {
			a.startQueue(q)
			return
		}
	}
	a.startQueue(core.NewQueue([]core.Item{it}, 0))
}

func (a *App) isIPTVPlayback() bool {
	if a.playerState.Session == nil || a.playerState.Session.Queue == nil {
		return false
	}
	if it, ok := a.playerState.Session.Queue.Active(); ok {
		return it.Ref.Source == iptv.SourceID
	}
	return false
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
		return
	}
	if strings.HasPrefix(rec.Ref.Source, "online:") {
		sid := rec.SeriesID
		if sid == "" {
			if rec.Ref.Kind == core.KindSeries {
				sid = rec.Ref.ID
			} else {
				a.logger.Printf("resumeWatchRecord: missing seriesId for %+v", rec)
				return
			}
		}
		a.openDetail(core.Item{
			Ref:    core.ItemRef{Source: rec.Ref.Source, Kind: core.KindSeries, ID: sid},
			Title:  rec.Title,
			Poster: rec.Poster,
		})
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

func (a *App) resetPlayerContext(clearSeriesRelated bool, resetIPTV bool) {
	if a.mpv != nil {
		a.mpv.Command("stop")
		if resetIPTV {
			a.mpv.SetProperty("speed", "1.0")
		}
	}
	if a.playerState.DanmakuSched != nil {
		a.playerState.DanmakuSched.Clear()
	}
	a.playerState.Snap = mpv.Snapshot{}
	a.playerState.SnapAt = time.Time{}
	a.playerState.BufferingSince = time.Time{}
	a.playerState.Seeking = false
	a.playerState.SeekTarget = 0
	a.playerState.ActiveMenu = ""
	a.playerState.OSDMessage = ""
	a.playerState.OSDUntil = time.Time{}

	if clearSeriesRelated && a.seriesProfile != nil {
		a.seriesProfile.Related = nil
		a.seriesProfile.DrawerTab = 0
		a.playerState.DrawerOpen = false
		a.playerState.DrawerList = ui.ListState{}
	}
	if resetIPTV && a.iptvProfile != nil {
		a.iptvProfile.PendingIdx = -1
		a.iptvProfile.PendingUntil = time.Time{}
		a.iptvProfile.DigitBuf = ""
		a.iptvProfile.DigitUntil = time.Time{}
		a.iptvProfile.NowProgram = ""
		a.iptvProfile.EPGList = nil
		a.iptvProfile.DrawerMode = "channels"
	}
}

func (a *App) startQueue(q *core.Queue) {
	a.savePlaybackProgress()
	var prevSource, prevSeries string
	if a.playerState.Session != nil && a.playerState.Session.Queue != nil {
		if prev, ok := a.playerState.Session.Queue.Active(); ok {
			prevSource = prev.Ref.Source
			prevSeries = prev.Extra["seriesId"]
		}
	}
	nextItem, _ := q.Active()
	sourceChanged := prevSource != "" && prevSource != nextItem.Ref.Source
	seriesChanged := prevSeries != "" && prevSeries != nextItem.Extra["seriesId"]
	a.resetPlayerContext(sourceChanged || seriesChanged, nextItem.Ref.Source != iptv.SourceID || sourceChanged)

	if a.playerState.Session == nil {
		a.playerState.Session = core.NewPlaybackSession()
	}
	gen := a.playerState.Session.BeginResolve(q)
	a.playerState.DrawerList.ScrollTo(q.Current, ui.Center)
	a.playerState.Touch(time.Now())
	if a.router.Path() != "/play" {
		a.router.Push("/play")
	}

	if len(a.seriesProfile.Related) == 0 && nextItem.Extra["seriesId"] != "" {
		if a.detailState.Item.Ref.ID == nextItem.Extra["seriesId"] && len(a.detailState.Related) > 0 {
			a.seriesProfile.Related = a.detailState.Related
		} else if strings.HasPrefix(nextItem.Ref.Source, "online:") && a.gateway != nil {
			srcID, sid, sTitle := nextItem.Ref.Source, nextItem.Extra["seriesId"], nextItem.Extra["seriesTitle"]
			safego.Go("gw-player-related", a.logger.Printf, func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				rel, err := a.gateway.Related(ctx, srcID, sid, sTitle)
				if err != nil || len(rel) == 0 {
					return
				}
				a.update(func() {
					if a.playerState.Session != nil && a.playerState.Session.Generation == gen {
						a.seriesProfile.Related = rel
					}
				})
			})
		}
	}

	item, ok := q.Active()
	if !ok {
		return
	}
	if err := a.ensurePlayerReady(); err != nil {
		a.playerState.Session.Phase = core.PhaseFailed
		a.playerState.Session.LastError = err.Error()
		return
	}

	safego.Go("resolve-play", a.logger.Printf, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var src *core.PlaySource
		var err error
		switch {
		case item.Ref.Source == local.SourceID:
			src, err = a.local.Resolve(ctx, item.Ref, core.ResolveOpts{})
		case item.Ref.Source == iptv.SourceID:
			src, err = a.iptvMgr.Resolve(ctx, item.Ref, core.ResolveOpts{})
		case strings.HasPrefix(item.Ref.Source, "online:"):
			if a.gateway == nil {
				err = fmt.Errorf("网关未就绪")
			} else {
				sid := item.Extra["seriesId"]
				epNo, _ := strconv.Atoi(item.Extra["ep"])
				if epNo <= 0 {
					epNo = item.Index
				}
				src, err = a.gateway.Resolve(ctx, item.Ref.Source, sid, item.Ref.ID, epNo, "720p")
			}
		default:
			src = &core.PlaySource{Ref: item.Ref, Title: item.Title, URL: item.Ref.ID, Kind: core.StreamMP4}
		}

		a.update(func() {
			if err != nil {
				if a.playerState.Session.Generation == gen {
					a.playerState.Session.LastError = err.Error()
					a.playerState.Session.Phase = core.PhaseFailed
				}
				return
			}
			if item.Ref.Source == iptv.SourceID {
				if a.iptvCurChannel != "" && a.iptvCurChannel != item.Ref.ID {
					a.iptvPrevChannel = a.iptvCurChannel
				}
				a.iptvCurChannel = item.Ref.ID
				if a.iptvProfile != nil {
					a.iptvProfile.LastChannel = a.iptvPrevChannel
					a.iptvProfile.NowProgram = ""
					a.iptvProfile.EPGList = nil
					_, _, epg := a.iptvMgr.Snapshot()
					if epg != nil {
						nowProg, _ := epg.NowAndNext(item.Extra["tvg_id"], item.Title, time.Now())
						if nowProg != nil {
							a.iptvProfile.NowProgram = nowProg.Title
						}
						a.iptvProfile.EPGList = epg.ForChannel(item.Extra["tvg_id"], item.Title)
					}
				}
			} else {
				if stTitle := item.Extra["seriesTitle"]; stTitle != "" && !strings.Contains(src.Title, stTitle) {
					if src.Title != "" {
						src.Title = stTitle + " · " + src.Title
					} else {
						src.Title = stTitle
					}
				}
				src.StartAt = a.history.ResumePosition(item.Ref)
			}
			if !a.playerState.Session.SetResolved(gen, src) {
				return
			}
			if loadErr := a.mpv.Load(src); loadErr != nil {
				a.playerState.Session.LastError = loadErr.Error()
				a.playerState.Session.Phase = core.PhaseFailed
				return
			}
			a.loadDanmakuForSource(gen, src)
		})
	})
}

func (a *App) loadDanmakuForSource(gen uint64, src *core.PlaySource) {
	if a.playerState.DanmakuSched != nil {
		a.playerState.DanmakuSched.Clear()
	}
	if src == nil {
		return
	}
	if src.Kind == core.StreamLocalFile {
		if sidecar := danmaku.DiscoverSidecar(src.URL); sidecar != "" {
			safego.Go("dm-local", a.logger.Printf, func() {
				items, err := danmaku.ParseFile(sidecar)
				if err != nil || len(items) == 0 {
					return
				}
				a.update(func() {
					if a.playerState.Session != nil && a.playerState.Session.Generation == gen {
						a.playerState.DanmakuSched.Load(items, a.settings.DanmakuKeywords)
					}
				})
			})
		}
		return
	}
	if src.Danmaku != nil && a.gateway != nil {
		ref := *src.Danmaku
		safego.Go("dm-online", a.logger.Printf, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			items, err := a.gateway.Danmaku(ctx, ref.Source, ref.SeriesID, ref.ItemID)
			if err != nil {
				a.logger.Printf("danmaku fetch failed (%s/%s): %v", ref.SeriesID, ref.ItemID, err)
				return
			}
			a.logger.Printf("danmaku loaded (%s/%s): %d items", ref.SeriesID, ref.ItemID, len(items))
			if len(items) == 0 {
				return
			}
			a.update(func() {
				if a.playerState.Session != nil && a.playerState.Session.Generation == gen {
					a.playerState.DanmakuSched.Load(items, a.settings.DanmakuKeywords)
				}
			})
		})
	}
}

func (a *App) onMPVSnapshot(snap mpv.Snapshot) {
	a.playerState.Snap = snap
	a.playerState.SnapAt = time.Now()
	sess := a.playerState.Session
	if sess == nil || !a.onPlayer {
		return
	}
	if snap.Error != "" {
		if retry, lineIdx := sess.HandleFailure(snap.Error); retry && sess.Source != nil {
			if lineIdx >= 0 && lineIdx < len(sess.Source.Lines) {
				line := sess.Source.Lines[lineIdx]
				sess.Source.URL = line.URL
				sess.Source.Kind = line.Kind
				sess.Source.Headers = line.Headers
			}
			_ = a.mpv.Load(sess.Source)
		}
		return
	}
	if snap.Loaded {
		sess.SetPlaying()
		if snap.Buffering && a.playerState.BufferingSince.IsZero() {
			a.playerState.BufferingSince = time.Now()
		} else if !snap.Buffering {
			a.playerState.BufferingSince = time.Time{}
		}
		sess.SetBuffering(snap.Buffering)
		title := ""
		if sess.Source != nil {
			title = sess.Source.Title
		}
		mediakeys.UpdateNowPlaying(title, true, !snap.Paused, snap.Position, snap.Duration)
		if sess.Source != nil && sess.Source.Ref.Source == iptv.SourceID &&
			sess.LineIndex >= 0 && sess.LineIndex < len(sess.Source.Lines) {
			a.iptvMgr.RememberWorkingLine(sess.Source.Ref.ID, sess.Source.Lines[sess.LineIndex].ID)
		}
	}
	if snap.EOF && sess.Phase == core.PhasePlaying {
		sess.Phase = core.PhaseEnded
		a.savePlaybackProgress()
		if a.settings.AutoNext && sess.Queue != nil {
			if sess.Queue.HasNext() {
				if _, ok := sess.Queue.Next(); ok {
					a.startQueue(sess.Queue)
				}
			} else if it, ok := sess.Queue.Active(); ok && strings.HasPrefix(it.Ref.Source, "online:") {
				a.playNextRecommendedSeries(it.Ref.Source, it.Extra["seriesId"])
			}
		}
	}
}

func (a *App) playNextRecommendedSeries(source, prevSeriesID string) {
	if a.gateway == nil {
		return
	}
	var cands []core.Item
	for _, it := range a.seriesProfile.Related {
		if it.Ref.ID != "" && it.Ref.ID != prevSeriesID {
			cands = append(cands, it)
		}
	}
	if len(cands) == 0 {
		for _, it := range a.detailState.Related {
			if it.Ref.ID != "" && it.Ref.ID != prevSeriesID {
				cands = append(cands, it)
			}
		}
	}

	a.playerState.ShowOSD(time.Now(), "正在为您挑选下一部短剧…")
	safego.Go("auto-next-drama", a.logger.Printf, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var chosen core.Item
		if len(cands) > 0 {
			chosen = cands[int(time.Now().UnixNano()%int64(len(cands)))]
		} else {
			page, err := a.gateway.Browse(ctx, source, core.Query{Section: "feed"})
			if err != nil || len(page.Items) == 0 {
				page, err = a.gateway.Browse(ctx, source, core.Query{Section: "recommend"})
				if err != nil || len(page.Items) == 0 {
					return
				}
			}
			var pool []core.Item
			for _, it := range page.Items {
				if it.Ref.ID != "" && it.Ref.ID != prevSeriesID {
					pool = append(pool, it)
				}
			}
			if len(pool) == 0 {
				pool = page.Items
			}
			chosen = pool[int(time.Now().UnixNano()%int64(len(pool)))]
		}

		d, err := a.gateway.Detail(ctx, chosen.Ref.Source, chosen.Ref.ID)
		if err != nil || d == nil || len(d.Episodes) == 0 {
			return
		}
		for i := range d.Episodes {
			if d.Episodes[i].Extra == nil {
				d.Episodes[i].Extra = map[string]string{}
			}
			if d.Item.Title != "" {
				d.Episodes[i].Extra["seriesTitle"] = d.Item.Title
			}
			if d.Episodes[i].Poster == "" {
				d.Episodes[i].Poster = d.Item.Poster
			}
		}
		rel, _ := a.gateway.Related(ctx, chosen.Ref.Source, chosen.Ref.ID, d.Item.Title)

		a.update(func() {
			a.detailState = detail.State{
				Item:    d.Item,
				Detail:  d,
				Related: rel,
			}
			a.seriesProfile.Related = rel
			a.playerState.ShowOSD(time.Now(), "即将连播：《"+d.Item.Title+"》")
			a.startQueue(core.NewQueue(d.Episodes, 0))
		})
	})
}

func (a *App) leavePlayerCleanup() {
	a.savePlaybackProgress()
	if a.surf != nil {
		a.surf.Show(false)
	}
	mediakeys.UpdateNowPlaying("", false, false, 0, 0)
	a.resetPlayerContext(true, true)
	if a.playerState.Session != nil {
		a.playerState.Session.Generation++
		a.playerState.Session.Phase = core.PhaseIdle
		a.playerState.Session.Source = nil
		a.playerState.Session.LastError = ""
	}
	a.playerState.DrawerOpen = false
}

func (a *App) savePlaybackProgress() {
	sess := a.playerState.Session
	if sess == nil || sess.Source == nil || !a.playerState.Snap.Loaded {
		return
	}
	if sess.Source.Ref.Source == iptv.SourceID {
		return
	}
	var seriesID, poster string
	var epNo int
	if sess.Queue != nil {
		if act, ok := sess.Queue.Active(); ok {
			seriesID = act.Extra["seriesId"]
			poster = act.Poster
			epNo, _ = strconv.Atoi(act.Extra["ep"])
			if epNo <= 0 {
				epNo = act.Index
			}
		}
	}
	if poster == "" && a.detailState.Item.Ref.ID == seriesID {
		poster = a.detailState.Item.Poster
	}
	a.history.Record(core.WatchRecord{
		Ref:      sess.Source.Ref,
		SeriesID: seriesID,
		Episode:  epNo,
		Title:    sess.Source.Title,
		Poster:   poster,
		Position: a.playerState.Snap.Position,
		Duration: a.playerState.Snap.Duration,
	})
	a.historyStore.Put(append([]core.WatchRecord(nil), a.history.Records...))
}

func (a *App) initPlayerCallbacks() {
	st := &a.playerState
	st.AspectRatio = a.settings.AspectRatio
	st.DanmakuSched = danmaku.NewScheduler()
	st.DanmakuRend = danmaku.NewRenderer()
	if a.settings.DanmakuFontSize <= 0 {
		a.settings.DanmakuEnabled = true
		a.settings.DanmakuFontSize = 20
	}
	if a.settings.DanmakuOpacity <= 0 {
		a.settings.DanmakuOpacity = 0.90
	}
	if a.settings.DanmakuSpeed <= 0 {
		a.settings.DanmakuSpeed = 1.0
	}
	if a.settings.DanmakuArea <= 0 {
		a.settings.DanmakuArea = 0.50
	}
	st.DanmakuCfg = danmaku.Config{
		Enabled:  a.settings.DanmakuEnabled,
		FontSize: a.settings.DanmakuFontSize,
		Opacity:  a.settings.DanmakuOpacity,
		Speed:    a.settings.DanmakuSpeed,
		Area:     a.settings.DanmakuArea,
		Keywords: a.settings.DanmakuKeywords,
	}
	syncDanmakuSettings := func() {
		a.settings.DanmakuEnabled = st.DanmakuCfg.Enabled
		a.settings.DanmakuFontSize = st.DanmakuCfg.FontSize
		a.settings.DanmakuOpacity = st.DanmakuCfg.Opacity
		a.settings.DanmakuSpeed = st.DanmakuCfg.Speed
		a.settings.DanmakuArea = st.DanmakuCfg.Area
		a.settingsStore.Put(a.settings)
		// Re-rasterize active sprites immediately so adjustments take effect live.
		for i := range st.DanmakuSched.Active {
			it := &st.DanmakuSched.Active[i]
			it.Sprite = st.DanmakuRend.MakeSprite(it.Text, it.Color, st.DanmakuCfg)
		}
	}
	st.OnToggleDanmaku = func() {
		st.DanmakuCfg.Enabled = !st.DanmakuCfg.Enabled
		syncDanmakuSettings()
		if st.DanmakuCfg.Enabled {
			st.ShowOSD(time.Now(), "弹幕：开启")
		} else {
			st.ShowOSD(time.Now(), "弹幕：关闭")
		}
	}
	st.OnDanmakuChanged = syncDanmakuSettings
	st.OnLoadDanmakuFile = func() {
		safego.Go("open-danmaku-dialog", a.logger.Printf, func() {
			files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
				Parent: a.win,
				Title:  "选择弹幕文件 (XML / ASS)",
				Filters: []mygo.FileFilter{
					{Name: "弹幕文件", Extensions: []string{"xml", "ass", "ssa"}},
				},
			})
			if err == nil && len(files) > 0 {
				items, parseErr := danmaku.ParseFile(files[0])
				a.update(func() {
					if parseErr != nil {
						st.ShowOSD(time.Now(), "弹幕解析失败")
						return
					}
					st.DanmakuSched.Load(items, st.DanmakuCfg.Keywords)
					st.DanmakuCfg.Enabled = true
					syncDanmakuSettings()
					st.ShowOSD(time.Now(), fmt.Sprintf("已加载 %d 条弹幕", len(items)))
				})
			}
		})
	}
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
		dir := filepath.Join(homeDir, "Pictures", "Velan")
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

func (a *App) initIPTVCallbacks() {
	p := player.NewIPTVProfile()
	p.Images = a.img
	a.iptvProfile = p
	p.OnSelectLine = func(lineIdx int) {
		sess := a.playerState.Session
		if sess == nil || sess.Source == nil || lineIdx < 0 || lineIdx >= len(sess.Source.Lines) {
			return
		}
		line := sess.Source.Lines[lineIdx]
		sess.LineIndex = lineIdx
		sess.Source.URL = line.URL
		sess.Source.Kind = line.Kind
		sess.Source.Headers = line.Headers
		if a.mpv != nil {
			_ = a.mpv.Load(sess.Source)
		}
		a.playerState.ShowOSD(time.Now(), "切换线路: "+line.Label)
	}
	p.OnRefreshStream = func() {
		sess := a.playerState.Session
		if sess == nil || sess.Source == nil || a.mpv == nil {
			return
		}
		_ = a.mpv.Load(sess.Source)
		a.playerState.ShowOSD(time.Now(), "重新加载直播流")
	}
	p.OnTuneIndex = func(idx int) {
		sess := a.playerState.Session
		if sess == nil || sess.Queue == nil {
			return
		}
		if _, ok := sess.Queue.Jump(idx); ok {
			a.startQueue(sess.Queue)
		}
	}
	p.OnRecallPrev = func() {
		if a.iptvPrevChannel == "" {
			return
		}
		sess := a.playerState.Session
		if sess != nil && sess.Queue != nil {
			for i, it := range sess.Queue.Items {
				if it.Ref.ID == a.iptvPrevChannel {
					if _, ok := sess.Queue.Jump(i); ok {
						a.startQueue(sess.Queue)
					}
					return
				}
			}
		}
	}
	p.OnOpenEPG = func() {
		sess := a.playerState.Session
		if sess == nil || sess.Source == nil || a.iptvMgr == nil {
			return
		}
		ref := sess.Source.Ref
		safego.Go("iptv-epg", a.logger.Printf, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			list, err := a.iptvMgr.EPG(ctx, ref, time.Now())
			if err != nil {
				return
			}
			a.update(func() {
				p.EPGList = list
			})
		})
	}
}
