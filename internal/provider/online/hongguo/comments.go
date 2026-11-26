package hongguo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/legacymodel"
)

// Public reviews come from the public h5 SSR pages. No request signing is
// involved, unlike the timed danmaku which needs the signed App API.
//
// Two different pages carry two different review sets:
//
//	/player/{series}/{vid}  -> episodeSocialInfo.reviews  (本集评论)
//	/detail?series_id={sid} -> seriesSocialInfo.reviews   (剧集评论/评分)
//
// The h5 SSR mixes numbers and strings for the same kind of field, so every
// numeric field is read through flexInt.

// flexInt reads a number that the SSR may emit as a JSON string or a number.
func flexInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

// flexFloat reads a decimal that the SSR may emit as a JSON string or a number.
func flexFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

// publicReview is one review entry as the SSR delivers it.
type publicReview struct {
	CreateTime  any            `json:"create_time"`
	DiggCount   any            `json:"digg_count"`
	Rating      any            `json:"rating"`
	UserName    string         `json:"user_name"`
	UserAvatar  string         `json:"user_avatar"`
	Content     string         `json:"content"`
	Replies     []publicReview `json:"replies"`
	ReplyList   []publicReview `json:"reply_list"`
	SubComments []publicReview `json:"sub_comments"`
}

// socialReviews is the envelope the SSR wraps review lists in.
type socialReviews struct {
	TotalCount any            `json:"total_count"`
	Reviews    []publicReview `json:"reviews"`
}

// socialInfo carries the review envelope plus the aggregate rating.
type socialInfo struct {
	Rating      any            `json:"rating"`
	RatingCount any            `json:"rating_count"`
	Reviews     *socialReviews `json:"reviews"`
}

// loaderNode is the subset of an SSR route payload this file cares about.
type loaderNode struct {
	IsSuccess         *bool       `json:"isSuccess"`
	SeriesID          string      `json:"series_id"`
	Vid               string      `json:"vid"`
	SeriesSocialInfo  *socialInfo `json:"seriesSocialInfo"`
	EpisodeSocialInfo *socialInfo `json:"episodeSocialInfo"`
}

// findLoaderNode digs the route payload out of an h5 SSR page.
//
// The page embeds the router state as JSON. Depending on the route the
// payload may sit one level down ("loaderData" -> route key) or directly at
// the top, so both shapes are accepted.
func findLoaderNode(html string) (*loaderNode, error) {
	blob, ok := extractJSONObject(html, "loaderData")
	if !ok {
		return nil, ErrNotFound
	}
	var outer struct {
		LoaderData map[string]json.RawMessage `json:"loaderData"`
	}
	if err := json.Unmarshal(blob, &outer); err == nil && len(outer.LoaderData) > 0 {
		if node := pickLoaderNode(outer.LoaderData); node != nil {
			return node, nil
		}
	}
	// Fallback: the blob may already be the route map.
	var direct map[string]json.RawMessage
	if err := json.Unmarshal(blob, &direct); err == nil {
		if node := pickLoaderNode(direct); node != nil {
			return node, nil
		}
	}
	return nil, ErrNotFound
}

// pickLoaderNode returns the route payload that carries the social info.
//
// The upstream reference implementation keys off "seriesDetail", but relying on
// a single field makes the whole feature break the moment the SSR reshapes
// that node. Any node carrying social info or a series id is accepted.
func pickLoaderNode(routes map[string]json.RawMessage) *loaderNode {
	for _, raw := range routes {
		var node loaderNode
		if json.Unmarshal(raw, &node) != nil {
			continue
		}
		if node.SeriesSocialInfo == nil && node.EpisodeSocialInfo == nil && node.SeriesID == "" {
			continue
		}
		return &node
	}
	return nil
}

// toCommentItems converts SSR reviews into the unified model.
func toCommentItems(reviews *socialReviews, limit int) []model.CommentItem {
	if reviews == nil || len(reviews.Reviews) == 0 {
		return []model.CommentItem{}
	}
	out := make([]model.CommentItem, 0, len(reviews.Reviews))
	seen := map[string]bool{}
	for _, r := range reviews.Reviews {
		content := strings.TrimSpace(r.Content)
		if content == "" {
			continue
		}
		if len([]rune(content)) > 2000 {
			content = string([]rune(content)[:2000])
		}
		author := strings.TrimSpace(r.UserName)
		if author == "" {
			author = "匿名用户"
		}
		avatar := strings.TrimSpace(r.UserAvatar)
		if !strings.HasPrefix(avatar, "https://") {
			avatar = ""
		}
		created := flexInt(r.CreateTime)
		id := reviewKey(author, created, content)
		if seen[id] {
			continue
		}
		seen[id] = true

		var replies []model.CommentItem
		candidates := r.Replies
		if len(candidates) == 0 {
			candidates = r.ReplyList
		}
		if len(candidates) == 0 {
			candidates = r.SubComments
		}
		for _, sub := range candidates {
			subContent := strings.TrimSpace(sub.Content)
			if subContent == "" {
				continue
			}
			subAuthor := strings.TrimSpace(sub.UserName)
			if subAuthor == "" {
				subAuthor = "匿名用户"
			}
			subAvatar := strings.TrimSpace(sub.UserAvatar)
			if !strings.HasPrefix(subAvatar, "https://") && !strings.HasPrefix(subAvatar, "http://") {
				subAvatar = ""
			}
			subCreated := flexInt(sub.CreateTime)
			replies = append(replies, model.CommentItem{
				ID:       reviewKey(subAuthor, subCreated, subContent),
				Author:   subAuthor,
				Avatar:   subAvatar,
				Content:  subContent,
				Likes:    flexInt(sub.DiggCount),
				TimeText: formatCommentTime(subCreated),
			})
		}

		out = append(out, model.CommentItem{
			ID:       id,
			Author:   author,
			Avatar:   avatar,
			Content:  content,
			Likes:    flexInt(r.DiggCount),
			TimeText: formatCommentTime(created),
			Replies:  replies,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// reviewKey builds a stable identifier for a review.
func reviewKey(author string, created int, content string) string {
	return fmt.Sprintf("%s|%d|%s", author, created, content)
}

// formatCommentTime renders a unix timestamp as a short relative label.
func formatCommentTime(unix int) string {
	if unix <= 0 {
		return ""
	}
	stamp := timeUnix(unix)
	if stamp.IsZero() {
		return ""
	}
	return stamp.Format("2006-01-02")
}

// timeUnix converts unix seconds to a local time, tolerating bad input.
func timeUnix(unix int) time.Time {
	if unix <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(unix), 0)
}

// SeriesComments returns the drama-level public reviews plus its rating.
//
// The detail page is the only place that carries seriesSocialInfo, so it is
// fetched separately from the episode player page.
func (c *Client) SeriesComments(seriesID string, limit int) ([]model.CommentItem, float64, int, error) {
	if seriesID == "" {
		return nil, 0, 0, fmt.Errorf("hongguo: missing series id")
	}
	html, err := c.get("/detail", url.Values{"series_id": {seriesID}}, BaseURL+"/")
	if err != nil {
		return nil, 0, 0, err
	}
	node, err := findLoaderNode(string(html))
	if err != nil {
		return nil, 0, 0, err
	}
	if node.SeriesID != "" && node.SeriesID != seriesID {
		return nil, 0, 0, fmt.Errorf("hongguo: 评论剧集不匹配")
	}
	var rating float64
	var ratingCount int
	var reviews *socialReviews
	if node.SeriesSocialInfo != nil {
		rating = flexFloat(node.SeriesSocialInfo.Rating)
		ratingCount = flexInt(node.SeriesSocialInfo.RatingCount)
		reviews = node.SeriesSocialInfo.Reviews
	}
	return toCommentItems(reviews, limit), rating, ratingCount, nil
}

// EpisodeComments returns the public reviews attached to one episode.
func (c *Client) EpisodeComments(seriesID, vid string, limit int) ([]model.CommentItem, error) {
	if seriesID == "" || vid == "" {
		return nil, fmt.Errorf("hongguo: missing series or episode id")
	}
	html, err := c.get("/player/"+url.PathEscape(seriesID)+"/"+url.PathEscape(vid), nil, BaseURL+"/")
	if err != nil {
		return nil, err
	}
	node, err := findLoaderNode(string(html))
	if err != nil {
		return nil, err
	}
	if node.Vid != "" && node.Vid != vid {
		return nil, fmt.Errorf("hongguo: 评论分集不匹配")
	}
	if node.EpisodeSocialInfo == nil {
		return []model.CommentItem{}, nil
	}
	return toCommentItems(node.EpisodeSocialInfo.Reviews, limit), nil
}
