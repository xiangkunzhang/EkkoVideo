package yeguo

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/legacymodel"
)

func (c *Client) Browse(mode, keyword string, page, limit int) ([]Drama, int, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 24
	}
	params := url.Values{}
	params.Set("page", strconv.Itoa(page))
	params.Set("limit", strconv.Itoa(limit))

	ep := "/api/theater/exploreList"
	switch {
	case strings.TrimSpace(keyword) != "":
		ep = "/api/search/result"
		params.Set("keyword", strings.TrimSpace(keyword))
	case mode == "rank":
		ep = "/api/theater/videoRank"
	case mode == "recommend":
		ep = "/api/page/recommendList"
	}

	data, err := c.post(ep, params)
	if err != nil {
		return nil, 0, err
	}
	var parsed struct {
		Total int `json:"total"`
		List  []struct {
			ID          int64  `json:"id"`
			VideoID     int64  `json:"video_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Intro       string `json:"intro"`
			Cover       string `json:"cover"`
			Episodes    int    `json:"episodes"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, 0, err
	}
	out := make([]Drama, 0, len(parsed.List))
	for _, it := range parsed.List {
		id := it.VideoID
		if id == 0 {
			id = it.ID
		}
		if id == 0 {
			continue
		}
		desc := it.Description
		if desc == "" {
			desc = it.Intro
		}
		out = append(out, Drama{
			ID:            strconv.FormatInt(id, 10),
			Title:         it.Title,
			Description:   desc,
			CoverURL:      it.Cover,
			TotalEpisodes: it.Episodes,
		})
	}
	return out, parsed.Total, nil
}

// DetailExtra fetches actors and tags for a Yeguo drama.
func (c *Client) DetailExtra(id string) (actors []string, tags []string) {
	params := url.Values{}
	params.Set("playlet_id", id)
	params.Set("video_id", id)
	data, err := c.post("/api/playlet/detail", params)
	if err != nil {
		return nil, nil
	}
	var parsed struct {
		Actors any      `json:"actors"`
		Tags   []string `json:"tags"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return nil, nil
	}
	switch v := parsed.Actors.(type) {
	case string:
		for _, part := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == '，' || r == '/' || r == '、'
		}) {
			if s := strings.TrimSpace(part); s != "" {
				actors = append(actors, s)
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				actors = append(actors, strings.TrimSpace(s))
			}
		}
	}
	return actors, parsed.Tags
}

// Comments fetches comments for a Yeguo playlet.
func (c *Client) Comments(id string, page int) ([]model.CommentItem, error) {
	if page < 1 {
		page = 1
	}
	params := url.Values{}
	params.Set("playlet_id", id)
	params.Set("video_id", id)
	params.Set("page", strconv.Itoa(page))
	params.Set("limit", "20")
	data, err := c.post("/api/playlet/commentList", params)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		List []struct {
			ID        any    `json:"id"`
			Content   string `json:"content"`
			Nickname  string `json:"nickname"`
			Avatar    string `json:"avatar"`
			LikeCount int    `json:"like_count"`
			CreatedAt string `json:"created_at"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	out := make([]model.CommentItem, 0, len(parsed.List))
	for _, it := range parsed.List {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		author := strings.TrimSpace(it.Nickname)
		if author == "" {
			author = "匿名用户"
		}
		out = append(out, model.CommentItem{
			ID:       toStringID(it.ID),
			Author:   author,
			Avatar:   it.Avatar,
			Content:  it.Content,
			Likes:    it.LikeCount,
			TimeText: it.CreatedAt,
		})
	}
	return out, nil
}

// Danmaku fetches timed danmaku items for a Yeguo episode.
func (c *Client) Danmaku(dramaID, epID string) ([]model.DanmakuItem, error) {
	params := url.Values{}
	params.Set("playlet_id", dramaID)
	params.Set("video_id", dramaID)
	params.Set("episode_id", epID)
	data, err := c.post("/api/playlet/danmakuList", params)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		List []struct {
			Content string  `json:"content"`
			Text    string  `json:"text"`
			Time    float64 `json:"time"`
			TimeMs  float64 `json:"time_ms"`
			Color   string  `json:"color"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	out := make([]model.DanmakuItem, 0, len(parsed.List))
	for _, it := range parsed.List {
		txt := strings.TrimSpace(it.Content)
		if txt == "" {
			txt = strings.TrimSpace(it.Text)
		}
		if txt == "" {
			continue
		}
		sec := it.Time
		if sec == 0 && it.TimeMs > 0 {
			sec = it.TimeMs / 1000.0
		}
		out = append(out, model.DanmakuItem{
			TimeSec: sec,
			Text:    txt,
			Color:   it.Color,
		})
	}
	return out, nil
}

func toStringID(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	default:
		return ""
	}
}
