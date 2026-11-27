package player

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/infra/imageloader"
	"github.com/xiangkunzhang/Velan/internal/provider/live"
)

// LiveRoomProfile implements PlayerProfile for live streaming rooms:
// no seek bar, CDN/quality line switching, real-time danmaku chat panel,
// and strictly zero gift/superchat/revenue UI.
type LiveRoomProfile struct {
	DrawerTab   int // 0 = chat danmaku, 1 = room list
	Streamer    string
	OnlineCount string
	IsFavorited bool
	Images      *imageloader.Loader
	ChatList    []live.ChatMessage
	ChatState   ui.ListState

	OnSelectLine    func(lineIdx int)
	OnRefreshStream func()
	OnClearChat     func()
	OnToggleFav     func()
}

func NewLiveRoomProfile() *LiveRoomProfile { return &LiveRoomProfile{} }

func (p *LiveRoomProfile) ID() string { return "live_room" }

func (p *LiveRoomProfile) TopBar(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	title := "直播间"
	if st.Session != nil && st.Session.Source != nil && st.Session.Source.Title != "" {
		title = st.Session.Source.Title
	}
	if p.Streamer != "" {
		title = p.Streamer + " · " + title
	}
	leftPad := float32(16)
	if !st.Fullscreen {
		leftPad = 82
	}
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight+8).
		Padding(8, 16, 0, leftPad).AlignItems(ui.Center).Gap(10).
		Background(ui.RGBA(10, 12, 16, 0.58)).DragWindow().Children(func() {
		if iconBtn(c, "lr-back", theme.IconBack, "返回", false).Clicked() && st.OnBack != nil {
			st.OnBack()
		}
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(white).SingleLine().Grow(1).MinWidth(0)
		if p.OnlineCount != "" {
			ui.Box(c).Padding(2, 8).Radius(5).Background(ui.RGBA(255, 255, 255, 0.1)).Children(func() {
				ui.Text(c, p.OnlineCount+" 人气").FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.85)).FontFeatures("tnum")
			})
		}
		if p.OnToggleFav != nil {
			favLabel := "☆ 关注"
			if p.IsFavorited {
				favLabel = "★ 已关注"
			}
			if pillBtn(c, "lr-fav", favLabel, p.IsFavorited).Clicked() {
				st.Touch(c.Now())
				p.OnToggleFav()
			}
		}
		if iconBtn(c, "lr-pin", theme.IconPin, "置顶", st.AlwaysOnTop).Clicked() && st.OnTogglePin != nil {
			st.Touch(c.Now())
			st.OnTogglePin()
		}
		if pillBtn(c, "lr-chat-btn", "弹幕聊天", st.DrawerOpen).Clicked() {
			st.Touch(c.Now())
			st.DrawerOpen = !st.DrawerOpen
		}
		if iconBtn(c, "lr-fs", theme.IconFullscreen, "全屏", st.Fullscreen).Clicked() && st.OnToggleFullscreen != nil {
			st.Touch(c.Now())
			st.OnToggleFullscreen()
		}
	})
}

func (p *LiveRoomProfile) BottomBar(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c).Absolute().Bottom(14).Left(18).Right(18).
		Padding(10, 14).Radius(12).Background(ui.RGBA(12, 14, 18, 0.84)).
		Border(1, ui.RGBA(255, 255, 255, 0.1)).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Box(c).Padding(2, 8).Radius(5).Background(ui.RGBA(229, 57, 53, 0.85)).Children(func() {
				ui.Text(c, "LIVE").FontSize(10.5).Bold().TextColor(white)
			})
			volIcon := theme.IconVolume
			if st.Snap.Muted || st.Snap.Volume <= 0 {
				volIcon = theme.IconMuted
			}
			if iconBtn(c, "lr-mute", volIcon, "静音", st.Snap.Muted).Clicked() && st.OnToggleMute != nil {
				st.Touch(c.Now())
				st.OnToggleMute()
			}
			vol := st.Snap.Volume
			vsl := ui.Slider(c.Key("lr-vol"), &vol, 0, 100).Width(84)
			if vsl.Changed() && st.OnVolume != nil {
				st.Touch(c.Now())
				st.OnVolume(vol)
			}

			ui.Spacer(c)

			if iconBtn(c, "lr-prev", theme.IconPrev, "上一个直播间", false).Clicked() && st.OnPrev != nil {
				st.Touch(c.Now())
				st.OnPrev()
			}
			playIcon := theme.IconPause
			if st.Snap.Paused {
				playIcon = theme.IconPlay
			}
			pb := ui.ButtonBase(c.Key("lr-play")).Size(34, 34).Radius(17).
				Background(white).Center().Cursor(ui.CursorPointer)
			pb.Children(func() {
				ui.Icon(c, playIcon).Size(15, 15).TextColor(ui.Hex("#111318"))
			})
			if pb.Clicked() && st.OnTogglePlay != nil {
				st.Touch(c.Now())
				st.OnTogglePlay()
			}
			if iconBtn(c, "lr-next", theme.IconNext, "下一个直播间", false).Clicked() && st.OnNext != nil {
				st.Touch(c.Now())
				st.OnNext()
			}

			ui.Spacer(c)

			lineLabel := "画质/线路"
			if st.Session != nil && st.Session.Source != nil && len(st.Session.Source.Lines) > 0 {
				lineLabel = fmt.Sprintf("线路 %d/%d", st.Session.LineIndex+1, len(st.Session.Source.Lines))
			}
			if pillBtn(c, "lr-lines", lineLabel, st.ActiveMenu == "lines").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "lines")
			}
			if pillBtn(c, "lr-reload", "重连", false).Clicked() && p.OnRefreshStream != nil {
				st.Touch(c.Now())
				p.OnRefreshStream()
			}
			if pillBtn(c, "lr-dm-btn", "弹", st.DanmakuCfg.Enabled || st.ActiveMenu == "danmaku").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "danmaku")
			}
			if pillBtn(c, "lr-aspect", "比例", st.ActiveMenu == "aspect").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "aspect")
			}
			if iconBtn(c, "lr-shot", theme.IconCamera, "截图", false).Clicked() && st.OnScreenshot != nil {
				st.Touch(c.Now())
				st.OnScreenshot()
			}
		})
	})

	if st.ActiveMenu != "" {
		p.renderLiveMenu(c, st)
	}
}

func (p *LiveRoomProfile) renderLiveMenu(c *ui.Context, st *PlayerViewState) {
	if st.ActiveMenu == "aspect" || st.ActiveMenu == "danmaku" {
		renderPopupMenuAt(c, st, 72)
		return
	}
	if st.ActiveMenu != "lines" || st.Session == nil || st.Session.Source == nil {
		return
	}
	ui.Column(c.Key("lr-line-menu")).Absolute().Bottom(72).Right(156).Width(210).
		Padding(6, 8).Radius(10).Background(ui.RGBA(20, 22, 28, 0.96)).
		Border(1, ui.RGBA(255, 255, 255, 0.12)).Gap(4).Children(func() {
		for i, ln := range st.Session.Source.Lines {
			if menuRow(c, fmt.Sprintf("lr-ln-%d", i), ln.Label, i == st.Session.LineIndex).Clicked() {
				st.ActiveMenu = ""
				if p.OnSelectLine != nil {
					p.OnSelectLine(i)
				}
			}
		}
	})
}

// SideOrOverlay renders the pure text-only live chat drawer (no gifts/superchats).
func (p *LiveRoomProfile) SideOrOverlay(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c.Key("lr-drawer")).Width(300).FillHeight().
		Background(ui.Hex("#14161b")).Border(1, ui.RGBA(255, 255, 255, 0.08)).
		Padding(14, 12, 12, 12).Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			if pillBtn(c, "lr-tab-chat", fmt.Sprintf("弹幕 (%d)", len(p.ChatList)), p.DrawerTab == 0).Clicked() {
				p.DrawerTab = 0
			}
			if pillBtn(c, "lr-tab-rooms", "直播间", p.DrawerTab == 1).Clicked() {
				p.DrawerTab = 1
			}
			ui.Spacer(c)
			if p.DrawerTab == 0 && p.OnClearChat != nil && pillBtn(c, "lr-clear-chat", "清空", false).Clicked() {
				p.OnClearChat()
			}
			if pillBtn(c, "lr-close-drawer", "收起", false).Clicked() {
				st.DrawerOpen = false
			}
		})

		if p.DrawerTab == 1 {
			if st.Session == nil || st.Session.Queue == nil || len(st.Session.Queue.Items) == 0 {
				ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
					ui.Text(c, "暂无可切换直播间").FontSize(12.5).TextColor(ui.RGBA(255, 255, 255, 0.5))
				})
				return
			}
			q := st.Session.Queue
			ui.List(c, &st.DrawerList, len(q.Items), func(i int) {
				it := q.Items[i]
				active := i == q.Current
				row := ui.ButtonBase(c.Key(fmt.Sprintf("lrm-%d-%s", i, it.Ref.ID))).
					FillWidth().Height(54).Padding(6, 8).Radius(7).Cursor(ui.CursorPointer)
				if active {
					row.Background(ui.RGBA(90, 150, 230, 0.25))
				} else if row.Hovered() {
					row.Background(ui.RGBA(255, 255, 255, 0.07))
				}
				row.Children(func() {
					ui.Row(c).Fill().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Box(c).Width(60).Height(34).Shrink(0).Radius(5).Clip().
							Background(ui.RGBA(255, 255, 255, 0.08)).Children(func() {
							if p.Images != nil && it.Poster != "" {
								if bmp, alpha := p.Images.Get(c, it.Poster); bmp != nil {
									ui.Image(c, bmp).Fill().Fit(ui.Cover).Opacity(alpha)
								}
							}
						})
						ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
							col := white
							if active {
								col = ui.Hex("#7cc0ff")
							}
							ui.Text(c, it.Title).FontSize(12.5).FontWeight(500).TextColor(col).SingleLine()
							sub := it.Subtitle
							if on := it.Extra["online"]; on != "" {
								sub += " · " + on + " 人气"
							}
							ui.Text(c, sub).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.55)).SingleLine()
						})
					})
				})
				if row.Clicked() && st.OnJumpQueue != nil {
					st.Touch(c.Now())
					st.OnJumpQueue(i)
				}
			}).Grow(1).FillWidth()
			return
		}

		if len(p.ChatList) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
				ui.Text(c, "等待弹幕消息…").FontSize(12.5).TextColor(ui.RGBA(255, 255, 255, 0.5))
			})
			return
		}

		p.ChatState.FollowEnd = true
		ui.List(c, &p.ChatState, len(p.ChatList), func(i int) {
			msg := p.ChatList[i]
			ui.Row(c.Key(fmt.Sprintf("lc-%d", i))).FillWidth().Padding(4, 6).Radius(6).
				Background(ui.RGBA(255, 255, 255, 0.03)).Gap(6).AlignItems(ui.Start).Children(func() {
				if msg.Author != "" {
					ui.Text(c, msg.Author+":").FontSize(11.5).FontWeight(600).
						TextColor(ui.Hex("#7cc0ff")).Shrink(0)
				}
				ui.Text(c, msg.Text).FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.9)).Grow(1).MinWidth(0)
			})
		}).Grow(1).FillWidth()
	})
}

func (p *LiveRoomProfile) BindShortcuts(c *ui.Context, st *PlayerViewState) {
	now := c.Now()
	touch := func() { st.Touch(now) }
	c.OnShortcut(0, ui.KeySpace, func() {
		touch()
		if st.OnTogglePlay != nil {
			st.OnTogglePlay()
		}
	})
	c.OnShortcut(0, ui.KeyM, func() {
		touch()
		if st.OnToggleMute != nil {
			st.OnToggleMute()
		}
	})
	c.OnShortcut(0, ui.KeyF, func() {
		touch()
		if st.OnToggleFullscreen != nil {
			st.OnToggleFullscreen()
		}
	})
	c.OnShortcut(0, ui.KeyD, func() {
		touch()
		if st.OnToggleDanmaku != nil {
			st.OnToggleDanmaku()
		}
	})
	c.OnShortcut(0, ui.KeyC, func() {
		touch()
		st.DrawerOpen = !st.DrawerOpen
	})
	c.OnShortcut(0, ui.KeyR, func() {
		touch()
		if p.OnRefreshStream != nil {
			p.OnRefreshStream()
		}
	})
	c.OnShortcut(0, ui.KeyPageUp, func() {
		touch()
		if st.OnPrev != nil {
			st.OnPrev()
		}
	})
	c.OnShortcut(0, ui.KeyPageDown, func() {
		touch()
		if st.OnNext != nil {
			st.OnNext()
		}
	})
	c.OnShortcut(0, ui.KeyEscape, func() {
		switch {
		case st.ActiveMenu != "":
			st.ActiveMenu = ""
		case st.DrawerOpen:
			st.DrawerOpen = false
		case st.Fullscreen && st.OnToggleFullscreen != nil:
			st.OnToggleFullscreen()
		case st.OnBack != nil:
			st.OnBack()
		}
	})
}
