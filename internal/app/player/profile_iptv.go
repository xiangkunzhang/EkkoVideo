package player

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/infra/imageloader"
)

const iptvDwellDelay = 1 * time.Second

// IPTVProfile implements PlayerProfile for live TV channels with EPG,
// multi-line switching, 1s dwell zapping, digit channel entry, and A/B recall.
type IPTVProfile struct {
	DrawerMode string // "channels" | "epg"
	NowProgram string
	EPGList    []core.Programme
	Images     *imageloader.Loader

	// Dwell zapping state
	PendingIdx   int
	PendingUntil time.Time
	DigitBuf     string
	DigitUntil   time.Time
	LastChannel  string // previous channel ID for Z recall

	OnSelectLine    func(lineIdx int)
	OnRefreshStream func()
	OnTuneIndex     func(idx int)
	OnRecallPrev    func()
	OnOpenEPG       func()
}

func NewIPTVProfile() *IPTVProfile {
	return &IPTVProfile{DrawerMode: "channels", PendingIdx: -1}
}

func (p *IPTVProfile) ID() string { return "iptv" }

func (p *IPTVProfile) TopBar(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	title := "IPTV"
	if st.Session != nil && st.Session.Source != nil && st.Session.Source.Title != "" {
		title = st.Session.Source.Title
	}
	if p.NowProgram != "" {
		title += " · 正在播出: " + p.NowProgram
	}
	leftPad := float32(16)
	if !st.Fullscreen {
		leftPad = 82
	}
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight+8).
		Padding(8, 16, 0, leftPad).AlignItems(ui.Center).Gap(10).
		Background(ui.RGBA(10, 12, 16, 0.58)).DragWindow().Children(func() {
		if iconBtn(c, "iptv-back", theme.IconBack, "返回", false).Clicked() && st.OnBack != nil {
			st.OnBack()
		}
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(white).SingleLine().Grow(1).MinWidth(0)
		if iconBtn(c, "iptv-pin", theme.IconPin, "置顶", st.AlwaysOnTop).Clicked() && st.OnTogglePin != nil {
			st.Touch(c.Now())
			st.OnTogglePin()
		}
		if pillBtn(c, "iptv-epg-btn", "节目单", st.DrawerOpen && p.DrawerMode == "epg").Clicked() {
			st.Touch(c.Now())
			if st.DrawerOpen && p.DrawerMode == "epg" {
				st.DrawerOpen = false
			} else {
				p.DrawerMode = "epg"
				st.DrawerOpen = true
				if p.OnOpenEPG != nil {
					p.OnOpenEPG()
				}
			}
		}
		if iconBtn(c, "iptv-ch-btn", theme.IconList, "频道列表", st.DrawerOpen && p.DrawerMode == "channels").Clicked() {
			st.Touch(c.Now())
			if st.DrawerOpen && p.DrawerMode == "channels" {
				st.DrawerOpen = false
			} else {
				p.DrawerMode = "channels"
				st.DrawerOpen = true
			}
		}
		if iconBtn(c, "iptv-fs", theme.IconFullscreen, "全屏", st.Fullscreen).Clicked() && st.OnToggleFullscreen != nil {
			st.Touch(c.Now())
			st.OnToggleFullscreen()
		}
	})
}

func (p *IPTVProfile) BottomBar(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c).Absolute().Bottom(14).Left(18).Right(18).
		Padding(10, 14).Radius(12).Background(ui.RGBA(12, 14, 18, 0.84)).
		Border(1, ui.RGBA(255, 255, 255, 0.1)).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			// Live badge + Volume
			ui.Box(c).Padding(2, 8).Radius(5).Background(ui.RGBA(229, 57, 53, 0.85)).Children(func() {
				ui.Text(c, "LIVE").FontSize(10.5).Bold().TextColor(white)
			})
			volIcon := theme.IconVolume
			if st.Snap.Muted || st.Snap.Volume <= 0 {
				volIcon = theme.IconMuted
			}
			if iconBtn(c, "iptv-mute", volIcon, "静音", st.Snap.Muted).Clicked() && st.OnToggleMute != nil {
				st.Touch(c.Now())
				st.OnToggleMute()
			}
			vol := st.Snap.Volume
			if vsl := ui.Slider(c.Key("iptv-vol"), &vol, 0, 100).Width(84); vsl.Changed() && st.OnVolume != nil {
				st.Touch(c.Now())
				st.OnVolume(vol)
			}

			ui.Spacer(c)

			// Center: CH- / Play-Pause / CH+
			if pillBtn(c, "iptv-ch-prev", "CH−", false).Clicked() && st.OnPrev != nil {
				st.Touch(c.Now())
				st.OnPrev()
			}
			playIc := theme.IconPause
			if st.Snap.Paused {
				playIc = theme.IconPlay
			}
			playB := ui.ButtonBase(c.Key("iptv-play")).Size(34, 34).Radius(17).
				Background(white).TextColor(ui.RGB(18, 20, 24)).Center().Cursor(ui.CursorPointer)
			playB.Children(func() { ui.Icon(c, playIc).Size(16, 16) })
			if playB.Clicked() && st.OnTogglePlay != nil {
				st.Touch(c.Now())
				st.OnTogglePlay()
			}
			if pillBtn(c, "iptv-ch-next", "CH+", false).Clicked() && st.OnNext != nil {
				st.Touch(c.Now())
				st.OnNext()
			}

			ui.Spacer(c)

			// Right: Line switcher, Refresh, Aspect, Screenshot
			lineLabel := "线路"
			if st.Session != nil && st.Session.Source != nil && len(st.Session.Source.Lines) > 0 {
				lineLabel = fmt.Sprintf("线路 %d/%d", st.Session.LineIndex+1, len(st.Session.Source.Lines))
			}
			if pillBtn(c, "iptv-lines", lineLabel, st.ActiveMenu == "lines").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "lines")
			}
			if pillBtn(c, "iptv-reload", "重连", false).Clicked() && p.OnRefreshStream != nil {
				st.Touch(c.Now())
				p.OnRefreshStream()
			}
			if pillBtn(c, "iptv-aspect", "比例", st.ActiveMenu == "aspect").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "aspect")
			}
			if iconBtn(c, "iptv-shot", theme.IconCamera, "截图", false).Clicked() && st.OnScreenshot != nil {
				st.Touch(c.Now())
				st.OnScreenshot()
			}
		})
	})
	if st.ActiveMenu != "" {
		p.renderIPTVMenu(c, st)
	}
}

func (p *IPTVProfile) renderIPTVMenu(c *ui.Context, st *PlayerViewState) {
	if st.ActiveMenu == "aspect" {
		renderPopupMenuAt(c, st, 72)
		return
	}
	if st.ActiveMenu != "lines" || st.Session == nil || st.Session.Source == nil {
		return
	}
	ui.Column(c.Key("iptv-line-menu")).Absolute().Bottom(72).Right(120).Width(196).
		Padding(6, 8).Radius(10).Background(ui.RGBA(20, 22, 28, 0.96)).
		Border(1, ui.RGBA(255, 255, 255, 0.12)).Gap(4).Children(func() {
		for i, ln := range st.Session.Source.Lines {
			if menuRow(c, fmt.Sprintf("ln-%d", i), ln.Label, i == st.Session.LineIndex).Clicked() {
				st.ActiveMenu = ""
				if p.OnSelectLine != nil {
					p.OnSelectLine(i)
				}
			}
		}
	})
}

func (p *IPTVProfile) SideOrOverlay(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c.Key("iptv-drawer")).Width(300).FillHeight().
		Background(ui.Hex("#14161b")).Border(1, ui.RGBA(255, 255, 255, 0.08)).
		Padding(14, 12, 12, 12).Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			if pillBtn(c, "tab-ch", "频道表", p.DrawerMode == "channels").Clicked() {
				p.DrawerMode = "channels"
			}
			if pillBtn(c, "tab-epg", "节目单", p.DrawerMode == "epg").Clicked() {
				p.DrawerMode = "epg"
				if p.OnOpenEPG != nil {
					p.OnOpenEPG()
				}
			}
			ui.Spacer(c)
			if pillBtn(c, "close-iptv-drawer", "收起", false).Clicked() {
				st.DrawerOpen = false
			}
		})

		if p.DrawerMode == "epg" {
			if len(p.EPGList) == 0 {
				ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
					ui.Text(c, "暂无节目单数据").FontSize(12.5).TextColor(ui.RGBA(255, 255, 255, 0.5))
				})
				return
			}
			now := time.Now()
			for i, pr := range p.EPGList {
				airing := !now.Before(pr.Start) && now.Before(pr.End)
				bg := ui.RGBA(255, 255, 255, 0.04)
				if airing {
					bg = ui.RGBA(90, 150, 230, 0.25)
				}
				ui.Column(c.Key(fmt.Sprintf("epg-%d", i))).FillWidth().Padding(8, 10).Radius(7).
					Background(bg).Gap(5).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Textf(c, "%s–%s", pr.Start.Format("15:04"), pr.End.Format("15:04")).
							FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.55)).FontFeatures("tnum").Width(82)
						col := ui.RGBA(255, 255, 255, 0.88)
						if airing {
							col = ui.Hex("#7cc0ff")
						}
						ui.Text(c, pr.Title).FontSize(12.5).TextColor(col).SingleLine().Grow(1).MinWidth(0)
					})
					if airing && pr.End.After(pr.Start) {
						total := pr.End.Sub(pr.Start).Seconds()
						elapsed := now.Sub(pr.Start).Seconds()
						pct := float32(max(0, min(1, elapsed/total)))
						remMin := max(1, int(pr.End.Sub(now).Minutes()+0.5))
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Box(c).Grow(1).MinWidth(0).Height(3).Radius(1.5).
								Background(ui.RGBA(255, 255, 255, 0.14)).Children(func() {
								ui.Box(c).Width(pct * 170).Height(3).Radius(1.5).Background(ui.Hex("#38BDF8"))
							})
							ui.Textf(c, "剩 %d 分钟", remMin).FontSize(10.5).TextColor(ui.Hex("#7cc0ff")).FontFeatures("tnum")
						})
					}
				})
			}
			return
		}

		// Channel list
		if st.Session == nil || st.Session.Queue == nil {
			return
		}
		q := st.Session.Queue
		ui.List(c, &st.DrawerList, len(q.Items), func(i int) {
			it := q.Items[i]
			active := i == q.Current
			row := ui.ButtonBase(c.Key(fmt.Sprintf("ich-%d-%s", i, it.Ref.ID))).
				FillWidth().Height(36).Padding(0, 10).Radius(7).Cursor(ui.CursorPointer)
			if active {
				row.Background(ui.RGBA(90, 150, 230, 0.25))
			} else if row.Hovered() {
				row.Background(ui.RGBA(255, 255, 255, 0.07))
			}
			row.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Textf(c, "%03d", it.Index).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.5)).FontFeatures("tnum").Width(28)
					if p.Images != nil && it.Poster != "" {
						ui.Box(c).Width(24).Height(18).Shrink(0).Radius(4).Clip().
							Background(ui.RGBA(255, 255, 255, 0.06)).Center().Children(func() {
							if bmp, alpha := p.Images.Get(c, it.Poster); bmp != nil {
								ui.Image(c, bmp).Fill().Fit(ui.Contain).Opacity(alpha)
							}
						})
					}
					col := white
					if active {
						col = ui.Hex("#7cc0ff")
					}
					ui.Text(c, it.Title).FontSize(12.5).TextColor(col).SingleLine().Grow(1).MinWidth(0)
				})
			})
			if row.Clicked() && st.OnJumpQueue != nil {
				st.Touch(c.Now())
				st.OnJumpQueue(i)
			}
		}).Grow(1).FillWidth()
	})
}

func (p *IPTVProfile) BindShortcuts(c *ui.Context, st *PlayerViewState) {
	now := c.Now()
	touch := func() { st.Touch(now) }

	// Process pending 1s dwell zapping
	if p.PendingIdx >= 0 {
		if !now.Before(p.PendingUntil) {
			idx := p.PendingIdx
			p.PendingIdx = -1
			if p.OnTuneIndex != nil {
				p.OnTuneIndex(idx)
			}
		} else {
			c.After(p.PendingUntil.Sub(now))
		}
	}
	// Process digit channel entry
	if p.DigitBuf != "" {
		if !now.Before(p.DigitUntil) {
			n, _ := strconv.Atoi(p.DigitBuf)
			p.DigitBuf = ""
			if n > 0 && p.OnTuneIndex != nil {
				p.OnTuneIndex(n - 1)
			}
		} else {
			c.After(p.DigitUntil.Sub(now))
		}
	}

	stepChannel := func(delta int) {
		if st.Session == nil || st.Session.Queue == nil || len(st.Session.Queue.Items) == 0 {
			return
		}
		n := len(st.Session.Queue.Items)
		cur := st.Session.Queue.Current
		if p.PendingIdx >= 0 {
			cur = p.PendingIdx
		}
		next := (cur + delta + n) % n
		p.PendingIdx = next
		p.PendingUntil = now.Add(iptvDwellDelay)
		it := st.Session.Queue.Items[next]
		st.ShowOSD(now, fmt.Sprintf("CH %03d · %s（1s后切换）", it.Index, it.Title))
	}

	c.OnShortcut(0, ui.KeyUp, func() { touch(); stepChannel(1) })
	c.OnShortcut(0, ui.KeyDown, func() { touch(); stepChannel(-1) })
	c.OnShortcut(0, ui.KeyEnter, func() {
		touch()
		if p.PendingIdx >= 0 && p.OnTuneIndex != nil {
			idx := p.PendingIdx
			p.PendingIdx = -1
			p.OnTuneIndex(idx)
		} else if p.DigitBuf != "" && p.OnTuneIndex != nil {
			n, _ := strconv.Atoi(p.DigitBuf)
			p.DigitBuf = ""
			if n > 0 {
				p.OnTuneIndex(n - 1)
			}
		} else if st.OnTogglePlay != nil {
			st.OnTogglePlay()
		}
	})
	c.OnShortcut(0, ui.KeyC, func() {
		touch()
		if st.DrawerOpen && p.DrawerMode == "channels" {
			st.DrawerOpen = false
		} else {
			p.DrawerMode = "channels"
			st.DrawerOpen = true
		}
	})
	c.OnShortcut(0, ui.KeyE, func() {
		touch()
		if st.DrawerOpen && p.DrawerMode == "epg" {
			st.DrawerOpen = false
		} else {
			p.DrawerMode = "epg"
			st.DrawerOpen = true
			if p.OnOpenEPG != nil {
				p.OnOpenEPG()
			}
		}
	})
	c.OnShortcut(0, ui.KeyZ, func() {
		touch()
		if p.OnRecallPrev != nil {
			p.OnRecallPrev()
		}
	})
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
	c.OnShortcut(0, ui.KeyR, func() {
		touch()
		if p.OnRefreshStream != nil {
			p.OnRefreshStream()
		}
	})
	c.OnShortcut(0, ui.KeyEscape, func() {
		touch()
		switch {
		case p.PendingIdx >= 0:
			p.PendingIdx = -1
			p.DigitBuf = ""
			st.OSDMessage = ""
		case st.DrawerOpen:
			st.DrawerOpen = false
		case st.ActiveMenu != "":
			st.ActiveMenu = ""
		case st.Fullscreen && st.OnToggleFullscreen != nil:
			st.OnToggleFullscreen()
		case st.OnBack != nil:
			st.OnBack()
		}
	})
	c.OnShortcut(0, ui.KeyEqual, func() {
		touch()
		if st.OnVolume != nil {
			st.OnVolume(min(100, st.Snap.Volume+5))
		}
	})
	c.OnShortcut(0, ui.KeyMinus, func() {
		touch()
		if st.OnVolume != nil {
			st.OnVolume(max(0, st.Snap.Volume-5))
		}
	})

	digits := []struct {
		k ui.Key
		d string
	}{
		{ui.Key0, "0"}, {ui.Key1, "1"}, {ui.Key2, "2"}, {ui.Key3, "3"}, {ui.Key4, "4"},
		{ui.Key5, "5"}, {ui.Key6, "6"}, {ui.Key7, "7"}, {ui.Key8, "8"}, {ui.Key9, "9"},
	}
	for _, dg := range digits {
		d := dg.d
		c.OnShortcut(0, dg.k, func() {
			touch()
			if len(p.DigitBuf) < 4 {
				p.DigitBuf += d
				p.DigitUntil = now.Add(1200 * time.Millisecond)
				st.ShowOSD(now, "频道号: "+p.DigitBuf)
			}
		})
	}
}
