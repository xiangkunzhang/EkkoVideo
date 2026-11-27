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

	"github.com/dop251/goja"
	"github.com/xiangkunzhang/Velan/internal/core"
)

type flexStr string

func (f *flexStr) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*f = ""
		return nil
	}
	if s[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		*f = flexStr(strings.TrimSpace(str))
		return nil
	}
	*f = flexStr(s)
	return nil
}

type douyuAdapter struct {
	http *http.Client
}

func newDouyuAdapter(hc *http.Client) *douyuAdapter {
	return &douyuAdapter{http: hc}
}

func (d *douyuAdapter) Platform() PlatformInfo {
	return PlatformInfo{
		ID:   "douyu",
		Name: "斗鱼直播",
		Categories: []core.Section{
			{ID: "", Label: "全部"},
			{ID: "1", Label: "英雄联盟"},
			{ID: "181", Label: "王者荣耀"},
			{ID: "270", Label: "绝地求生"},
			{ID: "208", Label: "一起看"},
			{ID: "201", Label: "颜值"},
		},
	}
}

func (d *douyuAdapter) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.douyu.com/")
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("douyu http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (d *douyuAdapter) Rooms(ctx context.Context, category string, page int) ([]core.Item, bool, error) {
	offset := (page - 1) * 24
	path := "https://open.douyucdn.cn/api/RoomApi/live"
	if category != "" {
		path += "/" + url.PathEscape(category)
	}
	u := fmt.Sprintf("%s?offset=%d&limit=24", path, offset)
	var res struct {
		Error int `json:"error"`
		Data  []struct {
			RoomID   flexStr `json:"room_id"`
			RoomName string  `json:"room_name"`
			Nickname string  `json:"nickname"`
			RoomSrc  string  `json:"room_src"`
			CateName string  `json:"cate_name"`
			Online   int64   `json:"online"`
		} `json:"data"`
	}
	if err := d.getJSON(ctx, u, &res); err != nil {
		return nil, false, err
	}
	if res.Error != 0 {
		return nil, false, fmt.Errorf("douyu api error=%d", res.Error)
	}
	items := make([]core.Item, 0, len(res.Data))
	for _, r := range res.Data {
		rid := string(r.RoomID)
		if rid == "" {
			continue
		}
		items = append(items, core.Item{
			Ref:      core.ItemRef{Source: SourcePrefix + "douyu", Kind: core.KindLiveRoom, ID: rid},
			Title:    firstNonEmpty(r.RoomName, r.Nickname),
			Subtitle: r.Nickname,
			Poster:   r.RoomSrc,
			Playable: true,
			Extra: map[string]string{
				"streamer": r.Nickname,
				"area":     r.CateName,
				"online":   formatOnline(r.Online),
			},
		})
	}
	return items, len(items) == 24, nil
}

func (d *douyuAdapter) Search(ctx context.Context, query string) ([]core.Item, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if isDigits(q) {
		var res struct {
			Error int `json:"error"`
			Data  struct {
				RoomID     flexStr `json:"room_id"`
				RoomName   string  `json:"room_name"`
				OwnerName  string  `json:"owner_name"`
				RoomThumb  string  `json:"room_thumb"`
				CateName   string  `json:"cate_name"`
				Online     int64   `json:"online"`
				RoomStatus flexStr `json:"room_status"`
			} `json:"data"`
		}
		u := "https://open.douyucdn.cn/api/RoomApi/room/" + url.PathEscape(q)
		if err := d.getJSON(ctx, u, &res); err == nil && res.Error == 0 && res.Data.RoomID != "" {
			return []core.Item{{
				Ref:      core.ItemRef{Source: SourcePrefix + "douyu", Kind: core.KindLiveRoom, ID: string(res.Data.RoomID)},
				Title:    firstNonEmpty(res.Data.RoomName, res.Data.OwnerName),
				Subtitle: res.Data.OwnerName,
				Poster:   res.Data.RoomThumb,
				Playable: true,
				Extra: map[string]string{
					"streamer": res.Data.OwnerName,
					"area":     res.Data.CateName,
					"online":   formatOnline(res.Data.Online),
				},
			}}, nil
		}
	}
	u := "https://www.douyu.com/japi/search/api/searchShow?kw=" + url.QueryEscape(q) + "&page=1&pageSize=20"
	var res struct {
		Error int `json:"error"`
		Data  struct {
			RelateShow []struct {
				RID      int64  `json:"rid"`
				RoomName string `json:"roomName"`
				NickName string `json:"nickName"`
				RoomSrc  string `json:"roomSrc"`
				CateName string `json:"cateName"`
			} `json:"relateShow"`
		} `json:"data"`
	}
	if err := d.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	out := make([]core.Item, 0, len(res.Data.RelateShow))
	for _, r := range res.Data.RelateShow {
		if r.RID <= 0 {
			continue
		}
		out = append(out, core.Item{
			Ref:      core.ItemRef{Source: SourcePrefix + "douyu", Kind: core.KindLiveRoom, ID: strconv.FormatInt(r.RID, 10)},
			Title:    firstNonEmpty(r.RoomName, r.NickName),
			Subtitle: r.NickName,
			Poster:   r.RoomSrc,
			Playable: true,
			Extra: map[string]string{
				"streamer": r.NickName,
				"area":     r.CateName,
			},
		})
	}
	return out, nil
}

func (d *douyuAdapter) ResolveRoom(ctx context.Context, roomID string) (*core.PlaySource, error) {
	u := "https://open.douyucdn.cn/api/RoomApi/room/" + url.PathEscape(roomID)
	var res struct {
		Error int `json:"error"`
		Data  struct {
			RoomID     flexStr `json:"room_id"`
			RoomName   string  `json:"room_name"`
			RoomStatus flexStr `json:"room_status"`
		} `json:"data"`
	}
	if err := d.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	rid := string(res.Data.RoomID)
	if res.Error != 0 || rid == "" {
		return nil, fmt.Errorf("douyu: 房间 %s 不存在", roomID)
	}
	if string(res.Data.RoomStatus) != "1" {
		return nil, fmt.Errorf("douyu: 主播暂未开播")
	}

	// Fetch dynamic JS signer (ub98484234) and evaluate via pure-Go goja
	var encRes struct {
		Error int               `json:"error"`
		Data  map[string]string `json:"data"`
	}
	if err := d.getJSON(ctx, "https://www.douyu.com/swf_api/homeH5Enc?rids="+url.QueryEscape(rid), &encRes); err != nil {
		return nil, err
	}
	jsCode := encRes.Data["room"+rid]
	if jsCode == "" {
		return nil, fmt.Errorf("douyu: 未获取到房间签名脚本")
	}

	const did = "10000000000000000000000000001501"
	tt := strconv.FormatInt(time.Now().Unix(), 10)
	signedQuery, err := evalDouyuSign(jsCode, rid, did, tt)
	if err != nil {
		return nil, fmt.Errorf("douyu sign: %w", err)
	}

	form, _ := url.ParseQuery(signedQuery)
	form.Set("cdn", "")
	form.Set("rate", "0")
	form.Set("ver", "Douyu_223061205")
	form.Set("iar", "0")
	form.Set("ive", "0")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.douyu.com/lapi/live/getH5Play/"+url.PathEscape(rid),
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.douyu.com/"+rid)
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var playRes struct {
		Error int    `json:"error"`
		Msg   string `json:"msg"`
		Data  struct {
			RtmpURL  string `json:"rtmp_url"`
			RtmpLive string `json:"rtmp_live"`
			CDNs     []struct {
				Name string `json:"name"`
				CDN  string `json:"cdn"`
			} `json:"cdnsWithName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &playRes); err != nil {
		return nil, fmt.Errorf("douyu decode play: %w", err)
	}
	if playRes.Error != 0 || playRes.Data.RtmpURL == "" || playRes.Data.RtmpLive == "" {
		return nil, fmt.Errorf("douyu: 取流失败 (%d %s)", playRes.Error, playRes.Msg)
	}

	headers := map[string]string{
		"Referer":    "https://www.douyu.com/" + rid,
		"User-Agent": biliUA,
	}
	streamURL := strings.TrimRight(playRes.Data.RtmpURL, "/") + "/" + strings.TrimLeft(playRes.Data.RtmpLive, "/")
	return &core.PlaySource{
		Ref:     core.ItemRef{Source: SourcePrefix + "douyu", Kind: core.KindLiveRoom, ID: rid},
		Title:   res.Data.RoomName,
		URL:     streamURL,
		Kind:    core.StreamFLV,
		Headers: headers,
		Lines: []core.Line{{
			ID:      "flv-0",
			Label:   "FLV 原画",
			URL:     streamURL,
			Kind:    core.StreamFLV,
			Headers: headers,
		}},
	}, nil
}

func evalDouyuSign(jsCode, rid, did, tt string) (string, error) {
	vm := goja.New()
	_ = vm.Set("__md5", func(s string) string { return md5Hex(s) })
	_, err := vm.RunString(`var CryptoJS = { MD5: function(s) { return { toString: function() { return __md5(String(s)); } }; } };`)
	if err != nil {
		return "", err
	}
	if _, err := vm.RunString(jsCode); err != nil {
		return "", err
	}
	fn, ok := goja.AssertFunction(vm.Get("ub98484234"))
	if !ok {
		return "", fmt.Errorf("ub98484234 not found")
	}
	val, err := fn(goja.Undefined(), vm.ToValue(rid), vm.ToValue(did), vm.ToValue(tt))
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

func (d *douyuAdapter) StreamDanmaku(ctx context.Context, roomID string, out chan<- ChatMessage) error {
	for ctx.Err() == nil {
		if err := d.streamWS(ctx, roomID, out); err == nil || ctx.Err() != nil {
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
