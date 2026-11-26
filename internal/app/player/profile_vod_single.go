package player

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/theme"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/mpv"
)

// VODSingleProfile implements PlayerProfile for local files and single VOD playback.
type VODSingleProfile struct{}

func NewVODSingleProfile() *VODSingleProfile { return &VODSingleProfile{} }

func (p *VODSingleProfile) ID() string { return "vod_single" }

func (p *VODSingleProfile) TopBar(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	title := "EkkoVideo"
	if st.Session != nil && st.Session.Source != nil && st.Session.Source.Title != "" {
		title = st.Session.Source.Title
	}
	leftPad := float32(16)
	if !st.Fullscreen {
		leftPad = 82 // Clear macOS traffic lights when sidebar is collapsed
	}
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight+8).
		Padding(8, 16, 0, leftPad).AlignItems(ui.Center).Gap(10).
		Background(ui.RGBA(10, 12, 16, 0.58)).DragWindow().Children(func() {
		if iconBtn(c, "top-back", theme.IconBack, "返回", false).Clicked() && st.OnBack != nil {
			st.OnBack()
		}
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(white).SingleLine().Grow(1).MinWidth(0)
		if iconBtn(c, "top-pin", theme.IconPin, "置顶", st.AlwaysOnTop).Clicked() && st.OnTogglePin != nil {
			st.Touch(c.Now())
			st.OnTogglePin()
		}
		if iconBtn(c, "top-list", theme.IconList, "播放列表", st.DrawerOpen).Clicked() {
			st.Touch(c.Now())
			st.DrawerOpen = !st.DrawerOpen
		}
		if iconBtn(c, "top-fs", theme.IconFullscreen, "全屏", st.Fullscreen).Clicked() && st.OnToggleFullscreen != nil {
			st.Touch(c.Now())
			st.OnToggleFullscreen()
		}
	})
}

func (p *VODSingleProfile) BottomBar(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	dur := st.Snap.Duration
	pos := st.Snap.Position
	if st.Seeking {
		pos = st.SeekTarget
	}

	ui.Column(c).Absolute().Bottom(14).Left(18).Right(18).
		Padding(10, 14).Radius(12).Background(ui.RGBA(12, 14, 18, 0.84)).
		Border(1, ui.RGBA(255, 255, 255, 0.1)).Gap(8).Children(func() {
		// Track / Aspect popup menu anchored above bottom bar
		if st.ActiveMenu != "" {
			renderPopupMenu(c, st)
		}

		// Seek bar
		if dur > 0 {
			val := pos
			sl := ui.Slider(c.Key("vod-seek"), &val, 0, dur).FillWidth()
			if sl.Changed() {
				st.Touch(c.Now())
				st.Seeking = true
				st.SeekTarget = val
			}
			if !sl.Pressed() && st.Seeking {
				st.Seeking = false
				if st.OnSeek != nil {
					st.OnSeek(st.SeekTarget)
				}
			}
		}

		// Transport row (left clock, center transport, right tools)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			// Left: time + volume
			ui.Textf(c, "%s / %s", fmtClock(pos), fmtClock(dur)).
				FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.85)).FontFeatures("tnum").Width(105)

			volIcon := theme.IconVolume
			if st.Snap.Muted || st.Snap.Volume <= 0 {
				volIcon = theme.IconMuted
			}
			if iconBtn(c, "mute-btn", volIcon, "静音", st.Snap.Muted).Clicked() && st.OnToggleMute != nil {
				st.Touch(c.Now())
				st.OnToggleMute()
			}
			vol := st.Snap.Volume
			if vsl := ui.Slider(c.Key("vod-vol"), &vol, 0, 100).Width(78); vsl.Changed() && st.OnVolume != nil {
				st.Touch(c.Now())
				st.OnVolume(vol)
			}

			ui.Spacer(c)

			// Center: Prev / Play-Pause / Next
			hasPrev := st.Session != nil && st.Session.Queue != nil && st.Session.Queue.HasPrev()
			hasNext := st.Session != nil && st.Session.Queue != nil && st.Session.Queue.HasNext()
			prevB := iconBtn(c, "prev-btn", theme.IconPrev, "上一集", false)
			if !hasPrev {
				prevB.Disabled(true)
			}
			if prevB.Clicked() && hasPrev && st.OnPrev != nil {
				st.Touch(c.Now())
				st.OnPrev()
			}

			playIc := theme.IconPause
			if st.Snap.Paused {
				playIc = theme.IconPlay
			}
			playB := ui.ButtonBase(c.Key("play-btn")).Size(34, 34).Radius(17).
				Background(white).TextColor(ui.RGB(18, 20, 24)).Center().Cursor(ui.CursorPointer)
			playB.Children(func() {
				ui.Icon(c, playIc).Size(16, 16)
			})
			if playB.Clicked() && st.OnTogglePlay != nil {
				st.Touch(c.Now())
				st.OnTogglePlay()
			}

			nextB := iconBtn(c, "next-btn", theme.IconNext, "下一集", false)
			if !hasNext {
				nextB.Disabled(true)
			}
			if nextB.Clicked() && hasNext && st.OnNext != nil {
				st.Touch(c.Now())
				st.OnNext()
			}

			ui.Spacer(c)

			// Right: Speed, Audio, Subtitles, Aspect, Screenshot
			spdLabel := fmt.Sprintf("%.2gx", st.Snap.Speed)
			if pillBtn(c, "speed-btn", spdLabel, st.Snap.Speed != 1.0).Clicked() && st.OnSpeed != nil {
				st.Touch(c.Now())
				next := 1.0
				switch {
				case st.Snap.Speed < 1.2:
					next = 1.25
				case st.Snap.Speed < 1.45:
					next = 1.5
				case st.Snap.Speed < 1.9:
					next = 2.0
				default:
					next = 1.0
				}
				st.OnSpeed(next)
			}
			if iconBtn(c, "audio-btn", theme.IconAudio, "音轨", st.ActiveMenu == "audio").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "audio")
			}
			if iconBtn(c, "sub-btn", theme.IconSubtitles, "字幕", st.ActiveMenu == "sub").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "sub")
			}
			aspectLabel := "比例"
			if st.AspectRatio != "" && st.AspectRatio != "auto" {
				aspectLabel = st.AspectRatio
			}
			if pillBtn(c, "aspect-btn", aspectLabel, st.ActiveMenu == "aspect").Clicked() {
				st.Touch(c.Now())
				toggleMenu(st, "aspect")
			}
			if iconBtn(c, "shot-btn", theme.IconCamera, "截图", false).Clicked() && st.OnScreenshot != nil {
				st.Touch(c.Now())
				st.OnScreenshot()
			}
		})
	})
}

func (p *VODSingleProfile) SideOrOverlay(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c.Key("vod-playlist-drawer")).Width(280).FillHeight().
		Background(ui.Hex("#14161b")).Border(1, ui.RGBA(255, 255, 255, 0.08)).
		Padding(14, 12, 12, 12).Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "播放列表").FontSize(14).FontWeight(600).TextColor(white)
			ui.Spacer(c)
			if pillBtn(c, "close-drawer", "收起", false).Clicked() {
				st.DrawerOpen = false
			}
		})
		if st.Session == nil || st.Session.Queue == nil || len(st.Session.Queue.Items) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
				ui.Text(c, "当前仅单文件").FontSize(12.5).TextColor(ui.RGBA(255, 255, 255, 0.5))
			})
			return
		}
		q := st.Session.Queue
		ui.List(c, &st.DrawerList, len(q.Items), func(i int) {
			it := q.Items[i]
			active := i == q.Current
			row := ui.ButtonBase(c.Key(fmt.Sprintf("q-%d-%s", i, it.Ref.ID))).
				FillWidth().Height(34).Padding(0, 10).Radius(7).Cursor(ui.CursorPointer)
			if active {
				row.Background(ui.RGBA(90, 150, 230, 0.25))
			} else if row.Hovered() {
				row.Background(ui.RGBA(255, 255, 255, 0.07))
			}
			row.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Textf(c, "%d", i+1).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.45)).FontFeatures("tnum").Width(22)
					col := ui.RGBA(255, 255, 255, 0.88)
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

func (p *VODSingleProfile) BindShortcuts(c *ui.Context, st *PlayerViewState) {
	touch := func() { st.Touch(c.Now()) }
	c.OnShortcut(0, ui.KeySpace, func() {
		touch()
		if st.OnTogglePlay != nil {
			st.OnTogglePlay()
		}
	})
	c.OnShortcut(0, ui.KeyLeft, func() {
		touch()
		if st.OnSeekRel != nil {
			st.OnSeekRel(-5)
		}
	})
	c.OnShortcut(0, ui.KeyRight, func() {
		touch()
		if st.OnSeekRel != nil {
			st.OnSeekRel(5)
		}
	})
	c.OnShortcut(ui.Shift, ui.KeyLeft, func() {
		touch()
		if st.OnSeekRel != nil {
			st.OnSeekRel(-30)
		}
	})
	c.OnShortcut(ui.Shift, ui.KeyRight, func() {
		touch()
		if st.OnSeekRel != nil {
			st.OnSeekRel(30)
		}
	})
	c.OnShortcut(0, ui.KeyUp, func() {
		touch()
		if st.OnVolume != nil {
			st.OnVolume(min(100, st.Snap.Volume+5))
		}
	})
	c.OnShortcut(0, ui.KeyDown, func() {
		touch()
		if st.OnVolume != nil {
			st.OnVolume(max(0, st.Snap.Volume-5))
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
	c.OnShortcut(0, ui.KeyEscape, func() {
		touch()
		switch {
		case st.ActiveMenu != "":
			st.ActiveMenu = ""
		case st.Fullscreen && st.OnToggleFullscreen != nil:
			st.OnToggleFullscreen()
		case st.OnBack != nil:
			st.OnBack()
		}
	})
	c.OnShortcut(0, ui.KeyT, func() {
		touch()
		if st.OnTogglePin != nil {
			st.OnTogglePin()
		}
	})
	c.OnShortcut(0, ui.KeyL, func() {
		touch()
		st.DrawerOpen = !st.DrawerOpen
	})
	c.OnShortcut(0, ui.KeyV, func() {
		touch()
		if st.OnCycleSub != nil {
			st.OnCycleSub()
		}
	})
	c.OnShortcut(0, ui.KeyA, func() {
		touch()
		if st.OnCycleAudio != nil {
			st.OnCycleAudio()
		}
	})
	c.OnShortcut(0, ui.KeyS, func() {
		touch()
		if st.OnScreenshot != nil {
			st.OnScreenshot()
		}
	})
	c.OnShortcut(0, ui.KeyBracketLeft, func() {
		touch()
		if st.OnSpeed != nil {
			st.OnSpeed(max(0.5, st.Snap.Speed-0.25))
		}
	})
	c.OnShortcut(0, ui.KeyBracketRight, func() {
		touch()
		if st.OnSpeed != nil {
			st.OnSpeed(min(3.0, st.Snap.Speed+0.25))
		}
	})
	c.OnShortcut(0, ui.KeyBackslash, func() {
		touch()
		if st.OnSpeed != nil {
			st.OnSpeed(1.0)
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
}

func toggleMenu(st *PlayerViewState, name string) {
	if st.ActiveMenu == name {
		st.ActiveMenu = ""
	} else {
		st.ActiveMenu = name
	}
}

func renderPopupMenu(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c.Key("popup-menu")).FillWidth().Padding(6, 8).Radius(8).
		Background(ui.RGBA(26, 28, 34, 0.96)).Border(1, ui.RGBA(255, 255, 255, 0.1)).Gap(4).Children(func() {
		switch st.ActiveMenu {
		case "aspect":
			for _, opt := range []struct{ id, label string }{
				{"auto", "原始比例"}, {"16:9", "16:9"}, {"4:3", "4:3"}, {"fill", "填充裁切"},
			} {
				active := st.AspectRatio == opt.id || (st.AspectRatio == "" && opt.id == "auto")
				if menuRow(c, "asp-"+opt.id, opt.label, active).Clicked() {
					st.ActiveMenu = ""
					if st.OnAspect != nil {
						st.OnAspect(opt.id)
					}
				}
			}
		case "audio", "sub":
			tracks := filterTracks(st.Snap.Tracks, st.ActiveMenu)
			if len(tracks) == 0 {
				ui.Text(c, "无可用轨道").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.6)).Padding(4, 6)
				return
			}
			for _, tr := range tracks {
				label := tr.Title
				if label == "" {
					label = fmt.Sprintf("轨道 #%d (%s)", tr.ID, tr.Lang)
				} else if tr.Lang != "" {
					label = fmt.Sprintf("%s [%s]", label, tr.Lang)
				}
				if menuRow(c, fmt.Sprintf("tr-%s-%d", tr.Type, tr.ID), label, tr.Selected).Clicked() {
					st.ActiveMenu = ""
					if st.OnSelectTrack != nil {
						st.OnSelectTrack(tr.Type, tr.ID)
					}
				}
			}
			if st.ActiveMenu == "sub" {
				if menuRow(c, "sub-off", "关闭字幕", false).Clicked() {
					st.ActiveMenu = ""
					if st.OnSelectTrack != nil {
						st.OnSelectTrack("sub", 0)
					}
				}
			}
		}
		_ = white
	})
}

func filterTracks(tracks []mpv.Track, typ string) []mpv.Track {
	var out []mpv.Track
	for _, t := range tracks {
		if t.Type == typ {
			out = append(out, t)
		}
	}
	return out
}

func menuRow(c *ui.Context, key, label string, active bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).FillWidth().Height(28).Padding(0, 10).Radius(6).Cursor(ui.CursorPointer)
	if active {
		b.Background(ui.RGBA(90, 150, 230, 0.28))
	} else if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.08))
	}
	b.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(12).TextColor(theme.White()).SingleLine()
		})
	})
	return b
}

func iconBtn(c *ui.Context, key string, ic *ui.SVG, label string, active bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(7).Label(label).Center().Cursor(ui.CursorPointer)
	if active {
		b.Background(ui.RGBA(255, 255, 255, 0.2)).TextColor(ui.Hex("#7cc0ff"))
	} else if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.14)).TextColor(theme.White())
	} else {
		b.TextColor(ui.RGBA(255, 255, 255, 0.88))
	}
	b.Children(func() {
		ui.Icon(c, ic).Size(16, 16)
	})
	return b
}

func pillBtn(c *ui.Context, key, text string, active bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(26).Padding(0, 8).Radius(6).Center().Cursor(ui.CursorPointer)
	if active {
		b.Background(ui.RGBA(255, 255, 255, 0.2)).TextColor(ui.Hex("#7cc0ff"))
	} else if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.14)).TextColor(theme.White())
	} else {
		b.TextColor(ui.RGBA(255, 255, 255, 0.88))
	}
	b.Children(func() {
		ui.Text(c, text).FontSize(11.5).FontWeight(500).FontFeatures("tnum")
	})
	return b
}

func fmtClock(sec float64) string {
	s := int(sec)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
