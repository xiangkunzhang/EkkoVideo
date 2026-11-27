// Package list implements BrowseShell and separated list page templates
// (FileBrowser, PosterGrid, ChannelList, LiveGrid).
package list

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
)

// Shell renders the shared browse page header, scroll-edge blur, and body slot.
func Shell(c *ui.Context, title string, topExtraH float32, right, controls, body func()) {
	top := float32(theme.TitleBarHeight) + topExtraH
	ui.Box(c).Fill().Children(func() {
		body()
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(top + 10).PassThrough().Material(glass.ScrollEdge{})
		ui.Column(c).Absolute().Top(0).Left(0).Right(0).Children(func() {
			ui.Row(c).Height(theme.TitleBarHeight).Shrink(0).AlignItems(ui.Center).
				Padding(0, 24).Gap(10).DragWindow().Children(func() {
				ui.Text(c, title).FontSize(20).Bold().SingleLine()
				ui.Spacer(c)
				if right != nil {
					right()
				}
			})
			if controls != nil {
				ui.Row(c).Height(topExtraH).AlignItems(ui.Center).Padding(0, 24).Gap(8).Children(controls)
			}
		})
	})
}
