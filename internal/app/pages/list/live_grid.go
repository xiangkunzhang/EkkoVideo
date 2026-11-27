package list

import (
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/infra/imageloader"
	"github.com/xiangkunzhang/Velan/internal/provider/live"
)

// LiveGridState holds state for the dedicated 16:9 live-room browser.
type LiveGridState struct {
	Platforms   []live.PlatformInfo
	PlatformID  string // "bilibili" | "huya" | "douyu" | "__fav__"
	Category    string
	SearchQuery string
	Rooms       []core.Item
	Page        int
	HasMore     bool
	Loading     bool
	Err         string
	GridState   ui.GridState
}

type LiveGridActions struct {
	OnSelectPlatform func(id string)
	OnSelectCategory func(cat string)
	OnSearch         func(q string)
	OnRefresh        func()
	OnLoadMore       func()
	OnOpenRoom       func(item core.Item)
	OnToggleFav      func(item core.Item)
	IsFav            func(ref core.ItemRef) bool
}

// LiveGridPage renders a dedicated 16:9 widescreen grid for live rooms,
// completely separate from 3:4 VOD posters and 1-row IPTV channels.
func LiveGridPage(c *ui.Context, st *LiveGridState, img *imageloader.Loader, act LiveGridActions) {
	t := c.Theme()
	var activeCats []core.Section
	for _, p := range st.Platforms {
		if p.ID == st.PlatformID {
			activeCats = p.Categories
			break
		}
	}
	controlsH := float32(38)
	if len(activeCats) > 0 && st.PlatformID != "__fav__" {
		controlsH = 72
	}

	Shell(c, "直播广场", controlsH,
		func() {
			sf := ui.SearchField(c.Key("live-search"), &st.SearchQuery).
				Placeholder("搜索主播或输入房间号").Width(210)
			if sf.Submitted() && act.OnSearch != nil {
				act.OnSearch(st.SearchQuery)
			}
			if ui.Button(c.Key("live-search-go"), "搜索").Clicked() && act.OnSearch != nil {
				act.OnSearch(st.SearchQuery)
			}
			if st.SearchQuery != "" && ui.Button(c.Key("live-search-clr"), "清除").Clicked() && act.OnSearch != nil {
				st.SearchQuery = ""
				act.OnSearch("")
			}
			if ui.Button(c.Key("live-refresh"), "刷新").Clicked() && act.OnRefresh != nil {
				act.OnRefresh()
			}
		},
		func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					for _, p := range st.Platforms {
						active := st.PlatformID == p.ID
						b := ui.ButtonBase(c.Key("live-plat-"+p.ID)).Height(28).Padding(0, 12).Radius(7).Cursor(ui.CursorPointer)
						if active {
							b.Background(t.Accent).TextColor(t.AccentText)
						} else if b.Hovered() {
							b.Background(t.Text.Alpha(0.08))
						} else {
							b.Background(t.Text.Alpha(0.04))
						}
						b.Children(func() {
							ui.Text(c, p.Name).FontSize(12.5).FontWeight(500)
						})
						if b.Clicked() && act.OnSelectPlatform != nil {
							act.OnSelectPlatform(p.ID)
						}
					}
					favActive := st.PlatformID == "__fav__"
					fb := ui.ButtonBase(c.Key("live-plat-fav")).Height(28).Padding(0, 12).Radius(7).Cursor(ui.CursorPointer)
					if favActive {
						fb.Background(t.Accent).TextColor(t.AccentText)
					} else if fb.Hovered() {
						fb.Background(t.Text.Alpha(0.08))
					} else {
						fb.Background(t.Text.Alpha(0.04))
					}
					fb.Children(func() {
						ui.Text(c, "★ 关注主播").FontSize(12.5).FontWeight(500)
					})
					if fb.Clicked() && act.OnSelectPlatform != nil {
						act.OnSelectPlatform("__fav__")
					}
					ui.Spacer(c)
					if len(st.Rooms) > 0 {
						ui.Textf(c, "已加载 %d 个直播间", len(st.Rooms)).FontSize(11.5).TextColor(t.TextMuted).FontFeatures("tnum")
					}
					if st.HasMore && act.OnLoadMore != nil {
						lbl := "加载更多"
						if st.Loading {
							lbl = "加载中…"
						}
						mb := ui.ButtonBase(c.Key("live-load-more")).Height(24).Padding(0, 10).Radius(6).
							Background(t.Text.Alpha(0.06)).Cursor(ui.CursorPointer)
						mb.Children(func() {
							ui.Text(c, lbl).FontSize(11.5).TextColor(t.TextMuted)
						})
						if mb.Clicked() && !st.Loading {
							act.OnLoadMore()
						}
					}
				})
				if len(activeCats) > 0 && st.PlatformID != "__fav__" {
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						for _, cat := range activeCats {
							active := st.Category == cat.ID
							b := ui.ButtonBase(c.Key("live-cat-"+cat.ID)).Height(25).Padding(0, 10).Radius(6).Cursor(ui.CursorPointer)
							if active {
								b.Background(t.Accent.Alpha(0.18)).TextColor(t.Accent)
							} else if b.Hovered() {
								b.Background(t.Text.Alpha(0.07))
							}
							b.Children(func() {
								ui.Text(c, cat.Label).FontSize(12)
							})
							if b.Clicked() && act.OnSelectCategory != nil {
								act.OnSelectCategory(cat.ID)
							}
						}
					})
				}
			})
		},
		func() {
			topPad := float32(theme.TitleBarHeight) + controlsH + 12
			if len(st.Rooms) == 0 {
				ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Gap(10).Children(func() {
					switch {
					case st.Loading:
						theme.BrandLoader(c, 58, "正在加载直播间…", t.TextMuted, true)
					case st.Err != "":
						ui.Text(c, "加载失败").FontSize(15).FontWeight(600)
						ui.Text(c, st.Err).FontSize(12.5).TextColor(t.TextMuted).MaxWidth(420).TextAlign(ui.Center)
						if ui.Button(c.Key("live-retry"), "重试").Clicked() && act.OnRefresh != nil {
							act.OnRefresh()
						}
					default:
						if mark := theme.BrandMark(); mark != nil {
							ui.Image(c, mark).Size(52, 52).Opacity(0.8)
						}
						ui.Text(c, "暂无直播间").FontSize(14).TextColor(t.TextMuted)
					}
				})
				return
			}

			w, h := c.Size()
			const cellMinW = float32(256)
			const padX = float32(24)
			gap := t.Space(2)
			inner := max(cellMinW, w-2*padX)
			cols := max(1, int((inner+gap)/(cellMinW+gap)))
			cellW := (inner - float32(cols-1)*gap) / float32(cols)
			rowH := (cellW-12)*9/16 + 62

			n := len(st.Rooms)
			rows := (n + cols - 1) / cols
			viewH := max(1, h-topPad-28)
			if float32(rows)*rowH <= viewH+rowH*0.5 && st.HasMore && !st.Loading && act.OnLoadMore != nil {
				act.OnLoadMore()
			}

			ui.GridView(c, &st.GridState, n, cellMinW, rowH, func(i int) {
				liveRoomCard(c, st.Rooms[i], img, act)
				if i >= n-cols*2 && st.HasMore && !st.Loading && act.OnLoadMore != nil {
					act.OnLoadMore()
				}
			}).Fill().Padding(topPad, padX, 28, padX).ScrollbarInsets(topPad, 0, 0, 0)
		},
	)
}

func liveRoomCard(c *ui.Context, item core.Item, img *imageloader.Loader, act LiveGridActions) {
	t := c.Theme()
	card := ui.ButtonBase(c.Key("lr-" + item.Ref.Source + "-" + item.Ref.ID)).Fill().Padding(6).Radius(10).Cursor(ui.CursorPointer)
	if card.Hovered() {
		card.Background(t.Text.Alpha(0.06))
	}
	fav := act.IsFav != nil && act.IsFav(item.Ref)
	starClicked := false
	card.Children(func() {
		ui.Column(c).Fill().Gap(6).Children(func() {
			// 16:9 widescreen thumbnail
			ui.Box(c).FillWidth().Grow(1).MinHeight(0).Radius(8).Clip().Background(t.Text.Alpha(0.08)).Children(func() {
				if bmp, alpha := img.Get(c, item.Poster); bmp != nil {
					ui.Image(c, bmp).Fill().Fit(ui.Cover).Opacity(alpha)
				}
				if area := item.Extra["area"]; area != "" {
					ui.Box(c).Absolute().Top(6).Left(6).Padding(2, 6).Radius(4).
						Background(ui.RGBA(0, 0, 0, 0.62)).Children(func() {
						ui.Text(c, area).FontSize(10).TextColor(theme.White())
					})
				}
				if online := item.Extra["online"]; online != "" {
					ui.Box(c).Absolute().Bottom(6).Right(6).Padding(2, 6).Radius(4).
						Background(ui.RGBA(0, 0, 0, 0.62)).Children(func() {
						ui.Text(c, online+" 人气").FontSize(10).TextColor(theme.White()).FontFeatures("tnum")
					})
				}
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, item.Title).FontSize(13).FontWeight(500).SingleLine()
					if item.Subtitle != "" {
						ui.Text(c, item.Subtitle).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
					}
				})
				if act.OnToggleFav != nil {
					star := "☆"
					starCol := t.TextMuted.Alpha(0.6)
					if fav {
						star = "★"
						starCol = ui.Hex("#F59E0B")
					}
					sb := ui.ButtonBase(c.Key("lr-fav-"+item.Ref.Source+"-"+item.Ref.ID)).
						Size(26, 26).Radius(6).Center().Cursor(ui.CursorPointer)
					if sb.Hovered() {
						sb.Background(t.Text.Alpha(0.08))
					}
					sb.Children(func() {
						ui.Text(c, star).FontSize(13).TextColor(starCol)
					})
					if sb.Clicked() {
						starClicked = true
						act.OnToggleFav(item)
					}
				}
			})
		})
	})
	if !starClicked && card.Clicked() && act.OnOpenRoom != nil {
		act.OnOpenRoom(item)
	}
}
