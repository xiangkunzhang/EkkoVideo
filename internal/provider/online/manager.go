// Package online adapts the 5 online short-drama platforms into unified core models
// inside the isolated VelanGateway subprocess.
package online

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/provider/online/dsd"
	"github.com/xiangkunzhang/Velan/internal/provider/online/hongguo"
	"github.com/xiangkunzhang/Velan/internal/provider/online/huangdou"
	"github.com/xiangkunzhang/Velan/internal/provider/online/huangju"
	legacymodel "github.com/xiangkunzhang/Velan/internal/provider/online/legacymodel"
	"github.com/xiangkunzhang/Velan/internal/provider/online/yeguo"
)

// Manager owns the 5 online platform clients inside the gateway process.
type Manager struct {
	Hongguo  *hongguo.Client
	Yeguo    *yeguo.Client
	Huangdou *huangdou.Client
	Huangju  *huangju.Client
	DSD      *dsd.Client
}

// NewManager initializes all 5 online platform clients.
func NewManager(dataDir string) *Manager {
	hg := hongguo.New()
	_ = dataDir
	return &Manager{
		Hongguo:  hg,
		Yeguo:    yeguo.NewClient(),
		Huangdou: huangdou.NewClient(),
		Huangju:  huangju.NewClient(),
		DSD:      dsd.NewClient(),
	}
}

// Providers returns metadata for all 5 online platforms.
func (m *Manager) Providers() []core.ProviderInfo {
	return []core.ProviderInfo{
		{ID: "online:hongguo", Name: "红果短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
		{ID: "online:yeguo", Name: "野果短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
		{ID: "online:huangdou", Name: "黄豆短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
		{ID: "online:huangju", Name: "黄桔短剧", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
		{ID: "online:dsd", Name: "短剧岛", Type: core.ProviderOnline, ListStyle: core.ListPosterGrid},
	}
}

// SectionsResponse describes modes and category chips for a platform.
type SectionsResponse struct {
	Modes      []core.Section `json:"modes"`
	Categories []core.Section `json:"categories"`
}

func (m *Manager) Sections(source string) SectionsResponse {
	src := trimSource(source)
	switch src {
	case "yeguo":
		return SectionsResponse{
			Modes: []core.Section{{ID: "recommend", Label: "推荐"}, {ID: "rank", Label: "榜单"}},
		}
	case "huangdou":
		return SectionsResponse{
			Modes: []core.Section{{ID: "recommend", Label: "推荐"}, {ID: "rank", Label: "热播"}},
		}
	case "huangju":
		cats, _ := m.Huangju.Categories()
		var out []core.Section
		for _, c := range cats {
			out = append(out, core.Section{ID: c.Name, Label: c.Name})
		}
		return SectionsResponse{
			Modes:      []core.Section{{ID: "recommend", Label: "全部"}, {ID: "category", Label: "分类"}},
			Categories: out,
		}
	case "dsd":
		return SectionsResponse{
			Modes: []core.Section{{ID: "recommend", Label: "最新"}, {ID: "rank", Label: "热门"}},
		}
	default: // hongguo
		return SectionsResponse{
			Modes: []core.Section{
				{ID: "recommend", Label: "推荐"},
				{ID: "feed", Label: "随心看"},
				{ID: "category", Label: "分类"},
				{ID: "rank", Label: "热榜"},
			},
			Categories: []core.Section{
				{ID: "real-drama", Label: "真人短剧"},
				{ID: "comic-drama", Label: "动漫短剧"},
				{ID: "ai-drama", Label: "AI 短剧"},
				{ID: "hot-drama", Label: "热剧榜"},
				{ID: "hot-real-drama", Label: "真人热榜"},
				{ID: "new-drama", Label: "新剧榜"},
			},
		}
	}
}

func (m *Manager) Browse(source string, q core.Query) (core.Page, error) {
	src := trimSource(source)
	pageNum := 1
	if q.Cursor != "" {
		if n, err := strconv.Atoi(q.Cursor); err == nil && n > 0 {
			pageNum = n
		}
	}
	mode := q.Section
	if mode == "" {
		mode = "recommend"
	}

	switch src {
	case "yeguo":
		items, total, err := m.Yeguo.Browse(mode, "", pageNum, 24)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:yeguo", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("%d 集", d.TotalEpisodes),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Playable: false,
				Children: d.TotalEpisodes,
			})
		}
		next := ""
		if pageNum*24 < total {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next, Total: total}, nil

	case "huangdou":
		items, hasMore, err := m.Huangdou.Browse(mode, "", pageNum, 24)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:huangdou", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("%d 集", d.TotalEpisodes),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: d.TotalEpisodes,
			})
		}
		next := ""
		if hasMore {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next}, nil

	case "huangju":
		items, total, err := m.Huangju.Browse("", q.Category, q.Sort, pageNum, 24)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:huangju", Kind: core.KindSeries, ID: fmt.Sprintf("yanyu-%d", d.ID)},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("%d 集", d.TotalEpisodes),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: d.TotalEpisodes,
			})
		}
		next := ""
		if pageNum*24 < total {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next, Total: total}, nil

	case "dsd":
		items, hasMore, err := m.DSD.Browse(mode, q.Category, q.Sort, "", pageNum)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:dsd", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: d.Duration,
				Poster:   d.Cover,
				Children: 1,
			})
		}
		next := ""
		if hasMore {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next}, nil

	default: // hongguo
		var res *legacymodel.BrowseResult
		var err error
		switch mode {
		case "feed":
			res, err = m.Hongguo.RandomFeed(pageNum == 1, pageNum)
		case "category":
			cat := q.Category
			switch cat {
			case "real-drama", "comic-drama", "ai-drama":
			default:
				cat = "real-drama"
			}
			res, _, err = m.Hongguo.CategoryBrowse(cat, "", pageNum)
		case "rank":
			board := q.Category
			switch board {
			case "hot-drama", "hot-real-drama", "new-drama":
			default:
				board = "hot-drama"
			}
			res, err = m.Hongguo.RankBoard(board, pageNum)
		default:
			res, err = m.Hongguo.Recommend(pageNum)
		}
		if err != nil {
			return core.Page{}, err
		}
		return convertLegacyBrowse("online:hongguo", res, pageNum), nil
	}
}

func (m *Manager) Search(source, query, cursor string) (core.Page, error) {
	src := trimSource(source)
	pageNum := 1
	if cursor != "" {
		if n, err := strconv.Atoi(cursor); err == nil && n > 0 {
			pageNum = n
		}
	}

	switch src {
	case "yeguo":
		items, total, err := m.Yeguo.Browse("", query, pageNum, 24)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:yeguo", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("%d 集", d.TotalEpisodes),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: d.TotalEpisodes,
			})
		}
		next := ""
		if pageNum*24 < total {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next, Total: total}, nil

	case "huangdou":
		items, hasMore, err := m.Huangdou.Browse("", query, pageNum, 24)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:huangdou", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("%d 集", d.TotalEpisodes),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: d.TotalEpisodes,
			})
		}
		next := ""
		if hasMore {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next}, nil

	case "huangju":
		items, total, err := m.Huangju.Browse(query, "", "", pageNum, 24)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:huangju", Kind: core.KindSeries, ID: fmt.Sprintf("yanyu-%d", d.ID)},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("%d 集", d.TotalEpisodes),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: d.TotalEpisodes,
			})
		}
		next := ""
		if pageNum*24 < total {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next, Total: total}, nil

	case "dsd":
		items, hasMore, err := m.DSD.Browse("", "", "", query, pageNum)
		if err != nil {
			return core.Page{}, err
		}
		out := make([]core.Item, 0, len(items))
		for _, d := range items {
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:dsd", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: d.Duration,
				Poster:   d.Cover,
				Children: 1,
			})
		}
		next := ""
		if hasMore {
			next = strconv.Itoa(pageNum + 1)
		}
		return core.Page{Items: out, Next: next}, nil

	default: // hongguo
		items, err := m.Hongguo.Search(query)
		if err != nil {
			return core.Page{}, err
		}
		var out []core.Item
		seen := map[string]bool{}
		addVideo := func(v hongguo.VideoData) {
			if v.SeriesID == "" || seen[v.SeriesID] {
				return
			}
			seen[v.SeriesID] = true
			sub := v.EpisodeRightText
			if sub == "" && v.EpisodeCnt > 0 {
				sub = fmt.Sprintf("%d 集", v.EpisodeCnt)
			}
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:hongguo", Kind: core.KindSeries, ID: v.SeriesID},
				Title:    v.SeriesTitle,
				Subtitle: sub,
				Poster:   v.SeriesCover,
				Overview: v.SeriesIntro,
				Children: v.EpisodeCnt,
			})
		}
		for _, it := range items {
			addVideo(it.VideoData)
			for _, v := range it.VideoList {
				addVideo(v)
			}
		}
		return core.Page{Items: out, Total: len(out)}, nil
	}
}

func (m *Manager) Detail(source, id string) (*core.Detail, error) {
	src := trimSource(source)
	switch src {
	case "yeguo":
		d, err := m.Yeguo.Detail(id)
		if err != nil {
			return nil, err
		}
		actors, tags := m.Yeguo.DetailExtra(id)
		eps := make([]core.Item, 0, len(d.Episodes))
		for _, ep := range d.Episodes {
			eps = append(eps, core.Item{
				Ref:      core.ItemRef{Source: "online:yeguo", Kind: core.KindEpisode, ID: ep.ID},
				Title:    fmt.Sprintf("第 %d 集", ep.EpNo),
				Index:    ep.EpNo,
				Playable: ep.Playable,
				Locked:   !ep.Playable,
				Extra:    map[string]string{"seriesId": d.ID, "ep": strconv.Itoa(ep.EpNo)},
			})
		}
		return &core.Detail{
			Item: core.Item{
				Ref:      core.ItemRef{Source: "online:yeguo", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("共 %d 集", len(eps)),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Tags:     tags,
				Children: len(eps),
			},
			Episodes: eps,
			Actors:   actors,
		}, nil

	case "huangdou":
		d, err := m.Huangdou.Detail(id)
		if err != nil {
			return nil, err
		}
		eps := make([]core.Item, 0, len(d.Episodes))
		for _, ep := range d.Episodes {
			eps = append(eps, core.Item{
				Ref:      core.ItemRef{Source: "online:huangdou", Kind: core.KindEpisode, ID: ep.ID},
				Title:    fmt.Sprintf("第 %d 集", ep.EpNo),
				Index:    ep.EpNo,
				Playable: ep.Playable,
				Locked:   !ep.Playable,
				Extra:    map[string]string{"seriesId": d.ID, "ep": strconv.Itoa(ep.EpNo)},
			})
		}
		return &core.Detail{
			Item: core.Item{
				Ref:      core.ItemRef{Source: "online:huangdou", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("共 %d 集", len(eps)),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: len(eps),
			},
			Episodes: eps,
		}, nil

	case "huangju":
		d, err := m.Huangju.Detail(id)
		if err != nil {
			return nil, err
		}
		sid := fmt.Sprintf("yanyu-%d", d.ID)
		eps := make([]core.Item, 0, len(d.Episodes))
		for _, ep := range d.Episodes {
			eps = append(eps, core.Item{
				Ref:      core.ItemRef{Source: "online:huangju", Kind: core.KindEpisode, ID: strconv.FormatInt(ep.ID, 10)},
				Title:    fmt.Sprintf("第 %d 集", ep.EpNo),
				Index:    ep.EpNo,
				Playable: ep.Playable,
				Locked:   !ep.Playable,
				Extra:    map[string]string{"seriesId": sid, "ep": strconv.Itoa(ep.EpNo)},
			})
		}
		return &core.Detail{
			Item: core.Item{
				Ref:      core.ItemRef{Source: "online:huangju", Kind: core.KindSeries, ID: sid},
				Title:    d.Title,
				Subtitle: fmt.Sprintf("共 %d 集", len(eps)),
				Poster:   d.CoverURL,
				Overview: d.Description,
				Children: len(eps),
			},
			Episodes: eps,
		}, nil

	case "dsd":
		d, err := m.DSD.Detail(id)
		if err != nil {
			return nil, err
		}
		return &core.Detail{
			Item: core.Item{
				Ref:      core.ItemRef{Source: "online:dsd", Kind: core.KindSeries, ID: d.ID},
				Title:    d.Title,
				Subtitle: "全 1 集",
				Poster:   d.Cover,
				Children: 1,
			},
			Episodes: []core.Item{{
				Ref:      core.ItemRef{Source: "online:dsd", Kind: core.KindEpisode, ID: d.ID},
				Title:    "第 1 集",
				Index:    1,
				Playable: true,
				Extra:    map[string]string{"seriesId": d.ID, "ep": "1"},
			}},
		}, nil

	default: // hongguo
		d, err := m.Hongguo.Detail(id)
		if err != nil {
			return nil, err
		}
		eps := make([]core.Item, 0, len(d.VidList))
		for i, v := range d.VidList {
			eps = append(eps, core.Item{
				Ref:      core.ItemRef{Source: "online:hongguo", Kind: core.KindEpisode, ID: v},
				Title:    fmt.Sprintf("第 %d 集", i+1),
				Index:    i + 1,
				Playable: true,
				Extra:    map[string]string{"seriesId": d.SeriesID, "ep": strconv.Itoa(i + 1)},
			})
		}
		var actors []string
		for _, c := range d.Celebrities {
			if nick := strings.TrimSpace(c.Nickname); nick != "" {
				actors = append(actors, nick)
			}
		}
		return &core.Detail{
			Item: core.Item{
				Ref:      core.ItemRef{Source: "online:hongguo", Kind: core.KindSeries, ID: d.SeriesID},
				Title:    d.SeriesName,
				Subtitle: fmt.Sprintf("共 %d 集", d.EpisodeCnt),
				Poster:   d.SeriesCover,
				Overview: d.SeriesIntro,
				Tags:     d.Tags,
				Children: len(eps),
			},
			Episodes: eps,
			Actors:   actors,
		}, nil
	}
}

func (m *Manager) Related(source, id, title string) ([]core.Item, error) {
	if trimSource(source) != "hongguo" || id == "" {
		return nil, nil
	}
	works, err := m.Hongguo.Related(id, title)
	if err == nil && len(works) > 0 {
		out := make([]core.Item, 0, len(works))
		for _, w := range works {
			sub := fmt.Sprintf("%d 集", w.EpisodeCnt)
			if w.Season != "" {
				sub = w.Season + " · " + sub
			}
			out = append(out, core.Item{
				Ref:      core.ItemRef{Source: "online:hongguo", Kind: core.KindSeries, ID: w.SeriesID},
				Title:    w.Title,
				Subtitle: sub,
				Poster:   w.Cover,
				Overview: w.Intro,
				Children: w.EpisodeCnt,
			})
		}
		return out, nil
	}
	// Fallback: search by base title (e.g. other seasons) or recommend feed
	if base := strings.TrimRight(title, "0123456789第季 "); base != "" {
		if page, sErr := m.Search(source, base, ""); sErr == nil && len(page.Items) > 0 {
			var out []core.Item
			for _, it := range page.Items {
				if it.Ref.ID != id {
					out = append(out, it)
				}
				if len(out) >= 10 {
					break
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		}
	}
	if page, rErr := m.Browse(source, core.Query{Section: "recommend"}); rErr == nil {
		var out []core.Item
		for _, it := range page.Items {
			if it.Ref.ID != id {
				out = append(out, it)
			}
			if len(out) >= 10 {
				break
			}
		}
		return out, nil
	}
	return nil, err
}

// ResolvePlay resolves a playable URL (or gateway stream path for CENC/HLS) for an episode.
func (m *Manager) ResolvePlay(gwAddr, streamToken, source, seriesID, vid string, ep int, quality string) (*core.PlaySource, error) {
	if quality == "" {
		quality = "720p"
	}
	src := trimSource(source)
	if ep <= 0 {
		ep = 1
	}

	switch src {
	case "yeguo":
		u, err := m.Yeguo.Play(seriesID, vid)
		if err != nil {
			return nil, err
		}
		return &core.PlaySource{
			Ref:   core.ItemRef{Source: "online:yeguo", Kind: core.KindEpisode, ID: vid},
			Title: fmt.Sprintf("第 %d 集", ep),
			URL:   u,
			Kind:  core.StreamHLS,
			Danmaku: &core.DanmakuRef{
				Source:   "online:yeguo",
				SeriesID: seriesID,
				ItemID:   vid,
			},
		}, nil

	case "huangdou":
		u, err := m.Huangdou.Play(seriesID, ep)
		if err != nil {
			return nil, err
		}
		return &core.PlaySource{
			Ref:   core.ItemRef{Source: "online:huangdou", Kind: core.KindEpisode, ID: vid},
			Title: fmt.Sprintf("第 %d 集", ep),
			URL:   u,
			Kind:  core.StreamHLS,
		}, nil

	case "huangju":
		epID, _ := strconv.ParseInt(vid, 10, 64)
		res, err := m.Huangju.Play(epID)
		if err != nil {
			return nil, err
		}
		return &core.PlaySource{
			Ref:   core.ItemRef{Source: "online:huangju", Kind: core.KindEpisode, ID: vid},
			Title: fmt.Sprintf("第 %d 集", ep),
			URL:   res.URL,
			Kind:  core.StreamHLS,
		}, nil

	case "dsd":
		u, d, err := m.DSD.Play(seriesID)
		if err != nil {
			return nil, err
		}
		return &core.PlaySource{
			Ref:   core.ItemRef{Source: "online:dsd", Kind: core.KindEpisode, ID: seriesID},
			Title: d.Title,
			URL:   u,
			Kind:  core.StreamHLS,
		}, nil

	default: // hongguo
		d, err := m.Hongguo.Detail(seriesID)
		if err != nil {
			return nil, err
		}
		if vid == "" && ep-1 >= 0 && ep-1 < len(d.VidList) {
			vid = d.VidList[ep-1]
		}
		st, err := m.Hongguo.Resolve(d.SeriesID, vid, quality)
		if err != nil {
			return nil, err
		}
		title := fmt.Sprintf("%s · 第 %d 集", d.SeriesName, ep)
		dmRef := &core.DanmakuRef{Source: "online:hongguo", SeriesID: d.SeriesID, ItemID: vid}
		if st.Direct != "" {
			return &core.PlaySource{
				Ref:     core.ItemRef{Source: "online:hongguo", Kind: core.KindEpisode, ID: vid},
				Title:   title,
				URL:     st.Direct,
				Kind:    core.StreamMP4,
				Danmaku: dmRef,
			}, nil
		}
		// CENC encrypted stream: route through VelanGateway Range-CENC decryption proxy!
		proxyURL := fmt.Sprintf("http://%s/s/%s/stream.mp4?series_id=%s&ep=%d&definition=%s",
			gwAddr, streamToken, url.QueryEscape(d.SeriesID), ep, url.QueryEscape(quality))
		return &core.PlaySource{
			Ref:     core.ItemRef{Source: "online:hongguo", Kind: core.KindEpisode, ID: vid},
			Title:   title,
			URL:     proxyURL,
			Kind:    core.StreamMP4,
			Decrypt: &core.DecryptSpec{Scheme: "cenc"},
			Danmaku: dmRef,
		}, nil
	}
}

func convertLegacyBrowse(sourceID string, res *legacymodel.BrowseResult, pageNum int) core.Page {
	if res == nil {
		return core.Page{}
	}
	out := make([]core.Item, 0, len(res.Items))
	for _, it := range res.Items {
		sub := it.RightText
		if sub == "" && it.EpisodeCnt > 0 {
			sub = fmt.Sprintf("%d 集", it.EpisodeCnt)
		}
		out = append(out, core.Item{
			Ref:      core.ItemRef{Source: sourceID, Kind: core.KindSeries, ID: it.SeriesID},
			Title:    it.Title,
			Subtitle: sub,
			Poster:   it.Cover,
			Overview: it.Intro,
			Tags:     it.Categories,
			Index:    it.Rank,
			Children: it.EpisodeCnt,
		})
	}
	next := ""
	if res.HasMore {
		if res.NextCursor != "" {
			next = res.NextCursor
		} else {
			next = strconv.Itoa(pageNum + 1)
		}
	}
	return core.Page{Items: out, Next: next, Total: res.Total}
}

func trimSource(s string) string {
	return strings.TrimPrefix(s, "online:")
}

// Danmaku fetches timed comments for an episode on supported platforms (hongguo, yeguo).
func (m *Manager) Danmaku(source, seriesID, epID string) ([]core.Comment, error) {
	switch trimSource(source) {
	case "hongguo":
		items, err := m.Hongguo.Danmaku(seriesID, epID)
		if err != nil {
			return nil, err
		}
		out := make([]core.Comment, 0, len(items))
		for _, it := range items {
			out = append(out, core.Comment{TimeSec: it.TimeSec, Text: it.Text})
		}
		return out, nil
	case "yeguo":
		items, err := m.Yeguo.Danmaku(seriesID, epID)
		if err != nil {
			return nil, err
		}
		out := make([]core.Comment, 0, len(items))
		for _, it := range items {
			out = append(out, core.Comment{TimeSec: it.TimeSec, Text: it.Text})
		}
		return out, nil
	default:
		return nil, nil
	}
}
