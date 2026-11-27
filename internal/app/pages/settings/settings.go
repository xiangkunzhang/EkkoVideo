// Package settings renders Velan's preferences and diagnostics page.
package settings

import (
	"fmt"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
	gwclient "github.com/xiangkunzhang/Velan/internal/gateway/client"
)

type Diagnostics struct {
	AppVersion string
	MpvPath    string
	Hwdec      string
	Gateway    gwclient.Status
}

type Actions struct {
	OnSave            func()
	OnSimulateGWCrash func()
}

func Page(c *ui.Context, cfg *core.Settings, diag Diagnostics, act Actions) {
	t := c.Theme()
	topPad := float32(theme.TitleBarHeight + 16)
	ui.Box(c).Fill().Children(func() {
		ui.Scroll(c.Key("settings-scroll")).Fill().Padding(topPad, 24, 32, 24).ScrollbarInsets(theme.TitleBarHeight, 0, 0, 0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(720).Gap(20).Children(func() {
				// Playback preferences
				section(c, "播放设置", func() {
					row(c, "默认音量", fmt.Sprintf("%.0f%%", cfg.Volume), func() {
						v := cfg.Volume
						sl := ui.Slider(c.Key("cfg-vol"), &v, 0, 100).Width(170)
						if sl.Changed() {
							cfg.Volume = v
							if act.OnSave != nil {
								act.OnSave()
							}
						}
					})
					row(c, "自动播放下一集", "目录播放列表播完自动切换", func() {
						v := cfg.AutoNext
						if ui.Switch(c.Key("cfg-autonext"), &v).Changed() {
							cfg.AutoNext = v
							if act.OnSave != nil {
								act.OnSave()
							}
						}
					})
					row(c, "硬件解码", "auto-safe（推荐）或 no（软解）", func() {
						label := "自动硬解 (auto-safe)"
						if cfg.Hwdec == "no" {
							label = "软件解码 (no)"
						}
						if ui.Button(c.Key("cfg-hwdec"), label).Clicked() {
							if cfg.Hwdec == "no" {
								cfg.Hwdec = "auto-safe"
							} else {
								cfg.Hwdec = "no"
							}
							if act.OnSave != nil {
								act.OnSave()
							}
						}
					})
					row(c, "显示更多在线平台", "开启后在侧边栏显示红果之外的其他在线短剧平台", func() {
						v := cfg.ShowExtraOnlinePlatforms
						if ui.Switch(c.Key("cfg-extra-platforms"), &v).Changed() {
							cfg.ShowExtraOnlinePlatforms = v
							if act.OnSave != nil {
								act.OnSave()
							}
						}
					})
				})

				// Gateway & engine diagnostics
				section(c, "独立网关与引擎诊断", func() {
					gwState := "未运行"
					if diag.Gateway.Running {
						gwState = fmt.Sprintf("运行中 · PID %d · %s", diag.Gateway.PID, diag.Gateway.Addr)
					}
					row(c, "VelanGateway 子进程", fmt.Sprintf("%s（重启次数: %d）", gwState, diag.Gateway.Restarts), func() {
						if ui.Button(c.Key("diag-kill-gw"), "模拟崩溃恢复").Clicked() && act.OnSimulateGWCrash != nil {
							act.OnSimulateGWCrash()
						}
					})
					mpvPath := diag.MpvPath
					if mpvPath == "" {
						mpvPath = "未加载（进入播放时加载）"
					}
					row(c, "libmpv 动态库", mpvPath, nil)
					row(c, "当前版本", diag.AppVersion, nil)
				})

				// Shortcuts cheat sheet
				section(c, "常用快捷键", func() {
					pairs := [][2]string{
						{"Space", "播放 / 暂停"},
						{"← / → (Shift)", "快退 / 快进 5s（30s）"},
						{"↑ / ↓", "音量加 / 减"},
						{"M / F / T", "静音 / 全屏 / 置顶"},
						{"[ / ] / \\", "减速 / 加速 / 重置 1.0x"},
						{"V / A / L", "切换字幕 / 音轨 / 播放列表"},
						{"Cmd+O / Cmd+U", "打开文件 / 打开网络 URL"},
					}
					for _, p := range pairs {
						ui.Row(c).Height(34).AlignItems(ui.Center).Padding(0, 14).Children(func() {
							ui.Text(c, p[0]).FontSize(12.5).FontWeight(600).Width(160)
							ui.Text(c, p[1]).FontSize(12.5).TextColor(t.TextMuted)
						})
					}
				})
			})
		})

		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight).
			PassThrough().Material(glass.ScrollEdge{})
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight).
			Padding(0, 24).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, "设置").FontSize(20).Bold()
		})
	})
}

func section(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(t.TextMuted)
		ui.Column(c).FillWidth().Radius(10).Background(t.Surface).
			Border(1, theme.Hairline(t)).Children(body)
	})
}

func row(c *ui.Context, title, sub string, right func()) {
	t := c.Theme()
	ui.Row(c).MinHeight(50).AlignItems(ui.Center).Padding(8, 14).Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13.5).FontWeight(500)
			if sub != "" {
				ui.Text(c, sub).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
			}
		})
		if right != nil {
			right()
		}
	})
}
