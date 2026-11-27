package list

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/theme"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

type ChannelListState struct {
	Channels    []core.Item
	Groups      []string
	ActiveGroup string
	SearchQuery string
	Loading     bool
	Err         string
	ListState   ui.ListState
}

type ChannelListActions struct {
	OnAddSub      func()
	OnRefresh     func()
	OnSelectGroup func(group string)
	OnPlayChannel func(item core.Item)
}

func ChannelListPage(c *ui.Context, st *ChannelListState, act ChannelListActions) {
	t := c.Theme()
	Shell(c, "IPTV 电视直播", 40,
		func() {
			sf := ui.SearchField(c.Key("iptv-search"), &st.SearchQuery).Placeholder("搜索频道名或分组").Width(200)
			_ = sf
			if ui.Button(c.Key("iptv-refresh"), "刷新").Clicked() && act.OnRefresh != nil {
				act.OnRefresh()
			}
			if ui.PrimaryButton(c.Key("iptv-add"), "添加订阅…").Clicked() && act.OnAddSub != nil {
				act.OnAddSub()
			}
		},
		func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				groups := append([]string{"全部"}, st.Groups...)
				for i, g := range groups {
					if i > 10 {
						break
					}
					active := (g == "全部" && st.ActiveGroup == "") || st.ActiveGroup == g
					b := ui.ButtonBase(c.Key("iptv-grp-"+g)).Height(26).Padding(0, 10).Radius(6).Cursor(ui.CursorPointer)
					if active {
						b.Background(t.Accent).TextColor(t.AccentText)
					} else if b.Hovered() {
						b.Background(t.Text.Alpha(0.08))
					} else {
						b.Background(t.Text.Alpha(0.04))
					}
					b.Children(func() {
						ui.Text(c, g).FontSize(12)
					})
					if b.Clicked() && act.OnSelectGroup != nil {
						if g == "全部" {
							act.OnSelectGroup("")
						} else {
							act.OnSelectGroup(g)
						}
					}
				}
			})
		},
		func() {
			topPad := float32(theme.TitleBarHeight + 52)
			filtered := filterChannels(st.Channels, st.ActiveGroup, st.SearchQuery)
			switch {
			case st.Loading && len(st.Channels) == 0:
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Children(func() {
					ui.Spinner(c).FontSize(22).Label("正在加载订阅")
				})
			case st.Err != "" && len(st.Channels) == 0:
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Gap(10).Children(func() {
					ui.Text(c, "订阅加载失败").FontSize(15).FontWeight(600)
					ui.Text(c, st.Err).FontSize(12.5).TextColor(t.TextMuted)
					if ui.Button(c.Key("iptv-retry"), "重试").Clicked() && act.OnRefresh != nil {
						act.OnRefresh()
					}
				})
			case len(st.Channels) == 0:
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Gap(10).Children(func() {
					ui.Icon(c, theme.IconTV).Size(36, 36).TextColor(t.TextMuted.Alpha(0.5))
					ui.Text(c, "暂无 IPTV 频道").FontSize(14).TextColor(t.TextMuted)
					ui.Text(c, "点击右上角「添加订阅…」导入 M3U / M3U8 直播源").FontSize(12).TextColor(t.TextMuted.Alpha(0.75))
					if ui.PrimaryButton(c.Key("iptv-empty-add"), "添加 M3U 订阅…").Clicked() && act.OnAddSub != nil {
						act.OnAddSub()
					}
				})
			case len(filtered) == 0:
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Children(func() {
					ui.Text(c, "没有匹配的频道").FontSize(14).TextColor(t.TextMuted)
				})
			default:
				ui.List(c, &st.ListState, len(filtered), func(i int) {
					ch := filtered[i]
					row := ui.ButtonBase(c.Key("ch-"+ch.Ref.ID)).FillWidth().Height(50).
						Padding(0, 14).Radius(8).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(t.Text.Alpha(0.06))
					}
					row.Children(func() {
						ui.Row(c).Fill().AlignItems(ui.Center).Gap(12).Children(func() {
							ui.Textf(c, "%03d", ch.Index).FontSize(12.5).FontWeight(600).
								TextColor(t.TextMuted).FontFeatures("tnum").Width(34)
							ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
								ui.Text(c, ch.Title).FontSize(14).FontWeight(500).SingleLine()
								if ch.Subtitle != "" {
									ui.Text(c, ch.Subtitle).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
								}
							})
							if ch.Children > 1 {
								ui.Box(c).Padding(2, 8).Radius(5).Background(t.Accent.Alpha(0.14)).Children(func() {
									ui.Text(c, fmt.Sprintf("%d 线路", ch.Children)).FontSize(11).TextColor(t.Accent)
								})
							}
						})
					})
					if row.Clicked() && act.OnPlayChannel != nil {
						act.OnPlayChannel(ch)
					}
				}).Fill().Padding(topPad, 20, 24, 20).ScrollbarInsets(topPad, 0, 0, 0)
			}
		},
	)
}

func filterChannels(items []core.Item, group, query string) []core.Item {
	q := strings.ToLower(strings.TrimSpace(query))
	if group == "" && q == "" {
		return items
	}
	var out []core.Item
	for _, it := range items {
		if group != "" && it.Extra["group"] != group {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Title), q) &&
			!strings.Contains(strings.ToLower(it.Extra["group"]), q) {
			continue
		}
		out = append(out, it)
	}
	return out
}
