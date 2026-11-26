package hongguo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// SearchResultPage holds one page of search results from GET /api/search/result.
type SearchResultPage struct {
	Items      []SearchItem
	Page       int
	TotalPages int
	Total      int
	HasMore    bool
}

// Search queries GET /api/search/result (page 1).
func (c *Client) Search(query string) ([]SearchItem, error) {
	res, err := c.SearchPage(query, 1)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

// SearchPage queries GET /api/search/result with pagination support.
//
// The parameter is "query" (not "keyword"); "keyword" is rejected with
// invalid_query. The endpoint is public and needs no signature.
func (c *Client) SearchPage(query string, page int) (*SearchResultPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("hongguo: empty query")
	}
	if page < 1 {
		page = 1
	}

	params := url.Values{"query": {query}}
	if page > 1 {
		params.Set("page", strconv.Itoa(page))
	}
	body, err := c.get("/api/search/result", params, c.BaseURL+"/")
	if err != nil {
		return nil, err
	}

	var payload struct {
		SearchList []SearchItem `json:"searchList"`
		HasMore    *bool        `json:"hasMore"`
		TotalPages int          `json:"totalPages"`
		Total      int          `json:"total"`
		PageNum    int          `json:"pageNum"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("hongguo: decode search result: %w", err)
	}

	hasMore := false
	if len(payload.SearchList) > 0 && page < 100 {
		switch {
		case payload.HasMore != nil:
			hasMore = *payload.HasMore
		case payload.TotalPages > 0:
			hasMore = page < payload.TotalPages
		default:
			hasMore = true
		}
	}
	totalPages := payload.TotalPages
	if totalPages < page {
		if hasMore {
			totalPages = page + 1
		} else {
			totalPages = page
		}
	}

	return &SearchResultPage{
		Items:      payload.SearchList,
		Page:       page,
		TotalPages: totalPages,
		Total:      payload.Total,
		HasMore:    hasMore,
	}, nil
}

// Detail fetches the series metadata, including the complete episode list.
//
// The data lives in the SSR payload of /player/{series_id}. The page also
// exposes a JSON-LD block, but that one only lists the free episodes, while
// the SSR payload carries the full vid_list plus accessible_episode_cnt.
//
// If /player/{series_id} alone does not carry the payload (the page
// sometimes routes straight into the first episode), the first episode id is
// used to build the /player/{series_id}/{vid} URL and the request is retried.
func (c *Client) Detail(seriesID string) (*Detail, error) {
	seriesID = strings.TrimSpace(seriesID)
	if seriesID == "" {
		return nil, fmt.Errorf("hongguo: empty series id")
	}

	detail, err := c.detailFrom(c.BaseURL + "/player/" + seriesID)
	if err == nil {
		return detail, nil
	}
	// detailFrom 失败时 detail 是 nil：不能对它调方法，否则整个请求 panic。
	if detail == nil {
		return nil, err
	}

	// Fall back to a per-episode player URL, which always carries the payload.
	for _, vid := range detail.fallbackVids(seriesID) {
		d2, err2 := c.detailFrom(c.BaseURL + "/player/" + seriesID + "/" + vid)
		if err2 == nil {
			return d2, nil
		}
		err = err2
	}
	return nil, err
}

func (c *Client) detailFrom(pageURL string) (*Detail, error) {
	body, err := c.getURL(pageURL, c.BaseURL+"/")
	if err != nil {
		return nil, err
	}
	html := string(body)

	var d Detail
	if err := decodeEmbedded(html, "vid_list", &d); err != nil {
		return nil, fmt.Errorf("hongguo: parse detail: %w", err)
	}
	if d.SeriesID == "" {
		return nil, fmt.Errorf("hongguo: parse detail: %w", ErrNotFound)
	}
	return &d, nil
}

// fallbackVids derives candidate episode ids from the page itself.
func (d *Detail) fallbackVids(seriesID string) []string {
	return d.VidList
}

// getURL is a GET against an absolute URL.
func (c *Client) getURL(full, referer string) ([]byte, error) {
	req, err := newGet(full)
	if err != nil {
		return nil, err
	}
	c.decorate(req, referer)
	return c.do(req)
}

// PlayInfo resolves a playable URL for one episode.
//
// definition is a quality label understood by the BFF ("360p", "480p",
// "720p", "1080p"). The response carries a signed CDN URL that can be
// fetched without any Referer header.
func (c *Client) PlayInfo(seriesID, itemID, definition string) (*PlayInfo, error) {
	if definition == "" {
		definition = "720p"
	}
	body := map[string]any{
		"series_id":  seriesID,
		"item_id":    itemID,
		"definition": definition,
	}

	referer := fmt.Sprintf("%s/player/%s/%s", c.BaseURL, seriesID, itemID)
	raw, _ := c.postJSON("/api/video/protected/play-info", body, referer)
	if len(raw) == 0 {
		return nil, fmt.Errorf("hongguo: play-info: empty response")
	}

	var pi PlayInfo
	if err := json.Unmarshal(raw, &pi); err != nil {
		return nil, fmt.Errorf("hongguo: decode play-info: %w", err)
	}
	if pi.Error != "" {
		return &pi, fmt.Errorf("hongguo: play-info: %s", pi.Error)
	}
	return &pi, nil
}

// FallbackPlayInfo asks for the free preview stream of an episode.
//
// It answers even for locked episodes, but only yields the ~10 second
// trailer served from /video/public/{vid}/360p.mp4.
func (c *Client) FallbackPlayInfo(itemID string) (*FallbackPlayInfo, error) {
	body := map[string]any{"item_id": itemID}
	raw, _ := c.postJSON("/api/video/protected/fallback-play-info", body, c.BaseURL+"/")
	if len(raw) == 0 {
		return nil, fmt.Errorf("hongguo: fallback-play-info: empty response")
	}

	var fb FallbackPlayInfo
	if err := json.Unmarshal(raw, &fb); err != nil {
		return nil, fmt.Errorf("hongguo: decode fallback-play-info: %w", err)
	}
	if fb.Error != "" {
		return &fb, fmt.Errorf("hongguo: fallback-play-info: %s", fb.Error)
	}
	return &fb, nil
}

// TrailerURL is the static preview URL of an episode.
func (c *Client) TrailerURL(itemID string) string {
	return fmt.Sprintf("%s/video/public/%s/360p.mp4", c.BaseURL, itemID)
}

// Stream is the resolved playback source of one episode.
type Stream struct {
	// Direct is a progressive MP4 URL that any player can open as is.
	Direct string
	// Dash is set when the BFF answered with a fragmented/CENC stream.
	Dash *DashInfo
	// RawKey is populated when resolved via the App API (spade_a derived key).
	RawKey []byte
	// Full reports whether this is the complete episode. When false, the
	// source is the ~10 second preview trailer.
	Full bool
	// Locked is set when the episode is behind the paywall.
	Locked bool
	// Duration in seconds.
	Duration float64
}

// Resolve resolves the playback source of an episode.
//
// It returns the progressive MP4 when available, the DASH description when
// the episode is served as encrypted fragmented MP4, and falls back to the
// preview trailer for locked episodes.
func (c *Client) Resolve(seriesID, itemID, definition string) (*Stream, error) {
	return c.resolve(seriesID, itemID, definition, false)
}

// ResolveSpeculative is the prefetch-side variant: it may reuse an existing
// device identity but never registers a new one.
func (c *Client) ResolveSpeculative(seriesID, itemID, definition string) (*Stream, error) {
	return c.resolve(seriesID, itemID, definition, true)
}

// resolve resolves the playback source of an episode.
//
// It returns the progressive MP4 when available, the DASH description when
// the episode is served as encrypted fragmented MP4, and falls back to the
// preview trailer for locked episodes.
//
// speculative=true marks a background request (prefetch): it must not
// register a new device identity, because a speculative registration would
// race with the foreground playback that is about to start.
func (c *Client) resolve(seriesID, itemID, definition string, speculative bool) (*Stream, error) {
	// Prefer signed fqnovel App API for all episodes: it returns a single cleanly
	// muxed video+audio MP4 with moov-level CENC sample tables and 16-byte RawKey
	// for Tier-2 Range streaming. (BFF PlayInfo DASH streams put CENC tables in
	// moof/senc which our moov-level decryptor does not parse.)
	var appErr error
	if appSt, e := c.appResolve(itemID, definition, speculative); e == nil && appSt != nil {
		if len(appSt.Key) == 0 {
			return &Stream{Direct: appSt.URL, Full: true}, nil
		}
		return &Stream{
			Dash: &DashInfo{
				VID:        itemID,
				MainURL:    appSt.URL,
				BackupURL:  appSt.BackupURL,
				Definition: appSt.Definition,
				CodecType:  appSt.Codec,
			},
			RawKey: appSt.Key,
			Full:   true,
		}, nil
	} else {
		appErr = e
	}

	pi, err := c.PlayInfo(seriesID, itemID, definition)
	if err == nil && pi != nil {
		if pi.LegacyPlayInfo.MainURL != "" {
			return &Stream{
				Direct:   pi.LegacyPlayInfo.MainURL,
				Full:     true,
				Duration: pi.LegacyPlayInfo.Duration,
			}, nil
		}
		if pi.PlayInfo != nil && pi.PlayInfo.MainURL != "" {
			stream := &Stream{
				Dash:     pi.PlayInfo,
				Full:     true,
				Duration: pi.PlayInfo.Duration,
			}
			// BFF 流的密钥是 player_access_key。导出成 RawKey 之后 Range 流式
			// 路径才能用上 —— 否则只能整集下载完再播，首帧极慢。
			if key, kerr := DecodeAccessKey(pi.PlayInfo.PlayerAccessKey); kerr == nil && len(key) == 16 {
				stream.RawKey = key
			}
			return stream, nil
		}
	}

	// Locked episode: the BFF refuses the full stream and the preview
	// fallback is closed as well.
	fb, ferr := c.FallbackPlayInfo(itemID)
	if ferr == nil && fb.MainURL != "" {
		return &Stream{Direct: fb.MainURL, Full: false}, nil
	}
	if appErr != nil {
		return nil, fmt.Errorf("hongguo app resolve: %w", appErr)
	}
	if err != nil {
		return nil, err
	}
	return nil, ferr
}

// SeriesIDFromLink extracts the series id from a hongguoduanju.com link.
func SeriesIDFromLink(link string) string {
	if u, err := url.Parse(link); err == nil {
		if v := u.Query().Get("series_id"); v != "" {
			return v
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "player" {
			return parts[1]
		}
	}
	// bare id
	if isDigits(link) {
		return link
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// FormatDuration renders seconds as H:MM:SS.
func FormatDuration(sec float64) string {
	total := int(sec)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s) + "s"
}

// FormatCount renders a big number in the Chinese short-video style.
func FormatCount(n int64) string {
	switch {
	case n >= 100000000:
		return strconv.FormatFloat(float64(n)/100000000, 'f', 1, 64) + "亿"
	case n >= 10000:
		return strconv.FormatFloat(float64(n)/10000, 'f', 1, 64) + "万"
	default:
		return strconv.FormatInt(n, 10)
	}
}
