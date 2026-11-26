package list

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/theme"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/imageloader"
)

// PosterGridState holds feed and filter state for one online/media-server poster grid.
type PosterGridState struct {
	SourceID    string
	Title       string
	Mode        string // "recommend" | "rank" | "category" | "search"
	Category    string
	SearchQuery string
	Modes       []core.Section
	Categories  []core.Section

	Items      []core.Item
	Seen       map[string]bool
	NextCursor string
	HasMore    bool
	Started    bool
	Loading    bool
	Err        string
	Tries      int
	ErrAt      time.Time
	GridState  ui.GridState
}

func (s *PosterGridState) ResetFeed() {
	s.Items = nil
	s.Seen = make(map[string]bool)
	s.NextCursor = ""
	s.HasMore = true
	s.Started = false
	s.Loading = false
	s.Err = ""
	s.Tries = 0
}

func (s *PosterGridState) AppendPage(p core.Page) {
	if s.Seen == nil {
		s.Seen = make(map[string]bool)
	}
	added := 0
	for _, it := range p.Items {
		if it.Ref.ID == "" || s.Seen[it.Ref.ID] {
			continue
		}
		s.Seen[it.Ref.ID] = true
		s.Items = append(s.Items, it)
		added++
	}
	s.NextCursor = p.Next
	s.HasMore = p.Next != "" && added > 0
}

type PosterGridActions struct {
	OnLoadMore   func()
	OnSelectMode func(mode string)
	OnSelectCat  func(cat string)
	OnSearch     func(q string)
	OnOpenDetail func(item core.Item)
	OnToggleFav  func(item core.Item)
	IsFav        func(ref core.ItemRef) bool
}

// PosterGridPage renders a virtualized 3:4 poster grid inside BrowseShell.
func PosterGridPage(c *ui.Context, st *PosterGridState, img *imageloader.Loader, act PosterGridActions) {
	t := c.Theme()
	controlsH := float32(38)
	if len(st.Categories) > 0 && (st.Mode == "rank" || st.Mode == "category") {
		controlsH = 74
	}

	Shell(c, st.Title, controlsH,
		func() {
			sf := ui.SearchField(c.Key("pg-search-"+st.SourceID), &st.SearchQuery).
				Placeholder("搜索 " + st.Title).Width(220)
			if sf.Submitted() && act.OnSearch != nil {
				act.OnSearch(st.SearchQuery)
			}
		},
		func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					for _, m := range st.Modes {
						active := st.Mode == m.ID
						b := ui.ButtonBase(c.Key("mode-"+m.ID)).Height(28).Padding(0, 12).Radius(7).Cursor(ui.CursorPointer)
						if active {
							b.Background(t.Accent).TextColor(t.AccentText)
						} else if b.Hovered() {
							b.Background(t.Text.Alpha(0.08))
						} else {
							b.Background(t.Text.Alpha(0.04))
						}
						b.Children(func() {
							ui.Text(c, m.Label).FontSize(12.5).FontWeight(500)
						})
						if b.Clicked() && act.OnSelectMode != nil {
							act.OnSelectMode(m.ID)
						}
					}
				})
				if len(st.Categories) > 0 && (st.Mode == "rank" || st.Mode == "category") {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						for _, cat := range st.Categories {
							active := st.Category == cat.ID
							b := ui.ButtonBase(c.Key("cat-"+cat.ID)).Height(26).Padding(0, 10).Radius(6).Cursor(ui.CursorPointer)
							if active {
								b.Background(t.Accent.Alpha(0.18)).TextColor(t.Accent)
							} else if b.Hovered() {
								b.Background(t.Text.Alpha(0.07))
							}
							b.Children(func() {
								ui.Text(c, cat.Label).FontSize(12)
							})
							if b.Clicked() && act.OnSelectCat != nil {
								act.OnSelectCat(cat.ID)
							}
						}
					})
				}
			})
		},
		func() {
			topPad := float32(theme.TitleBarHeight) + controlsH + 12
			if len(st.Items) == 0 {
				posterEmptyState(c, st, topPad, act.OnLoadMore)
				return
			}
			w, _ := c.Size()
			const cellMinW = float32(176)
			const padX = float32(24)
			gap := c.Theme().Space(2)
			inner := max(cellMinW, w-2*padX)
			cols := max(1, int((inner+gap)/(cellMinW+gap)))
			cellW := (inner - float32(cols-1)*gap) / float32(cols)
			rowH := (cellW-16)*4/3 + 64

			n := len(st.Items)
			ui.GridView(c, &st.GridState, n, cellMinW, rowH, func(i int) {
				posterCard(c, st.Items[i], img, act)
				if i >= n-cols*2 && st.HasMore && !st.Loading && act.OnLoadMore != nil {
					act.OnLoadMore()
				}
			}).Fill().Padding(topPad, padX, 28, padX).ScrollbarInsets(topPad, 0, 0, 0)
		},
	)
}

func posterEmptyState(c *ui.Context, st *PosterGridState, topPad float32, retryFn func()) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(topPad, 24, 24, 24).Center().Gap(10).Children(func() {
		switch {
		case st.Err != "" && st.Tries < 4:
			wait := time.Duration(2<<st.Tries) * time.Second
			if since := time.Since(st.ErrAt); since >= wait {
				st.Tries++
				if retryFn != nil {
					retryFn()
				}
			} else {
				c.After(wait - since)
			}
			ui.Spinner(c).FontSize(22).Label("正在重试")
			ui.Text(c, "网络不稳定，正在重试…").FontSize(13).TextColor(t.TextMuted)
		case st.Err != "":
			ui.Text(c, "加载失败").FontSize(15).FontWeight(600)
			ui.Text(c, st.Err).FontSize(12.5).TextColor(t.TextMuted).MaxWidth(400).TextAlign(ui.Center)
			if ui.Button(c.Key("pg-retry"), "重试").Clicked() && retryFn != nil {
				st.Tries = 0
				retryFn()
			}
		case st.Loading || !st.Started:
			ui.Spinner(c).FontSize(22).Label("正在加载")
			if !st.Started && retryFn != nil {
				retryFn()
			}
		default:
			ui.Text(c, "暂无内容").FontSize(14).TextColor(t.TextMuted)
		}
	})
}

func posterCard(c *ui.Context, it core.Item, img *imageloader.Loader, act PosterGridActions) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("card-" + it.Ref.Source + "-" + it.Ref.ID)).Fill().Label(it.Title).Cursor(ui.CursorPointer)
	fav := act.IsFav != nil && act.IsFav(it.Ref)
	b.ContextMenu(func(m *ui.Menu) {
		lbl := "收藏"
		if fav {
			lbl = "取消收藏"
		}
		if m.Item(lbl).Chosen() && act.OnToggleFav != nil {
			act.OnToggleFav(it)
		}
	})
	b.Children(func() {
		ui.Column(c).Fill().Padding(4, 8, 12, 8).Gap(8).Children(func() {
			poster := ui.Box(c).Grow(1).FillWidth().MinHeight(0).Radius(10).Clip().Background(t.Text.Alpha(0.06))
			if b.Hovered() {
				poster.Border(2, t.Accent)
			}
			poster.Children(func() {
				if bmp, alpha := img.Get(c, it.Poster); bmp != nil {
					ui.Image(c, bmp).Fill().Fit(ui.Cover).Opacity(alpha)
				}
				if it.Index > 0 {
					ui.Box(c).Absolute().Top(6).Left(6).Padding(2, 6).Radius(5).
						Background(ui.RGBA(12, 14, 18, 0.78)).Children(func() {
						ui.Textf(c, "#%d", it.Index).FontSize(11).Bold().TextColor(theme.White()).FontFeatures("tnum")
					})
				}
				if fav {
					ui.Box(c).Absolute().Top(6).Right(6).Padding(2, 6).Radius(5).
						Background(ui.RGBA(12, 14, 18, 0.78)).Children(func() {
						ui.Text(c, "★").FontSize(11).TextColor(ui.Hex("#e5a93c"))
					})
				}
			})

			ui.Column(c).Height(38).Shrink(0).Gap(2).Children(func() {
				ui.Text(c, it.Title).FontSize(13.5).FontWeight(500).SingleLine()
				sub := it.Subtitle
				if sub == "" && it.Children > 0 {
					sub = fmt.Sprintf("%d 集", it.Children)
				}
				ui.Text(c, sub).FontSize(11.5).TextColor(t.TextMuted).SingleLine().FontFeatures("tnum")
			})
		})
	})
	if b.Clicked() && act.OnOpenDetail != nil {
		act.OnOpenDetail(it)
	}
}
