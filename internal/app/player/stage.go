// Package player implements the shared PlayerStage and mode-specific PlayerProfile slots.
package player

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/danmaku"
	"github.com/xiangkunzhang/Velan/internal/engine/mpv"
	"github.com/xiangkunzhang/Velan/internal/engine/surface"
)

const controlsHideDelay = 2500 * time.Millisecond

// PlayerViewState holds main-thread UI state and callbacks shared between PlayerStage and Profiles.
type PlayerViewState struct {
	Session        *core.PlaybackSession
	Snap           mpv.Snapshot
	Seeking        bool
	SeekTarget     float64
	AspectRatio    string // auto | 16:9 | 4:3 | fill
	AlwaysOnTop    bool
	Fullscreen     bool
	DrawerOpen     bool
	ActiveMenu     string // "" | "audio" | "sub" | "aspect"
	DrawerList     ui.ListState
	LastActivity   time.Time
	OSDMessage     string
	OSDUntil       time.Time
	PointerInside  bool
	LastPointerX   float32
	LastPointerY   float32
	BufferingSince time.Time

	DanmakuSched *danmaku.Scheduler
	DanmakuRend  *danmaku.Renderer
	DanmakuCfg   danmaku.Config
	SnapAt       time.Time

	OnBack             func()
	OnTogglePlay       func()
	OnSeek             func(sec float64)
	OnSeekRel          func(delta float64)
	OnVolume           func(vol float64)
	OnToggleMute       func()
	OnSpeed            func(speed float64)
	OnNext             func()
	OnPrev             func()
	OnJumpQueue        func(idx int)
	OnSelectTrack      func(typ string, id int64)
	OnCycleSub         func()
	OnCycleAudio       func()
	OnAspect           func(ratio string)
	OnScreenshot       func()
	OnToggleFullscreen func()
	OnTogglePin        func()
	OnToggleDanmaku    func()
	OnDanmakuChanged   func()
	OnLoadDanmakuFile  func()
	OnLoadSubtitleFile func()
	OnRetry            func()
}

// PlayerProfile supplies mode-specific UI slots and shortcuts to PlayerStage.
type PlayerProfile interface {
	ID() string
	TopBar(c *ui.Context, st *PlayerViewState)
	BottomBar(c *ui.Context, st *PlayerViewState)
	SideOrOverlay(c *ui.Context, st *PlayerViewState)
	BindShortcuts(c *ui.Context, st *PlayerViewState)
}

func (st *PlayerViewState) Touch(now time.Time) {
	st.LastActivity = now
}

func (st *PlayerViewState) ShowOSD(now time.Time, msg string) {
	st.OSDMessage = msg
	st.OSDUntil = now.Add(2 * time.Second)
}

func (st *PlayerViewState) ChromeVisible(now time.Time) bool {
	if st.Snap.Paused || st.DrawerOpen || st.ActiveMenu != "" || st.Seeking {
		return true
	}
	if st.Session != nil && (st.Session.Phase == core.PhaseResolving || st.Session.Phase == core.PhaseLoading || st.Session.Phase == core.PhaseRecovering || st.Session.Phase == core.PhaseFailed) {
		return true
	}
	if st.LastActivity.IsZero() {
		return true
	}
	return now.Sub(st.LastActivity) < controlsHideDelay
}

// Stage renders the shared video canvas, status overlays, OSD, and delegates chrome slots to prof.
func Stage(c *ui.Context, st *PlayerViewState, prof PlayerProfile, surf surface.VideoSurface, sideOffset float32) {
	now := c.Now()
	winW, winH := c.Size()
	drawerW := float32(0)
	if st.DrawerOpen {
		drawerW = 280
	}
	videoW := max(1, winW-sideOffset-drawerW)
	if surf != nil {
		surf.SetBounds(ui.Rect{X: sideOffset, Y: 0, W: videoW, H: winH}, float64(winH))
		surf.Show(true)
	}

	chrome := st.ChromeVisible(now)
	if chrome && !st.Snap.Paused && st.ActiveMenu == "" && !st.DrawerOpen {
		rem := controlsHideDelay - now.Sub(st.LastActivity)
		if rem > 0 {
			c.After(rem)
		}
	}

	ui.Row(c).Fill().Children(func() {
		// Video viewport area (transparent so native GL surface shows through)
		canvas := ui.Box(c.Key("player-canvas")).Grow(1).FillHeight().Background(ui.Transparent)
		if px, py, ok := canvas.PointerPosition(); ok {
			st.PointerInside = true
			inHotzone := py <= 58 || py >= winH-98
			moved := math.Abs(float64(px-st.LastPointerX)) >= 2 || math.Abs(float64(py-st.LastPointerY)) >= 2
			if moved || inHotzone {
				st.LastPointerX, st.LastPointerY = px, py
				st.Touch(now)
			}
		} else {
			st.PointerInside = false
		}
		if canvas.DoubleClicked() {
			st.Touch(now)
			if st.OnToggleFullscreen != nil {
				st.OnToggleFullscreen()
			}
		} else if canvas.Clicked() {
			st.Touch(now)
			if st.ActiveMenu != "" {
				st.ActiveMenu = ""
			} else if st.OnTogglePlay != nil {
				st.OnTogglePlay()
			}
		}

		canvas.Children(func() {
			// Native 2x danmaku layer strictly clipped to videoContentRect
			if st.DanmakuSched != nil && st.DanmakuRend != nil && st.DanmakuCfg.Enabled {
				posSec := st.Snap.Position
				if !st.Snap.Paused && !st.SnapAt.IsZero() && st.Snap.Loaded {
					spd := st.Snap.Speed
					if spd <= 0 {
						spd = 1
					}
					posSec += now.Sub(st.SnapAt).Seconds() * spd
				}
				if !st.Snap.Paused {
					vr := danmaku.VideoContentRect(ui.Rect{W: videoW, H: winH}, st.Snap.VideoWidth, st.Snap.VideoHeight, st.AspectRatio)
					st.DanmakuSched.Step(posSec, vr.W, vr.H, st.DanmakuCfg, st.DanmakuRend.MakeSprite)
				}
				if len(st.DanmakuSched.Active) > 0 && !st.Snap.Paused {
					c.Invalidate()
				}
				ui.Box(c.Key("danmaku-layer")).Absolute().Fill().PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
					vr := danmaku.VideoContentRect(r, st.Snap.VideoWidth, st.Snap.VideoHeight, st.AspectRatio)
					danmaku.Draw(p, vr, st.DanmakuSched, posSec, st.DanmakuCfg)
				})
			}

			// Phase overlays: Loading / Buffering / Failed
			if st.Session != nil {
				switch st.Session.Phase {
				case core.PhaseResolving, core.PhaseLoading:
					ui.Column(c).Absolute().Fill().Center().Background(ui.Hex("#0B101B")).Children(func() {
						theme.BrandLoader(c, 84, "正在准备播放…", ui.RGBA(255, 255, 255, 0.85), true)
					})
				case core.PhaseRecovering:
					ui.Column(c).Absolute().Fill().Center().PassThrough().Children(func() {
						ui.Box(c).Padding(14, 22).Radius(14).Background(ui.RGBA(10, 14, 22, 0.72)).Children(func() {
							theme.BrandLoader(c, 58, "连接断开，正在重连…", ui.RGBA(255, 255, 255, 0.90), true)
						})
					})
				case core.PhaseBuffering:
					if !st.BufferingSince.IsZero() && now.Sub(st.BufferingSince) < 250*time.Millisecond {
						c.After(250*time.Millisecond - now.Sub(st.BufferingSince))
					} else {
						ui.Column(c).Absolute().Fill().Center().PassThrough().Children(func() {
							ui.Box(c).Padding(14, 22).Radius(14).Background(ui.RGBA(10, 14, 22, 0.62)).Children(func() {
								theme.BrandLoader(c, 58, "正在缓冲…", ui.RGBA(255, 255, 255, 0.88), true)
							})
						})
					}
				case core.PhaseFailed:
					ui.Column(c).Absolute().Fill().Center().Gap(10).Background(ui.RGBA(0, 0, 0, 0.72)).Children(func() {
						ui.Text(c, "播放失败").FontSize(16).Bold().TextColor(ui.RGB(255, 255, 255))
						if st.Session.LastError != "" {
							ui.Text(c, st.Session.LastError).FontSize(12.5).TextColor(ui.RGBA(255, 255, 255, 0.7)).MaxWidth(420).TextAlign(ui.Center)
						}
						if ui.PrimaryButton(c.Key("player-retry"), "重试").Clicked() && st.OnRetry != nil {
							st.OnRetry()
						}
					})
				}
			}

			// OSD Pill
			if st.OSDMessage != "" && now.Before(st.OSDUntil) {
				c.After(st.OSDUntil.Sub(now))
				ui.Row(c).Absolute().Top(58).Left(24).Padding(6, 12).Radius(8).
					Background(ui.RGBA(12, 14, 18, 0.78)).PassThrough().Children(func() {
					ui.Text(c, st.OSDMessage).FontSize(12.5).FontWeight(500).TextColor(ui.RGB(255, 255, 255))
				})
			}

			if chrome {
				prof.TopBar(c, st)
				prof.BottomBar(c, st)
			}
		})

		if st.DrawerOpen {
			prof.SideOrOverlay(c, st)
		}
	})

	prof.BindShortcuts(c, st)
}
