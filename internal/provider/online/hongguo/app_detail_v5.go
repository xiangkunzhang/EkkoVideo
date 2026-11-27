package hongguo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// flexID decodes either a JSON string or a JSON number without float64 precision loss.
type flexID string

func (f *flexID) UnmarshalJSON(data []byte) error {
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
		*f = flexID(strings.TrimSpace(str))
		return nil
	}
	*f = flexID(s)
	return nil
}

func marshalV5DetailBody(seriesID string) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{
		"series_id": seriesID,
		"biz_param": map[string]any{
			"caller_scene":              "single_col",
			"detail_page_version":       0,
			"from_video_id":             "",
			"need_all_video_definition": false,
			"need_mp4_align":            false,
			"source":                    4,
			"video_id_type":             1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("hongguo: v5 detail payload: %w", err)
	}
	body, err := gzipBytes(payload)
	if err != nil {
		return nil, fmt.Errorf("hongguo: v5 detail gzip: %w", err)
	}
	return body, nil
}

// fetchAppDetail fetches the complete episode list (all numeric vids) via
// the signed App /novel/player/video_detail/v1/ endpoint.
func (c *Client) fetchAppDetail(seriesID string) (*Detail, error) {
	if !isDigits(seriesID) {
		return nil, fmt.Errorf("hongguo: invalid series id %q", seriesID)
	}
	dev, signer, err := defaultAppSession.get(c.HTTP, false)
	if err != nil {
		return nil, err
	}
	body, err := marshalV5DetailBody(seriesID)
	if err != nil {
		return nil, err
	}
	query := v5PlaybackQuery{
		DeviceID:    dev.DeviceID,
		InstallID:   dev.InstallID,
		CDID:        dev.CDID,
		VersionName: v5PreloadVersionName,
		Channel:     v5PreloadChannel,
		DeviceType:  v5PreloadDeviceType,
		OSVersion:   v5PreloadOSVersion,
	}

	hosts := appPlayHosts
	if c.Routes != nil {
		if planned := c.Routes.Plan(false); len(planned) > 0 {
			hosts = planned
		}
	}

	anonClient := &http.Client{Timeout: c.HTTP.Timeout, Transport: c.HTTP.Transport}
	var lastErr error
	for _, host := range hosts {
		baseURL := "https://" + host + "/novel/player/video_detail/v1/"
		fullURL, headers, err := signer.SignPlaybackV5(baseURL, query, body, currentMillis())
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("User-Agent", "okhttp/4.12.0")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		resp, err := anonClient.Do(req)
		if err != nil {
			if c.Routes != nil {
				c.Routes.OnFailure(host, true, false)
			}
			lastErr = err
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if c.Routes != nil {
				c.Routes.OnFailure(host, true, false)
			}
			lastErr = err
			continue
		}
		if len(raw) == 0 {
			if c.Routes != nil {
				c.Routes.OnFailure(host, false, true)
			}
			lastErr = fmt.Errorf("hongguo: v5 detail empty response from %s", host)
			continue
		}
		if resp.StatusCode >= 400 {
			if c.Routes != nil {
				c.Routes.OnFailure(host, resp.StatusCode < 500, false)
			}
			lastErr = fmt.Errorf("hongguo: v5 detail http %d from %s", resp.StatusCode, host)
			continue
		}

		d, err := parseAppVideoDetail(raw, seriesID)
		if err != nil {
			if c.Routes != nil {
				c.Routes.OnSuccess(host, true)
			}
			lastErr = err
			continue
		}
		if c.Routes != nil {
			c.Routes.OnSuccess(host, false)
		}
		return d, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("hongguo: v5 detail failed")
	}
	return nil, lastErr
}

func parseAppVideoDetail(raw []byte, seriesID string) (*Detail, error) {
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			VideoData struct {
				SeriesID    flexID `json:"series_id"`
				SeriesTitle string `json:"series_title"`
				SeriesCover string `json:"series_cover"`
				CoverURL    string `json:"cover_url"`
				SeriesIntro string `json:"series_intro"`
				EpisodeCnt  int    `json:"episode_cnt"`
				VideoList   []struct {
					VID      flexID `json:"vid"`
					VIDIndex int    `json:"vid_index"`
					SeriesID flexID `json:"series_id"`
					Title    string `json:"title"`
				} `json:"video_list"`
			} `json:"video_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("hongguo: decode v5 detail: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("hongguo: v5 detail code=%d %s", envelope.Code, envelope.Message)
	}
	vd := envelope.Data.VideoData
	if string(vd.SeriesID) != seriesID {
		return nil, fmt.Errorf("hongguo: v5 detail series mismatch: got %q want %q", vd.SeriesID, seriesID)
	}
	if len(vd.VideoList) == 0 {
		return nil, fmt.Errorf("hongguo: v5 detail empty video_list")
	}

	sort.SliceStable(vd.VideoList, func(i, j int) bool {
		return vd.VideoList[i].VIDIndex < vd.VideoList[j].VIDIndex
	})

	seen := make(map[string]bool, len(vd.VideoList))
	vids := make([]string, 0, len(vd.VideoList))
	for _, row := range vd.VideoList {
		vid := string(row.VID)
		if !isDigits(vid) || seen[vid] {
			continue
		}
		seen[vid] = true
		vids = append(vids, vid)
	}
	if len(vids) == 0 {
		return nil, fmt.Errorf("hongguo: v5 detail no valid numeric vids")
	}

	cover := strings.TrimSpace(vd.SeriesCover)
	if cover == "" {
		cover = strings.TrimSpace(vd.CoverURL)
	}
	epCnt := len(vids)
	if vd.EpisodeCnt > epCnt {
		epCnt = vd.EpisodeCnt
	}

	d := &Detail{
		SeriesID:             seriesID,
		SeriesName:           vd.SeriesTitle,
		SeriesCover:          cover,
		SeriesIntro:          vd.SeriesIntro,
		EpisodeCnt:           epCnt,
		AccessibleEpisodeCnt: len(vids),
		VidList:              vids,
	}
	d.SeriesEpisodeInfo.EpisodeCnt = len(vids)
	d.SeriesEpisodeInfo.EpisodeTotalCnt = epCnt
	return d, nil
}
