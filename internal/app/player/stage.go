// Package player implements the shared PlayerStage and mode-specific PlayerProfile slots.
package player

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/mpv"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/surface"
)

const controlsHideDelay = 2500 * time.Millisecond

// PlayerViewState holds main-thread UI state and callbacks shared between PlayerStage and Profiles.
type PlayerViewState struct {
	Session       *core.PlaybackSession
	Snap          mpv.Snapshot
	Seeking       bool
	SeekTarget    float64
	AspectRatio   string // auto | 16:9 | 4:3 | fill
	AlwaysOnTop   bool
	Fullscreen    bool
	DrawerOpen    bool
	ActiveMenu    string // "" | "audio" | "sub" | "aspect"
	DrawerList    ui.ListState
	LastActivity  time.Time
	OSDMessage    string
	OSDUntil      time.Time
	PointerInside bool

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
	if st.Session != nil && (st.Session.Phase == core.PhaseResolving || st.Session.Phase == core.PhaseLoading || st.Session.Phase == core.PhaseFailed) {
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
		if canvas.Hovered() {
			// Detect pointer movement/hover without spamming every frame unless moved
			if now.Sub(st.LastActivity) > 300*time.Millisecond && st.PointerInside {
				// Keep alive only on real interaction; mouse click/scroll/key wakes it
			}
			st.PointerInside = true
		} else {
			st.PointerInside = false
		}
		if canvas.Clicked() {
			st.Touch(now)
			if st.ActiveMenu != "" {
				st.ActiveMenu = ""
			} else if st.OnTogglePlay != nil {
				st.OnTogglePlay()
			}
		}

		canvas.Children(func() {
			// Phase overlays: Loading / Buffering / Failed
			if st.Session != nil {
				switch st.Session.Phase {
				case core.PhaseResolving, core.PhaseLoading:
					ui.Column(c).Fill().Center().Gap(10).Children(func() {
						ui.Spinner(c).FontSize(26).Label("正在加载")
						ui.Text(c, "正在准备播放…").FontSize(13).TextColor(ui.RGBA(255, 255, 255, 0.85))
					})
				case core.PhaseBuffering:
					ui.Column(c).Fill().Center().Gap(8).PassThrough().Children(func() {
						ui.Spinner(c).FontSize(24).Label("缓冲中")
						ui.Text(c, "正在缓冲…").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.8))
					})
				case core.PhaseFailed:
					ui.Column(c).Fill().Center().Gap(10).Background(ui.RGBA(0, 0, 0, 0.72)).Children(func() {
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
			} else {
				// Invisible hover strip at bottom to wake controls
				wake := ui.Box(c.Key("player-wake-bottom")).Absolute().Bottom(0).Left(0).Right(0).Height(36).PassThrough()
				if wake.Hovered() {
					st.Touch(now)
				}
			}
		})

		if st.DrawerOpen {
			prof.SideOrOverlay(c, st)
		}
	})

	prof.BindShortcuts(c, st)
}
