package huangdou

import (
	"encoding/json"
	"strings"
)

// Browse supports paginated /drama/list and /drama/rank.
func (c *Client) Browse(mode, q string, page, limit int) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 24
	}
	q = strings.TrimSpace(q)
	ep := "/drama/list"
	if mode == "rank" && q == "" {
		ep = "/drama/rank"
	}
	data, err := c.call(ep, map[string]any{
		"keyword": q,
		"page":    page,
		"limit":   limit,
	})
	if err != nil {
		return nil, false, err
	}
	var parsed struct {
		List []struct {
			ID           string `json:"id"`
			DramaID      string `json:"drama_id"`
			Title        string `json:"title"`
			Name         string `json:"name"`
			Description  string `json:"description"`
			Img          string `json:"img"`
			EpisodeCount any    `json:"episode_count"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, false, err
	}
	out := make([]Drama, 0, len(parsed.List))
	for _, it := range parsed.List {
		id := it.ID
		if id == "" {
			id = it.DramaID
		}
		title := it.Title
		if title == "" {
			title = it.Name
		}
		if id == "" || title == "" {
			continue
		}
		out = append(out, Drama{
			ID:            id,
			Title:         title,
			Description:   it.Description,
			CoverURL:      it.Img,
			TotalEpisodes: anyInt(it.EpisodeCount),
		})
	}
	return out, len(parsed.List) >= limit, nil
}
