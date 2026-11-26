package hongguo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/legacymodel"
	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

const relatedPlanPath = "/reading/bookapi/plan/v:version/"

var (
	relatedHosts = []string{
		"api5-normal-sinfonlinec.fqnovel.com",
		"api5-normal-hl.fqnovel.com",
		"api5-normal-lf.fqnovel.com",
	}
	relatedIDPattern         = regexp.MustCompile(`^[0-9]{1,30}$`)
	relatedSeasonSuffix      = regexp.MustCompile(`第?([0-9零〇一二两三四五六七八九十百]+)季$`)
	relatedSeasonWithEdition = regexp.MustCompile(`第([0-9零〇一二两三四五六七八九十百]+)季\s*(?:完结篇|完結篇)$`)
)

// Related fetches related dramas (other seasons in the same series and
// similar-genre recommendations) from Hongguo's reading plan endpoint.
func (c *Client) Related(seriesID, currentTitle string) ([]model.RelatedWork, error) {
	seriesID = strings.TrimSpace(seriesID)
	if !relatedIDPattern.MatchString(seriesID) {
		return nil, fmt.Errorf("hongguo: invalid series_id %q", seriesID)
	}
	dev, signer, err := defaultAppSession.get(c.HTTP, false)
	if err != nil {
		return nil, err
	}
	items, err := c.fetchRelatedWithIdentity(dev, signer, seriesID, currentTitle)
	if err != nil && upstream.IsIdentityRejected(err) {
		dev2, signer2, rerr := defaultAppSession.refreshRejected(c.HTTP, dev)
		if rerr != nil {
			return nil, rerr
		}
		return c.fetchRelatedWithIdentity(dev2, signer2, seriesID, currentTitle)
	}
	return items, err
}

func (c *Client) fetchRelatedWithIdentity(dev *RegisteredDevice, signer *AppSigner, seriesID, currentTitle string) ([]model.RelatedWork, error) {
	extra := [][2]string{
		{"scene", "10"},
		{"from", "detail_page_more_related"},
		{"book_id", seriesID},
		{"new_search_middle_page", "false"},
		{"bookstore_tab_type", "0"},
		{"query_history_removed", "false"},
		{"user_is_login", "0"},
		{"bookstore_tab", "0"},
		{"search_middle_page_version", "0"},
		{"pad_column_detail", "0"},
		{"hot_word_exchange", "false"},
		{"device_level", "3"},
		{"limit", "0"},
		{"offset", "0"},
		{"is_horizontal_screen", "false"},
		{"reader_single_col_from_ip_strategy", "false"},
	}

	var lastErr error
	for _, host := range relatedHosts {
		baseURL := "https://" + host + relatedPlanPath
		fullURL, headers, err := signer.SignRequestWithParams(baseURL, dev, extra, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodGet, fullURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("hongguo: related HTTP %d: %s", resp.StatusCode, snippet(raw))
			continue
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			lastErr = fmt.Errorf("hongguo: related empty response from %s", host)
			continue
		}
		items, perr := parseRelatedWorks(raw, seriesID, currentTitle)
		if perr != nil {
			if upstream.IsIdentityRejected(perr) {
				return nil, perr
			}
			lastErr = perr
			continue
		}
		return items, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("hongguo: related no hosts available")
	}
	return nil, lastErr
}

type relatedPlanResponse struct {
	Code int               `json:"code"`
	Data []relatedPlanCell `json:"data"`
}

type relatedPlanCell struct {
	ShowType  int               `json:"show_type"`
	CellName  string            `json:"cell_name"`
	VideoData []relatedVideoRow `json:"video_data"`
}

type relatedVideoRow struct {
	SeriesID    string `json:"series_id"`
	Title       string `json:"title"`
	ContentType int    `json:"content_type"`
	DisablePlay bool   `json:"disable_play"`
	EpisodeCnt  int    `json:"episode_cnt"`
	Cover       string `json:"cover"`
	VideoDesc   string `json:"video_desc"`
	SeasonIndex int    `json:"season_index"`
	TagInfo     *struct {
		Text string `json:"text"`
	} `json:"tag_info"`
}

func parseRelatedWorks(raw []byte, currentID, currentTitle string) ([]model.RelatedWork, error) {
	var parsed relatedPlanResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("hongguo: related parse failed: %w", err)
	}
	if parsed.Code != 0 {
		if rej := identityRejection(parsed.Code, "相关作品接口"); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("hongguo: related code=%d", parsed.Code)
	}

	currentStem := relatedTitleStem(currentTitle)
	seen := make(map[string]struct{})
	out := make([]model.RelatedWork, 0)

	for _, cell := range parsed.Data {
		if cell.ShowType != 547 && cell.CellName != "相关作品" {
			continue
		}
		for _, row := range cell.VideoData {
			id := strings.TrimSpace(row.SeriesID)
			title := strings.TrimSpace(row.Title)
			if id == "" || id == currentID || !relatedIDPattern.MatchString(id) || title == "" {
				continue
			}
			if row.ContentType != 1004 && row.ContentType != 1007 {
				continue
			}
			upcoming := row.ContentType == 1007
			if !upcoming && (row.DisablePlay || row.EpisodeCnt <= 0) {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}

			season := ""
			if row.TagInfo != nil {
				season = strings.TrimSpace(row.TagInfo.Text)
			}
			if season == "" && row.SeasonIndex > 0 {
				season = fmt.Sprintf("第%d季", row.SeasonIndex)
			}
			rightText := fmt.Sprintf("全 %d 集", row.EpisodeCnt)
			if upcoming {
				rightText = "待上线"
			}
			seasonIdx := row.SeasonIndex
			if seasonIdx < 0 {
				seasonIdx = 0
			}
			sameSeries := currentStem != "" && relatedTitleStem(title) == currentStem

			out = append(out, model.RelatedWork{
				DramaCard: model.DramaCard{
					Source:     "hongguo",
					SeriesID:   id,
					Title:      title,
					Cover:      strings.TrimSpace(row.Cover),
					Intro:      strings.TrimSpace(row.VideoDesc),
					EpisodeCnt: row.EpisodeCnt,
					RightText:  rightText,
				},
				Season:      season,
				SeasonIndex: seasonIdx,
				Upcoming:    upcoming,
				SameSeries:  sameSeries,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SameSeries != b.SameSeries {
			return a.SameSeries
		}
		sa := relatedOrderIndex(a)
		sb := relatedOrderIndex(b)
		if sa != sb {
			return sa < sb
		}
		return false
	})

	return out, nil
}

func relatedSeasonMarker(title string) (prefix string, season int, ok bool) {
	trimmed := strings.TrimSpace(title)
	for _, re := range []*regexp.Regexp{relatedSeasonSuffix, relatedSeasonWithEdition} {
		if loc := re.FindStringSubmatchIndex(trimmed); len(loc) >= 4 {
			return trimmed[:loc[0]], parseChineseSeasonNumber(trimmed[loc[2]:loc[3]]), true
		}
	}
	return trimmed, 0, false
}

func relatedOrderIndex(w model.RelatedWork) int {
	if w.SeasonIndex > 0 {
		return w.SeasonIndex
	}
	if _, n, ok := relatedSeasonMarker(w.Title); ok && n > 0 {
		return n
	}
	if w.SameSeries {
		return 1
	}
	return int(^uint(0) >> 1)
}

func relatedTitleStem(title string) string {
	s, _, _ := relatedSeasonMarker(title)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func parseChineseSeasonNumber(value string) int {
	if n, err := strconv.Atoi(value); err == nil {
		if n > 0 {
			return n
		}
		return 0
	}
	const digits = "零一二三四五六七八九"
	total := 0
	digit := 0
	for _, r := range value {
		switch r {
		case '十':
			if digit == 0 {
				digit = 1
			}
			total += digit * 10
			digit = 0
		case '百':
			if digit == 0 {
				digit = 1
			}
			total += digit * 100
			digit = 0
		case '两':
			digit = 2
		case '〇':
			digit = 0
		default:
			idx := strings.IndexRune(digits, r)
			if idx < 0 {
				return 0
			}
			// idx is byte offset in UTF-8 ("零一二..." are 3-byte runes)
			digit = idx / 3
		}
	}
	return total + digit
}
