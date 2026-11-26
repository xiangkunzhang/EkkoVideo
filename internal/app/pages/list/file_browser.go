package list

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/theme"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

// FileBrowserState holds UI state for the local/folder file browser page.
type FileBrowserState struct {
	CurrentDir string
	Items      []core.Item
	Loading    bool
	Err        string
	ListState  ui.ListState
}

// FileBrowserActions connects FileBrowserPage to the app shell.
type FileBrowserActions struct {
	OnOpenFolderDialog func()
	OnOpenFileDialog   func()
	OnNavigateDir      func(dir string)
	OnPlayFile         func(item core.Item)
}

// FileBrowserPage renders a directory/file browser with breadcrumb navigation.
func FileBrowserPage(c *ui.Context, st *FileBrowserState, act FileBrowserActions) {
	t := c.Theme()
	Shell(c, "本地视频", 38,
		func() {
			if ui.Button(c.Key("fb-open-file"), "打开文件…").Clicked() && act.OnOpenFileDialog != nil {
				act.OnOpenFileDialog()
			}
			if ui.Button(c.Key("fb-open-dir"), "选择文件夹…").Clicked() && act.OnOpenFolderDialog != nil {
				act.OnOpenFolderDialog()
			}
		},
		func() {
			parent := filepath.Dir(st.CurrentDir)
			canUp := st.CurrentDir != "" && parent != st.CurrentDir
			up := ui.Button(c.Key("fb-up"), "上级目录")
			if !canUp {
				up.Disabled(true)
			}
			if up.Clicked() && canUp && act.OnNavigateDir != nil {
				act.OnNavigateDir(parent)
			}
			pathLabel := st.CurrentDir
			if pathLabel == "" {
				pathLabel = "默认媒体目录"
			}
			ui.Text(c, pathLabel).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
		},
		func() {
			topPad := float32(theme.TitleBarHeight + 48)
			switch {
			case st.Loading && len(st.Items) == 0:
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Children(func() {
					ui.Spinner(c).FontSize(22).Label("正在读取目录")
				})
			case st.Err != "":
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Gap(8).Children(func() {
					ui.Text(c, "无法打开目录").FontSize(15).FontWeight(600)
					ui.Text(c, st.Err).FontSize(12).TextColor(t.TextMuted)
				})
			case len(st.Items) == 0:
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Gap(10).Children(func() {
					ui.Text(c, "当前目录没有可播放的视频").FontSize(14).TextColor(t.TextMuted)
					if ui.Button(c.Key("fb-empty-open"), "打开视频文件…").Clicked() && act.OnOpenFileDialog != nil {
						act.OnOpenFileDialog()
					}
				})
			default:
				ui.List(c, &st.ListState, len(st.Items), func(i int) {
					it := st.Items[i]
					row := ui.ButtonBase(c.Key(it.Ref.ID)).FillWidth().Height(40).
						Padding(0, 14).Radius(8).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(t.Text.Alpha(0.06))
					}
					row.Children(func() {
						ui.Row(c).Fill().AlignItems(ui.Center).Gap(10).Children(func() {
							ic := theme.IconFilm
							icColor := t.Accent
							if it.Ref.Kind == core.KindFolder {
								ic = theme.IconFolder
								icColor = t.TextMuted
							}
							ui.Icon(c, ic).Size(16, 16).TextColor(icColor).Shrink(0)
							ui.Text(c, it.Title).FontSize(13.5).SingleLine().Grow(1).MinWidth(0)
							ui.Text(c, it.Subtitle).FontSize(12).TextColor(t.TextMuted).FontFeatures("tnum").Shrink(0)
						})
					})
					if row.Clicked() {
						if it.Ref.Kind == core.KindFolder {
							if act.OnNavigateDir != nil {
								act.OnNavigateDir(it.Ref.ID)
							}
						} else if act.OnPlayFile != nil {
							act.OnPlayFile(it)
						}
					}
				}).Fill().Padding(topPad, 20, 24, 20).ScrollbarInsets(topPad, 0, 0, 0)
			}
		},
	)
}
