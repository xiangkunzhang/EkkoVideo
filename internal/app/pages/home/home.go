// Package home renders the Velan landing page with quick actions and watch history.
package home

import (
	"fmt"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
)

type Actions struct {
	OnOpenFile   func()
	OnOpenFolder func()
	OnOpenURL    func()
	OnResume     func(rec core.WatchRecord)
	OnBrowse     func()
}

func Page(c *ui.Context, records []core.WatchRecord, listState *ui.ListState, act Actions) {
	t := c.Theme()
	topPad := float32(theme.TitleBarHeight + 16)
	ui.Box(c).Fill().Children(func() {
		ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Gap(18).Children(func() {
			// Brand header
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				if badge := theme.BrandBadge(); badge != nil {
					ui.Box(c).Size(36, 36).Radius(9).Clip().Children(func() {
						ui.Image(c, badge).Size(36, 36)
					})
				}
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, "Velan · 微澜").FontSize(17).FontWeight(600)
					ui.Text(c, "本地影音 · 在线短剧 · 原生弹幕 · IPTV").FontSize(12).TextColor(t.TextMuted)
				})
			})

			// Quick launch strip
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c.Key("home-open-file"), "打开视频文件…").Clicked() && act.OnOpenFile != nil {
					act.OnOpenFile()
				}
				if ui.Button(c.Key("home-open-folder"), "打开文件夹…").Clicked() && act.OnOpenFolder != nil {
					act.OnOpenFolder()
				}
				if ui.Button(c.Key("home-open-url"), "打开网络 URL…").Clicked() && act.OnOpenURL != nil {
					act.OnOpenURL()
				}
				if ui.Button(c.Key("home-browse"), "浏览本地目录").Clicked() && act.OnBrowse != nil {
					act.OnBrowse()
				}
			})

			ui.Text(c, "最近观看").FontSize(16).FontWeight(600)

			if len(records) == 0 {
				ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
					if mark := theme.BrandMark(); mark != nil {
						ui.Image(c, mark).Size(56, 56).Opacity(0.85)
					} else {
						ui.Icon(c, theme.IconFilm).Size(36, 36).TextColor(t.TextMuted.Alpha(0.5))
					}
					ui.Text(c, "暂无播放记录").FontSize(14).TextColor(t.TextMuted)
					ui.Text(c, "打开本地视频、拖入文件，或输入网络流地址开始播放").FontSize(12).TextColor(t.TextMuted.Alpha(0.75))
				})
			} else {
				ui.List(c, listState, len(records), func(i int) {
					rec := records[i]
					row := ui.ButtonBase(c.Key(rec.Ref.Source+":"+rec.Ref.ID)).FillWidth().Height(46).
						Padding(0, 12).Radius(8).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(t.Text.Alpha(0.06))
					}
					row.Children(func() {
						ui.Row(c).Fill().AlignItems(ui.Center).Gap(12).Children(func() {
							ui.Icon(c, theme.IconPlay).Size(14, 14).TextColor(t.Accent).Shrink(0)
							ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
								ui.Text(c, rec.Title).FontSize(13.5).FontWeight(500).SingleLine()
								sub := formatProgress(rec)
								ui.Text(c, sub).FontSize(11.5).TextColor(t.TextMuted).FontFeatures("tnum").SingleLine()
							})
						})
					})
					if row.Clicked() && act.OnResume != nil {
						act.OnResume(rec)
					}
				}).Grow(1).FillWidth()
			}
		})

		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight).
			PassThrough().Material(glass.ScrollEdge{})
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight).
			Padding(0, 24).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, "首页").FontSize(20).Bold()
		})
	})
}

func formatProgress(rec core.WatchRecord) string {
	if rec.Completed {
		return "已看完 · " + formatSec(rec.Duration)
	}
	if rec.Duration > 0 {
		return fmt.Sprintf("看到 %s / %s", formatSec(rec.Position), formatSec(rec.Duration))
	}
	return fmt.Sprintf("看到 %s", formatSec(rec.Position))
}

func formatSec(sec float64) string {
	s := int(sec)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
