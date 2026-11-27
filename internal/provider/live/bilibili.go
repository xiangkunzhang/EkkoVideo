package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xiangkunzhang/Velan/internal/core"
)

const biliUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type bilibiliAdapter struct {
	http *http.Client
}

func newBilibiliAdapter(hc *http.Client) *bilibiliAdapter {
	return &bilibiliAdapter{http: hc}
}

func (b *bilibiliAdapter) Platform() PlatformInfo {
	return PlatformInfo{
		ID:   "bilibili",
		Name: "哔哩哔哩直播",
		Categories: []core.Section{
			{ID: "0", Label: "推荐"},
			{ID: "2", Label: "网游"},
			{ID: "3", Label: "手游"},
			{ID: "6", Label: "单机"},
			{ID: "1", Label: "娱乐"},
			{ID: "5", Label: "电台"},
		},
	}
}

func (b *bilibiliAdapter) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("bilibili live http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (b *bilibiliAdapter) Rooms(ctx context.Context, category string, page int) ([]core.Item, bool, error) {
	parentArea := category
	if parentArea == "" {
		parentArea = "0"
	}
	u := fmt.Sprintf("https://api.live.bilibili.com/room/v3/area/getRoomList?platform=web&parent_area_id=%s&area_id=0&sort_type=online&page=%d&page_size=24",
		url.QueryEscape(parentArea), page)
	if parentArea == "0" {
		u = fmt.Sprintf("https://api.live.bilibili.com/room/v3/area/getRoomList?platform=web&parent_area_id=2&area_id=0&sort_type=online&page=%d&page_size=24", page)
	}
	var res struct {
		Code int `json:"code"`
		Data struct {
			Count   int `json:"count"`
			HasMore int `json:"has_more"`
			List    []struct {
				RoomID    int64  `json:"roomid"`
				Title     string `json:"title"`
				Uname     string `json:"uname"`
				UserCover string `json:"user_cover"`
				SystemCov string `json:"system_cover"`
				Cover     string `json:"cover"`
				AreaName  string `json:"area_name"`
				Online    int64  `json:"online"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, u, &res); err != nil {
		return nil, false, err
	}
	if res.Code != 0 {
		return nil, false, fmt.Errorf("bilibili live code=%d", res.Code)
	}
	items := make([]core.Item, 0, len(res.Data.List))
	for _, r := range res.Data.List {
		if r.RoomID <= 0 {
			continue
		}
		cover := firstNonEmpty(r.UserCover, r.Cover, r.SystemCov)
		items = append(items, core.Item{
			Ref:      core.ItemRef{Source: SourcePrefix + "bilibili", Kind: core.KindLiveRoom, ID: strconv.FormatInt(r.RoomID, 10)},
			Title:    r.Title,
			Subtitle: r.Uname,
			Poster:   cover,
			Playable: true,
			Extra: map[string]string{
				"streamer": r.Uname,
				"area":     r.AreaName,
				"online":   formatOnline(r.Online),
			},
		})
	}
	hasMore := res.Data.HasMore == 1 || (res.Data.Count > page*24) || len(items) >= 24
	return items, hasMore && len(items) > 0, nil
}

func (b *bilibiliAdapter) Search(ctx context.Context, query string) ([]core.Item, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if isDigits(q) {
		var info struct {
			Code int `json:"code"`
			Data struct {
				RoomID     int64  `json:"room_id"`
				Title      string `json:"title"`
				UserCover  string `json:"user_cover"`
				AreaName   string `json:"area_name"`
				Online     int64  `json:"online"`
				LiveStatus int    `json:"live_status"`
			} `json:"data"`
		}
		u := "https://api.live.bilibili.com/room/v1/Room/get_info?room_id=" + url.QueryEscape(q)
		if err := b.getJSON(ctx, u, &info); err == nil && info.Code == 0 && info.Data.RoomID > 0 {
			sub := "房间 " + strconv.FormatInt(info.Data.RoomID, 10)
			if info.Data.LiveStatus != 1 {
				sub += "（未开播）"
			}
			return []core.Item{{
				Ref:      core.ItemRef{Source: SourcePrefix + "bilibili", Kind: core.KindLiveRoom, ID: strconv.FormatInt(info.Data.RoomID, 10)},
				Title:    info.Data.Title,
				Subtitle: sub,
				Poster:   info.Data.UserCover,
				Playable: true,
				Extra: map[string]string{
					"area":   info.Data.AreaName,
					"online": formatOnline(info.Data.Online),
				},
			}}, nil
		}
	}
	u := "https://api.bilibili.com/x/web-interface/search/type?search_type=live_room&keyword=" + url.QueryEscape(q)
	var res struct {
		Code int `json:"code"`
		Data struct {
			Result []struct {
				RoomID     int64  `json:"roomid"`
				Title      string `json:"title"`
				Uname      string `json:"uname"`
				UserCover  string `json:"user_cover"`
				Cover      string `json:"cover"`
				CateName   string `json:"cate_name"`
				Online     int64  `json:"online"`
				LiveStatus int    `json:"live_status"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	out := make([]core.Item, 0, len(res.Data.Result))
	for _, r := range res.Data.Result {
		if r.RoomID <= 0 {
			continue
		}
		title := stripHTMLTags(r.Title)
		cov := firstNonEmpty(r.UserCover, r.Cover)
		if strings.HasPrefix(cov, "//") {
			cov = "https:" + cov
		}
		out = append(out, core.Item{
			Ref:      core.ItemRef{Source: SourcePrefix + "bilibili", Kind: core.KindLiveRoom, ID: strconv.FormatInt(r.RoomID, 10)},
			Title:    title,
			Subtitle: r.Uname,
			Poster:   cov,
			Playable: true,
			Extra: map[string]string{
				"streamer": r.Uname,
				"area":     r.CateName,
				"online":   formatOnline(r.Online),
			},
		})
	}
	return out, nil
}

func (b *bilibiliAdapter) ResolveRoom(ctx context.Context, roomID string) (*core.PlaySource, error) {
	var info struct {
		Code int `json:"code"`
		Data struct {
			RoomID     int64  `json:"room_id"`
			Title      string `json:"title"`
			LiveStatus int    `json:"live_status"`
		} `json:"data"`
	}
	infoURL := "https://api.live.bilibili.com/room/v1/Room/get_info?room_id=" + url.QueryEscape(roomID)
	if err := b.getJSON(ctx, infoURL, &info); err != nil {
		return nil, err
	}
	if info.Code != 0 || info.Data.RoomID <= 0 {
		return nil, fmt.Errorf("bilibili live: 房间 %s 不存在", roomID)
	}
	if info.Data.LiveStatus != 1 {
		return nil, fmt.Errorf("bilibili live: 主播暂未开播")
	}

	realID := strconv.FormatInt(info.Data.RoomID, 10)
	playURL := fmt.Sprintf("https://api.live.bilibili.com/xlive/web-room/v2/index/getRoomPlayInfo?room_id=%s&no_playurl=0&mask=1&qn=10000&platform=web&protocol=0,1&format=0,1,2&codec=0,1", realID)
	var playRes struct {
		Code int `json:"code"`
		Data struct {
			PlayurlInfo struct {
				Playurl struct {
					Stream []struct {
						ProtocolName string `json:"protocol_name"`
						Format       []struct {
							FormatName string `json:"format_name"`
							Codec      []struct {
								CodecName string `json:"codec_name"`
								BaseURL   string `json:"base_url"`
								URLInfo   []struct {
									Host  string `json:"host"`
									Extra string `json:"extra"`
								} `json:"url_info"`
							} `json:"codec"`
						} `json:"format"`
					} `json:"stream"`
				} `json:"playurl"`
			} `json:"playurl_info"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, playURL, &playRes); err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Referer":    "https://live.bilibili.com/" + realID,
		"User-Agent": biliUA,
	}
	var lines []core.Line
	for _, st := range playRes.Data.PlayurlInfo.Playurl.Stream {
		kind := core.StreamFLV
		if strings.Contains(st.ProtocolName, "hls") {
			kind = core.StreamHLS
		}
		for _, f := range st.Format {
			for _, cd := range f.Codec {
				for idx, ui := range cd.URLInfo {
					full := ui.Host + cd.BaseURL + ui.Extra
					if full == "" {
						continue
					}
					lines = append(lines, core.Line{
						ID:      fmt.Sprintf("%s-%s-%d", f.FormatName, cd.CodecName, idx),
						Label:   fmt.Sprintf("%s · %s 线路%d", strings.ToUpper(f.FormatName), cd.CodecName, idx+1),
						URL:     full,
						Kind:    kind,
						Headers: headers,
					})
				}
			}
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("bilibili live: 未获取到可用直播流")
	}
	return &core.PlaySource{
		Ref:     core.ItemRef{Source: SourcePrefix + "bilibili", Kind: core.KindLiveRoom, ID: realID},
		Title:   info.Data.Title,
		URL:     lines[0].URL,
		Kind:    lines[0].Kind,
		Headers: headers,
		Lines:   lines,
	}, nil
}

// StreamDanmaku streams Bilibili Live chat messages via WebSocket and also
// polls gethistory as a reliable fallback so plain chat danmaku always arrives
// (strictly ignoring gifts/superchats).
func (b *bilibiliAdapter) StreamDanmaku(ctx context.Context, roomID string, out chan<- ChatMessage) error {
	// Run history poller in parallel as fallback when WS auth token is gated
	go b.pollHistory(ctx, roomID, out)

	for ctx.Err() == nil {
		if err := b.streamWS(ctx, roomID, out); err == nil || ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
	return nil
}

func (b *bilibiliAdapter) pollHistory(ctx context.Context, roomID string, out chan<- ChatMessage) {
	seen := make(map[string]struct{})
	order := make([]string, 0, 256)
	first := true

	fetchOnce := func() {
		u := "https://api.live.bilibili.com/xlive/web-room/v1/dM/gethistory?roomid=" + url.QueryEscape(roomID)
		var res struct {
			Code int `json:"code"`
			Data struct {
				Room []struct {
					Text     string `json:"text"`
					Nickname string `json:"nickname"`
					Timeline string `json:"timeline"`
					UID      int64  `json:"uid"`
				} `json:"room"`
			} `json:"data"`
		}
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := b.getJSON(reqCtx, u, &res); err != nil || res.Code != 0 {
			return
		}
		rows := res.Data.Room
		for idx, m := range rows {
			txt := strings.TrimSpace(m.Text)
			if txt == "" {
				continue
			}
			key := fmt.Sprintf("%d|%s|%s", m.UID, m.Timeline, txt)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			order = append(order, key)
			if len(order) > 400 {
				delete(seen, order[0])
				order = order[1:]
			}
			// On first poll, emit only the last 3 messages so user sees immediate chat feedback
			if first && idx < len(rows)-3 {
				continue
			}
			select {
			case out <- ChatMessage{Author: m.Nickname, Text: txt, Color: 0xFFFFFF, Time: time.Now()}:
			case <-ctx.Done():
				return
			}
		}
		first = false
	}

	fetchOnce()
	tk := time.NewTicker(2500 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			fetchOnce()
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func formatOnline(n int64) string {
	if n >= 10000 {
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	}
	if n > 0 {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
