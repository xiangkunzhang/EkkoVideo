package live

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
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

type huyaAdapter struct {
	http *http.Client
}

func newHuyaAdapter(hc *http.Client) *huyaAdapter {
	return &huyaAdapter{http: hc}
}

func (h *huyaAdapter) Platform() PlatformInfo {
	return PlatformInfo{
		ID:   "huya",
		Name: "虎牙直播",
		Categories: []core.Section{
			{ID: "0", Label: "全部"},
			{ID: "1", Label: "英雄联盟"},
			{ID: "2336", Label: "王者荣耀"},
			{ID: "2135", Label: "一起看"},
			{ID: "1663", Label: "娱乐"},
		},
	}
}

func (h *huyaAdapter) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.huya.com/")
	resp, err := h.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("huya http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (h *huyaAdapter) Rooms(ctx context.Context, category string, page int) ([]core.Item, bool, error) {
	gid := category
	if gid == "" {
		gid = "0"
	}
	u := fmt.Sprintf("https://www.huya.com/cache.php?m=LiveList&do=getLiveListByPage&gameId=%s&tagAll=0&page=%d",
		url.QueryEscape(gid), page)
	var res struct {
		Status int `json:"status"`
		Data   struct {
			Page      int `json:"page"`
			TotalPage int `json:"totalPage"`
			Datas     []struct {
				ProfileRoom string `json:"profileRoom"`
				RoomName    string `json:"roomName"`
				Nick        string `json:"nick"`
				Screenshot  string `json:"screenshot"`
				GameFull    string `json:"gameFullName"`
				TotalCount  string `json:"totalCount"`
			} `json:"datas"`
		} `json:"data"`
	}
	if err := h.getJSON(ctx, u, &res); err != nil {
		return nil, false, err
	}
	items := make([]core.Item, 0, len(res.Data.Datas))
	for _, r := range res.Data.Datas {
		if r.ProfileRoom == "" {
			continue
		}
		cov := r.Screenshot
		if strings.HasPrefix(cov, "//") {
			cov = "https:" + cov
		}
		items = append(items, core.Item{
			Ref:      core.ItemRef{Source: SourcePrefix + "huya", Kind: core.KindLiveRoom, ID: r.ProfileRoom},
			Title:    firstNonEmpty(r.RoomName, r.Nick),
			Subtitle: r.Nick,
			Poster:   cov,
			Playable: true,
			Extra: map[string]string{
				"streamer": r.Nick,
				"area":     r.GameFull,
				"online":   r.TotalCount,
			},
		})
	}
	return items, res.Data.Page < res.Data.TotalPage && len(items) > 0, nil
}

func (h *huyaAdapter) Search(ctx context.Context, query string) ([]core.Item, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	u := fmt.Sprintf("https://search.cdn.huya.com/?m=Search&do=getSearchContent&q=%s&uid=0&v=4&typ=-5&rows=20",
		url.QueryEscape(q))
	var res struct {
		Response struct {
			Docs []struct {
				RoomID     int64  `json:"room_id"`
				GameName   string `json:"game_name"`
				Nick       string `json:"game_nick"`
				Intro      string `json:"game_introduction"`
				Screenshot string `json:"game_screenshot"`
			} `json:"docs"`
		} `json:"response"`
	}
	if err := h.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	out := make([]core.Item, 0, len(res.Response.Docs))
	for _, d := range res.Response.Docs {
		if d.RoomID <= 0 {
			continue
		}
		cov := d.Screenshot
		if strings.HasPrefix(cov, "//") {
			cov = "https:" + cov
		}
		out = append(out, core.Item{
			Ref:      core.ItemRef{Source: SourcePrefix + "huya", Kind: core.KindLiveRoom, ID: fmt.Sprintf("%d", d.RoomID)},
			Title:    firstNonEmpty(d.Intro, d.Nick),
			Subtitle: d.Nick,
			Poster:   cov,
			Playable: true,
			Extra: map[string]string{
				"streamer": d.Nick,
				"area":     d.GameName,
			},
		})
	}
	return out, nil
}

func (h *huyaAdapter) ResolveRoom(ctx context.Context, roomID string) (*core.PlaySource, error) {
	u := "https://mp.huya.com/cache.php?m=Live&do=profileRoom&roomid=" + url.QueryEscape(roomID)
	var res struct {
		Status int    `json:"status"`
		Msg    string `json:"message"`
		Data   struct {
			LiveStatus string `json:"liveStatus"`
			LiveData   struct {
				Introduction string `json:"introduction"`
				Nick         string `json:"nick"`
			} `json:"liveData"`
			Stream struct {
				BaseSteamInfoList []struct {
					CDNType     string `json:"sCdnType"`
					FlvURL      string `json:"sFlvUrl"`
					FlvSuffix   string `json:"sFlvUrlSuffix"`
					FlvAntiCode string `json:"sFlvAntiCode"`
					HlsURL      string `json:"sHlsUrl"`
					HlsSuffix   string `json:"sHlsUrlSuffix"`
					HlsAntiCode string `json:"sHlsAntiCode"`
					StreamName  string `json:"sStreamName"`
				} `json:"baseSteamInfoList"`
			} `json:"stream"`
		} `json:"data"`
	}
	if err := h.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	if res.Data.LiveStatus != "ON" {
		return nil, fmt.Errorf("huya: 主播暂未开播")
	}
	headers := map[string]string{
		"Referer":    "https://www.huya.com/",
		"User-Agent": biliUA,
	}
	var lines []core.Line
	for i, s := range res.Data.Stream.BaseSteamInfoList {
		if s.HlsURL != "" && s.StreamName != "" {
			anti := signHuyaAntiCode(s.HlsAntiCode, s.StreamName)
			hls := fmt.Sprintf("%s/%s.%s?%s", strings.TrimRight(s.HlsURL, "/"), s.StreamName, s.HlsSuffix, anti)
			lines = append(lines, core.Line{
				ID:      fmt.Sprintf("hls-%s-%d", s.CDNType, i),
				Label:   fmt.Sprintf("%s HLS", s.CDNType),
				URL:     hls,
				Kind:    core.StreamHLS,
				Headers: headers,
			})
		}
	}
	for i, s := range res.Data.Stream.BaseSteamInfoList {
		if s.FlvURL != "" && s.StreamName != "" {
			anti := signHuyaAntiCode(s.FlvAntiCode, s.StreamName)
			flv := fmt.Sprintf("%s/%s.%s?%s", strings.TrimRight(s.FlvURL, "/"), s.StreamName, s.FlvSuffix, anti)
			lines = append(lines, core.Line{
				ID:      fmt.Sprintf("flv-%s-%d", s.CDNType, i),
				Label:   fmt.Sprintf("%s FLV", s.CDNType),
				URL:     flv,
				Kind:    core.StreamFLV,
				Headers: headers,
			})
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("huya: 未获取到可用直播流")
	}
	title := firstNonEmpty(res.Data.LiveData.Introduction, res.Data.LiveData.Nick, "虎牙直播 "+roomID)
	return &core.PlaySource{
		Ref:     core.ItemRef{Source: SourcePrefix + "huya", Kind: core.KindLiveRoom, ID: roomID},
		Title:   title,
		URL:     lines[0].URL,
		Kind:    lines[0].Kind,
		Headers: headers,
		Lines:   lines,
	}, nil
}

func (h *huyaAdapter) StreamDanmaku(ctx context.Context, roomID string, out chan<- ChatMessage) error {
	for ctx.Err() == nil {
		if err := h.streamWS(ctx, roomID, out); err == nil || ctx.Err() != nil {
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

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// signHuyaAntiCode re-signs Huya's fm/wsSecret anti-code query so HLS segments
// and FLV streams do not expire or return HTTP 403 after a few seconds.
func signHuyaAntiCode(rawAnti, streamName string) string {
	q, err := url.ParseQuery(rawAnti)
	if err != nil {
		return rawAnti
	}
	fmB64 := q.Get("fm")
	if fmB64 == "" {
		return rawAnti
	}
	fmRaw, err := base64.StdEncoding.DecodeString(fmB64)
	if err != nil {
		return rawAnti
	}
	const uid = int64(1463963878912)
	now := time.Now()
	nowMs := now.UnixMilli()
	seqid := strconv.FormatInt(nowMs+uid, 10)
	wsTime := strconv.FormatInt(now.Unix()+21600, 16)
	ctype := q.Get("ctype")
	if ctype == "" {
		ctype = "tars_mp"
	}
	tVal := q.Get("t")
	if tVal == "" {
		tVal = "102"
	}
	ss := md5Hex(seqid + "|" + ctype + "|" + tVal)
	plain := strings.NewReplacer(
		"$0", strconv.FormatInt(uid, 10),
		"$1", streamName,
		"$2", ss,
		"$3", wsTime,
	).Replace(string(fmRaw))
	wsSecret := md5Hex(plain)

	out := url.Values{}
	out.Set("wsSecret", wsSecret)
	out.Set("wsTime", wsTime)
	out.Set("seqid", seqid)
	out.Set("ctype", ctype)
	out.Set("ver", "1")
	if fs := q.Get("fs"); fs != "" {
		out.Set("fs", fs)
	}
	out.Set("u", strconv.FormatInt(uid, 10))
	out.Set("t", tVal)
	out.Set("sv", "2401090219")
	out.Set("sdk_sid", strconv.FormatInt(nowMs, 10))
	out.Set("codec", "264")
	return out.Encode()
}
