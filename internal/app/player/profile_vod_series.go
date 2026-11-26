package player

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/theme"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

// VODSeriesProfile extends VODSingleProfile for multi-episode online/library series
// by replacing the right drawer with a dual-tab [选集 | 剧集推荐] panel.
type VODSeriesProfile struct {
	VODSingleProfile
	DrawerTab     int // 0 = episodes, 1 = related recommendations
	Related       []core.Item
	OnOpenRelated func(core.Item)
}

func NewVODSeriesProfile() *VODSeriesProfile { return &VODSeriesProfile{} }

func (p *VODSeriesProfile) ID() string { return "vod_series" }

func (p *VODSeriesProfile) SideOrOverlay(c *ui.Context, st *PlayerViewState) {
	white := theme.White()
	ui.Column(c.Key("series-drawer")).Width(300).FillHeight().
		Background(ui.Hex("#14161b")).Border(1, ui.RGBA(255, 255, 255, 0.08)).
		Padding(14, 12, 12, 12).Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			if pillBtn(c, "tab-eps", "选集", p.DrawerTab == 0).Clicked() {
				p.DrawerTab = 0
			}
			if pillBtn(c, "tab-rel", "剧集推荐", p.DrawerTab == 1).Clicked() {
				p.DrawerTab = 1
			}
			ui.Spacer(c)
			if pillBtn(c, "close-series-drawer", "收起", false).Clicked() {
				st.DrawerOpen = false
			}
		})

		if p.DrawerTab == 1 {
			if len(p.Related) == 0 {
				ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
					ui.Text(c, "暂无相关推荐").FontSize(12.5).TextColor(ui.RGBA(255, 255, 255, 0.5))
				})
				return
			}
			for i, rel := range p.Related {
				row := ui.ButtonBase(c.Key(fmt.Sprintf("rel-d-%d-%s", i, rel.Ref.ID))).
					FillWidth().Height(48).Padding(0, 10).Radius(8).Cursor(ui.CursorPointer)
				if row.Hovered() {
					row.Background(ui.RGBA(255, 255, 255, 0.08))
				}
				row.Children(func() {
					ui.Column(c).Fill().Padding(6, 0).Gap(2).Children(func() {
						ui.Text(c, rel.Title).FontSize(13).TextColor(white).SingleLine()
						ui.Text(c, rel.Subtitle).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.55)).SingleLine()
					})
				})
				if row.Clicked() && p.OnOpenRelated != nil {
					p.OnOpenRelated(rel)
				}
			}
			return
		}

		// Tab 0: Episodes in current queue
		if st.Session == nil || st.Session.Queue == nil || len(st.Session.Queue.Items) == 0 {
			return
		}
		q := st.Session.Queue
		ui.List(c, &st.DrawerList, len(q.Items), func(i int) {
			it := q.Items[i]
			active := i == q.Current
			row := ui.ButtonBase(c.Key(fmt.Sprintf("sep-%d-%s", i, it.Ref.ID))).
				FillWidth().Height(36).Padding(0, 10).Radius(7).Cursor(ui.CursorPointer)
			if active {
				row.Background(ui.RGBA(90, 150, 230, 0.25))
			} else if row.Hovered() {
				row.Background(ui.RGBA(255, 255, 255, 0.07))
			}
			row.Children(func() {
				ui.Row(c).Fill().AlignItems(ui.Center).Gap(8).Children(func() {
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
