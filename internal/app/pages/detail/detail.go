// Package detail renders the immersive dark series/movie detail page with
// 100-episode range tabs and related/sequel recommendations.
package detail

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/app/theme"
	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/infra/imageloader"
)

type State struct {
	Item     core.Item
	Detail   *core.Detail
	Related  []core.Item
	Loading  bool
	Err      string
	RangeIdx int
}

type Actions struct {
	OnBack        func()
	OnPlayEpisode func(epIndex int)
	OnToggleFav   func()
	OnOpenRelated func(item core.Item)
	OnRetry       func()
}

func Page(c *ui.Context, st *State, img *imageloader.Loader, isFav bool, resumeEp int, act Actions) {
	white := theme.White()
	muted := ui.RGBA(255, 255, 255, 0.65)
	bg := ui.Hex("#0b0c10")

	ui.Box(c).Fill().Background(bg).Children(func() {
		// Top bar
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(theme.TitleBarHeight+6).
			Padding(6, 20, 0, 82).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
			b := ui.ButtonBase(c.Key("dt-back")).Height(30).Padding(0, 10).Radius(7).
				Background(ui.RGBA(255, 255, 255, 0.1)).Cursor(ui.CursorPointer)
			b.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Icon(c, theme.IconBack).Size(15, 15).TextColor(white)
					ui.Text(c, "返回").FontSize(12.5).TextColor(white)
				})
			})
			if b.Clicked() && act.OnBack != nil {
				act.OnBack()
			}
			topTitle := st.Item.Title
			if st.Detail != nil && st.Detail.Item.Title != "" {
				topTitle = st.Detail.Item.Title
			}
			ui.Text(c, topTitle).FontSize(15).FontWeight(600).TextColor(white).SingleLine().Grow(1).MinWidth(0)
		})

		topPad := float32(theme.TitleBarHeight + 18)
		winW, _ := c.Size()
		ui.Scroll(c.Key("detail-scroll")).Fill().Padding(topPad, 28, 36, 28).Children(func() {
			ui.Column(c).FillWidth().Gap(24).Children(func() {
				// Hero header: poster + metadata + primary actions
				ui.Row(c).FillWidth().Gap(22).AlignItems(ui.Start).Children(func() {
					ui.Box(c).Width(170).Height(238).Shrink(0).Radius(12).Clip().
						Background(ui.RGBA(255, 255, 255, 0.08)).Children(func() {
						cover := st.Item.Poster
						if cover == "" && st.Detail != nil {
							cover = st.Detail.Item.Poster
						}
						if bmp, alpha := img.Get(c, cover); bmp != nil {
							ui.Image(c, bmp).Fill().Fit(ui.Cover).Opacity(alpha)
						}
					})

					ui.Column(c).Grow(1).MinWidth(0).Gap(10).Children(func() {
						title := st.Item.Title
						overview := st.Item.Overview
						sub := st.Item.Subtitle
						var actors []string
						var tags []string
						if st.Detail != nil {
							if st.Detail.Item.Title != "" {
								title = st.Detail.Item.Title
							}
							if st.Detail.Item.Overview != "" {
								overview = st.Detail.Item.Overview
							}
							if st.Detail.Item.Subtitle != "" {
								sub = st.Detail.Item.Subtitle
							}
							actors = st.Detail.Actors
							tags = st.Detail.Item.Tags
						}
						ui.Text(c, title).FontSize(26).Bold().TextColor(white)
						if sub != "" {
							ui.Text(c, sub).FontSize(13).TextColor(muted)
						}
						if len(tags) > 0 {
							ui.Row(c).Gap(6).Children(func() {
								for i, tg := range tags {
									if i >= 6 {
										break
									}
									ui.Box(c).Padding(3, 8).Radius(6).Background(ui.RGBA(255, 255, 255, 0.1)).Children(func() {
										ui.Text(c, tg).FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.85))
									})
								}
							})
						}
						if len(actors) > 0 {
							ui.Text(c, "主演："+joinLimit(actors, 5)).FontSize(12.5).TextColor(muted).SingleLine()
						}
						if overview != "" {
							ui.Text(c, overview).FontSize(13).TextColor(ui.RGBA(255, 255, 255, 0.78))
						}

						ui.Row(c).Padding(6, 0, 0, 0).Gap(10).Children(func() {
							playLabel := "播放第 1 集"
							playIdx := 0
							if resumeEp > 0 {
								playLabel = fmt.Sprintf("继续看第 %d 集", resumeEp)
								playIdx = resumeEp - 1
							}
							pb := ui.PrimaryButton(c.Key("dt-play"), playLabel)
							if st.Detail == nil || len(st.Detail.Episodes) == 0 {
								pb.Disabled(true)
							}
							if pb.Clicked() && act.OnPlayEpisode != nil {
								act.OnPlayEpisode(playIdx)
							}

							favLabel := "☆ 收藏"
							if isFav {
								favLabel = "★ 已收藏"
							}
							fb := ui.ButtonBase(c.Key("dt-fav")).Height(32).Padding(0, 14).Radius(8).
								Background(ui.RGBA(255, 255, 255, 0.12)).Cursor(ui.CursorPointer)
							fb.Children(func() {
								ui.Row(c).Fill().Center().Children(func() {
									ui.Text(c, favLabel).FontSize(13).TextColor(white)
								})
							})
							if fb.Clicked() && act.OnToggleFav != nil {
								act.OnToggleFav()
							}
						})
					})
				})

				if st.Loading {
					ui.Row(c).FillWidth().Center().Padding(16, 0).Children(func() {
						theme.BrandLoader(c, 52, "正在加载剧集列表…", muted, true)
					})
				} else if st.Err != "" {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "加载失败："+st.Err).FontSize(13).TextColor(ui.Hex("#ff8f86"))
						if ui.Button(c.Key("dt-retry"), "重试").Clicked() && act.OnRetry != nil {
							act.OnRetry()
						}
					})
				}

				// Episodes in chunks of 100
				if st.Detail != nil && len(st.Detail.Episodes) > 0 {
					eps := st.Detail.Episodes
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
							ui.Textf(c, "选集（共 %d 集）", len(eps)).FontSize(16).FontWeight(600).TextColor(white)
							chunks := (len(eps) + 99) / 100
							if chunks > 1 {
								for ci := 0; ci < chunks; ci++ {
									lo := ci*100 + 1
									hi := min((ci+1)*100, len(eps))
									active := st.RangeIdx == ci
									rb := ui.ButtonBase(c.Key(fmt.Sprintf("rng-%d", ci))).Height(26).Padding(0, 10).Radius(6).Cursor(ui.CursorPointer)
									if active {
										rb.Background(ui.Hex("#44627a"))
									} else {
										rb.Background(ui.RGBA(255, 255, 255, 0.08))
									}
									rb.Children(func() {
										ui.Textf(c, "%d–%d", lo, hi).FontSize(12).TextColor(white).FontFeatures("tnum")
									})
									if rb.Clicked() {
										st.RangeIdx = ci
									}
								}
							}
						})

						start := st.RangeIdx * 100
						if start >= len(eps) {
							start = 0
						}
						end := min(start+100, len(eps))
						slice := eps[start:end]

						// Responsive columns based on window width
						perRow := max(5, int((winW-56+8)/(68+8)))
						for rowStart := 0; rowStart < len(slice); rowStart += perRow {
							rowEnd := min(rowStart+perRow, len(slice))
							ui.Row(c).FillWidth().Gap(8).Children(func() {
								for j := rowStart; j < rowEnd; j++ {
									ep := slice[j]
									idx := start + j
									lbl := fmt.Sprintf("%d", ep.Index)
									if ep.Locked {
										lbl += " 🔒"
									}
									eb := ui.ButtonBase(c.Key(fmt.Sprintf("ep-%d", idx))).
										Width(68).Height(36).Radius(8).Cursor(ui.CursorPointer)
									if idx+1 == resumeEp {
										eb.Background(ui.Hex("#44627a"))
									} else if eb.Hovered() {
										eb.Background(ui.RGBA(255, 255, 255, 0.18))
									} else {
										eb.Background(ui.RGBA(255, 255, 255, 0.09))
									}
									eb.Children(func() {
										ui.Box(c).Fill().Center().Children(func() {
											ui.Text(c, lbl).FontSize(12.5).TextColor(white).FontFeatures("tnum")
										})
									})
									if eb.Clicked() && !ep.Locked && act.OnPlayEpisode != nil {
										act.OnPlayEpisode(idx)
									}
								}
							})
						}
					})
				}

				// Related works / sequels
				if len(st.Related) > 0 {
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, "相关推荐 / 同系列").FontSize(16).FontWeight(600).TextColor(white)
						ui.Row(c).FillWidth().Gap(12).Children(func() {
							maxRel := max(6, int((winW-56+12)/(136+12)))
							for i, rel := range st.Related {
								if i >= maxRel {
									break
								}
								rb := ui.ButtonBase(c.Key("rel-" + rel.Ref.ID)).Width(136).Cursor(ui.CursorPointer)
								rb.Children(func() {
									ui.Column(c).FillWidth().Gap(6).Children(func() {
										ui.Box(c).Width(136).Height(184).Radius(9).Clip().
											Background(ui.RGBA(255, 255, 255, 0.08)).Children(func() {
											if bmp, alpha := img.Get(c, rel.Poster); bmp != nil {
												ui.Image(c, bmp).Fill().Fit(ui.Cover).Opacity(alpha)
											}
										})
										ui.Text(c, rel.Title).FontSize(12.5).TextColor(white).SingleLine()
										ui.Text(c, rel.Subtitle).FontSize(11).TextColor(muted).SingleLine()
									})
								})
								if rb.Clicked() && act.OnOpenRelated != nil {
									act.OnOpenRelated(rel)
								}
							}
						})
					})
				}
			})
		})
	})
}

func joinLimit(items []string, maxN int) string {
	if len(items) > maxN {
		items = items[:maxN]
	}
	out := ""
	for i, s := range items {
		if i > 0 {
			out += " · "
		}
		out += s
	}
	return out
}
