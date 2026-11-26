package hongguo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/legacymodel"
	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

// The timed danmaku lives on the signed App API, not on the public h5 pages.
//
// Endpoint:
//
//	POST https://api5-normal-sinfonlineb.fqnovel.com/novel/commentapi/comment/list/{vid}/v1/
//
// The API serves the danmaku in fixed 30 second windows. Every response
// carries the cursor for the next window plus a flag telling whether more
// windows exist, so a full episode is assembled by walking the cursor.
//
// A typical short-drama episode is around 70 seconds, which is three windows.

const (
	// danmakuCommentSource identifies the danmaku comment stream.
	danmakuCommentSource = 601
	// danmakuServerChannel is the server channel the danmaku stream lives on.
	danmakuServerChannel = 1000
	// danmakuMaxWindows caps how many windows a single request walks.
	// A long episode would otherwise trigger dozens of upstream calls.
	danmakuMaxWindows = 12
	// danmakuWindowSpanMS is the window size the API serves.
	danmakuWindowSpanMS = 30000
)

// danmakuResponse is the envelope the comment API answers with.
type danmakuResponse struct {
	Code int `json:"code"`
	Data struct {
		DataList []struct {
			Comment struct {
				CommentID string `json:"comment_id"`
				Common    struct {
					GroupID string `json:"group_id"`
					Status  int    `json:"status"`
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"common"`
				Expand struct {
					OffsetTime int64 `json:"offset_time"`
				} `json:"expand"`
			} `json:"comment"`
		} `json:"data_list"`
		Extra struct {
			NextQueryDanmakuListTime int64 `json:"next_query_danmaku_list_time"`
		} `json:"extra"`
		CommonListInfo struct {
			Cursor  string `json:"cursor"`
			HasMore bool   `json:"has_more"`
			Total   int    `json:"total"`
		} `json:"common_list_info"`
	} `json:"data"`
}

// danmakuPayload builds the request body for one window.
func danmakuPayload(seriesID, vid, cursor string) map[string]any {
	return map[string]any{
		"comment_source":    danmakuCommentSource,
		"server_channel":    danmakuServerChannel,
		"group_id":          vid,
		"group_type":        30,
		"comment_type":      20,
		"sort":              1,
		"count":             90,
		"cursor":            cursor,
		"aid":               8662,
		"compliance_status": 0,
		"business_param": map[string]any{
			"book_id":                 seriesID,
			"start_offset_time":       0,
			"playlet_item_duration":   danmakuWindowSpanMS * danmakuMaxWindows,
			"need_danmaku_guide_type": []int{1, 3, 4, 2},
		},
	}
}

// Danmaku returns the timed comments for one episode.
//
// The upstream window is 30 seconds long, so the whole episode is assembled by
// following the cursor until the API reports no further windows.
func (c *Client) Danmaku(seriesID, vid string) ([]model.DanmakuItem, error) {
	if seriesID == "" || vid == "" {
		return nil, fmt.Errorf("hongguo: missing series or episode id")
	}
	if c.HTTP == nil {
		return nil, fmt.Errorf("hongguo: nil http client")
	}
	dev, signer, err := defaultAppSession.get(c.HTTP, false)
	if err != nil {
		return nil, err
	}
	items, err := c.danmakuWithIdentity(dev, signer, seriesID, vid)
	if err != nil && upstream.IsIdentityRejected(err) {
		// 设备身份被拒：换一个新身份重试一次（弹幕与播放共用同一份身份）。
		dev2, signer2, rerr := defaultAppSession.refreshRejected(c.HTTP, dev)
		if rerr != nil {
			return nil, rerr
		}
		return c.danmakuWithIdentity(dev2, signer2, seriesID, vid)
	}
	return items, err
}

// danmakuWithIdentity 用指定身份拉取整集弹幕。
func (c *Client) danmakuWithIdentity(dev *RegisteredDevice, signer *AppSigner, seriesID, vid string) ([]model.DanmakuItem, error) {
	baseURL := commentListURL(vid)
	cursor := ""
	seen := map[string]bool{}
	items := make([]model.DanmakuItem, 0, 128)

	for window := 0; window < danmakuMaxWindows; window++ {
		body, err := json.Marshal(danmakuPayload(seriesID, vid, cursor))
		if err != nil {
			return nil, err
		}
		fullURL, headers, err := signer.SignPlayback(baseURL, dev, body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Comment-Source", fmt.Sprintf("%d", danmakuCommentSource))
		req.Header.Set("Server-Channel", fmt.Sprintf("%d", danmakuServerChannel))
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("hongguo: 弹幕接口 HTTP %d: %s", resp.StatusCode, snippet(raw))
		}
		var parsed danmakuResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("hongguo: 弹幕响应解析失败: %w", err)
		}
		if parsed.Code != 0 {
			if rej := identityRejection(parsed.Code, "弹幕接口"); rej != nil {
				return nil, rej
			}
			return nil, fmt.Errorf("hongguo: 弹幕接口返回 code=%d", parsed.Code)
		}
		for _, row := range parsed.Data.DataList {
			cm := row.Comment
			if cm.Common.GroupID != "" && cm.Common.GroupID != vid {
				continue
			}
			if cm.Common.Status != 1 {
				continue
			}
			text := strings.TrimSpace(cm.Common.Content.Text)
			if text == "" {
				continue
			}
			id := cm.CommentID
			if id == "" {
				id = fmt.Sprintf("%d-%s", cm.Expand.OffsetTime, text)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, model.DanmakuItem{
				TimeSec: float64(cm.Expand.OffsetTime) / 1000,
				Text:    text,
			})
		}
		if !parsed.Data.CommonListInfo.HasMore {
			break
		}
		cursor = parsed.Data.CommonListInfo.Cursor
		if cursor == "" {
			break
		}
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].TimeSec < items[j].TimeSec })
	return items, nil
}

// commentListURL builds the comment list endpoint for one episode.
func commentListURL(vid string) string {
	return "https://api5-normal-sinfonlineb.fqnovel.com/novel/commentapi/comment/list/" + vid + "/v1/"
}
