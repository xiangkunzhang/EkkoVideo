package hongguo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xiangkunzhang/Velan/internal/provider/online/legacymodel"
)

// RankBoard fetches a public Hongguo rank page (hot-drama, hot-real-drama, hot-comic-drama, hot-ai-drama).
func (c *Client) RankBoard(board string, page int) (*model.BrowseResult, error) {
	if board == "" {
		board = "hot-drama"
	}
	if page < 1 {
		page = 1
	}
	rawURL := fmt.Sprintf("/rank/%s?page=%d", url.PathEscape(board), page)
	html, err := c.get(rawURL, nil, BaseURL+"/")
	if err != nil {
		return nil, err
	}
	blob, ok := extractJSONObject(string(html), "rankList")
	if !ok {
		return nil, fmt.Errorf("hongguo: rankList not found on %s", board)
	}
	var payload struct {
		RankList []struct {
			Rank         int      `json:"rank"`
			SeriesID     string   `json:"seriesId"`
			ID           string   `json:"id"`
			Title        string   `json:"title"`
			Cover        string   `json:"cover"`
			Description  string   `json:"description"`
			EpisodeCount int      `json:"episodeCount"`
			HeatText     string   `json:"heatText"`
			Tags         []string `json:"tags"`
		} `json:"rankList"`
		Pagination struct {
			PageNum    int `json:"pageNum"`
			TotalPages int `json:"totalPages"`
			Total      int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(blob, &payload); err != nil {
		return nil, err
	}
	items := make([]model.DramaCard, 0, len(payload.RankList))
	for _, r := range payload.RankList {
		sid := r.SeriesID
		if sid == "" {
			sid = r.ID
		}
		if sid == "" || r.Title == "" {
			continue
		}
		items = append(items, model.DramaCard{
			Source:     "hongguo",
			SeriesID:   sid,
			Title:      r.Title,
			Cover:      r.Cover,
			Intro:      r.Description,
			EpisodeCnt: r.EpisodeCount,
			RightText:  fmt.Sprintf("共%d集", r.EpisodeCount),
			Metric:     r.HeatText,
			Categories: r.Tags,
			Rank:       r.Rank,
		})
	}
	totalPages := payload.Pagination.TotalPages
	if totalPages < 1 {
		totalPages = 1
	}
	return &model.BrowseResult{
		Items:      items,
		Page:       page,
		TotalPages: totalPages,
		Total:      payload.Pagination.Total,
		HasMore:    len(items) > 0 && page < totalPages,
	}, nil
}

// CategoryBrowse fetches a Hongguo public category page (/category/{route}/{topic}?page={page}).
func (c *Client) CategoryBrowse(catRoute, topic string, page int) (*model.BrowseResult, []model.CategoryOption, error) {
	if catRoute == "" {
		catRoute = "real-drama"
	}
	if page < 1 {
		page = 1
	}
	path := "/category/" + url.PathEscape(catRoute)
	if topic != "" && topic != "all" {
		path += "/" + url.PathEscape(topic)
	}
	rawURL := fmt.Sprintf("%s?page=%d", path, page)
	html, err := c.get(rawURL, nil, BaseURL+"/")
	if err != nil {
		return nil, nil, err
	}
	res, topics, err := parseCategoryHTML(string(html), page)
	if err != nil {
		return nil, nil, err
	}
	return res, topics, nil
}

// Recommend fetches the Hongguo public recommended drama list (/category?tab=1&sort_type=1&page={page}).
func (c *Client) Recommend(page int) (*model.BrowseResult, error) {
	if page < 1 {
		page = 1
	}
	rawURL := "/category?tab=1&sort_type=1"
	if page > 1 {
		rawURL += "&page=" + strconv.Itoa(page)
	}
	html, err := c.get(rawURL, nil, BaseURL+"/")
	if err != nil {
		return nil, err
	}
	res, _, err := parseCategoryHTML(string(html), page)
	return res, err
}

func parseCategoryHTML(html string, page int) (*model.BrowseResult, []model.CategoryOption, error) {
	blob, ok := extractJSONObject(html, "recommendList")
	if !ok {
		return nil, nil, fmt.Errorf("hongguo: recommendList not found")
	}
	var payload struct {
		RecommendList []struct {
			SeriesID       string   `json:"series_id"`
			SeriesIDCamel  string   `json:"seriesId"`
			Title          string   `json:"title"`
			SeriesName     string   `json:"series_name"`
			Cover          string   `json:"cover"`
			SeriesCover    string   `json:"series_cover"`
			VideoDesc      string   `json:"video_desc"`
			SeriesIntro    string   `json:"series_intro"`
			EpisodeCnt     int      `json:"episode_cnt"`
			EpisodeCount   int      `json:"episodeCount"`
			CategorySchema string   `json:"category_schema"`
			Tags           []string `json:"tags"`
		} `json:"recommendList"`
		Pagination struct {
			PageNum    int `json:"pageNum"`
			TotalPages int `json:"totalPages"`
			Total      int `json:"total"`
		} `json:"pagination"`
		SelectorList []struct {
			Items []struct {
				ID   string `json:"selector_item_id"`
				Name string `json:"show_name"`
			} `json:"items"`
		} `json:"selectorList"`
	}
	if err := json.Unmarshal(blob, &payload); err != nil {
		return nil, nil, err
	}
	items := make([]model.DramaCard, 0, len(payload.RecommendList))
	for _, r := range payload.RecommendList {
		sid := firstNonEmpty(r.SeriesID, r.SeriesIDCamel)
		title := firstNonEmpty(r.Title, r.SeriesName)
		if sid == "" || title == "" {
			continue
		}
		epCnt := r.EpisodeCnt
		if epCnt == 0 {
			epCnt = r.EpisodeCount
		}
		tags := r.Tags
		if len(tags) == 0 && strings.TrimSpace(r.CategorySchema) != "" {
			var cats []struct {
				Name string `json:"name"`
			}
			if json.Unmarshal([]byte(r.CategorySchema), &cats) == nil {
				for _, ct := range cats {
					if ct.Name != "" {
						tags = append(tags, ct.Name)
					}
				}
			}
		}
		items = append(items, model.DramaCard{
			Source:     "hongguo",
			SeriesID:   sid,
			Title:      title,
			Cover:      firstNonEmpty(r.Cover, r.SeriesCover),
			Intro:      firstNonEmpty(r.VideoDesc, r.SeriesIntro),
			EpisodeCnt: epCnt,
			RightText:  fmt.Sprintf("共%d集", epCnt),
			Categories: tags,
		})
	}
	var topics []model.CategoryOption
	for _, grp := range payload.SelectorList {
		for _, it := range grp.Items {
			if it.ID != "" && it.Name != "" {
				topics = append(topics, model.CategoryOption{ID: it.ID, Label: it.Name})
			}
		}
	}
	totalPages := payload.Pagination.TotalPages
	if totalPages < 1 {
		if len(items) > 0 {
			totalPages = page + 1
		} else {
			totalPages = page
		}
	}
	return &model.BrowseResult{
		Items:      items,
		Page:       page,
		TotalPages: totalPages,
		Total:      payload.Pagination.Total,
		HasMore:    len(items) > 0 && page < totalPages,
	}, topics, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
