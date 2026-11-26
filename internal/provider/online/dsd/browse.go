package dsd

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

// Browse lists DSD videos by mode (category, rank, recommend), category ID, sort, keyword, and page.
func (c *Client) Browse(mode, catID, sortBy, keyword string, page int) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if catID == "" || catID == "all" {
		catID = "1"
	}
	keyword = strings.TrimSpace(keyword)

	var target string
	switch {
	case keyword != "":
		if page > 1 {
			target = fmt.Sprintf("%s/index.php/vod/search/page/%d/wd/%s.html", c.baseURL, page, url.PathEscape(keyword))
		} else {
			target = fmt.Sprintf("%s/index.php/vod/search/wd/%s.html", c.baseURL, url.PathEscape(keyword))
		}
	case mode == "rank":
		by := sortBy
		if by == "" {
			by = "hits"
		}
		target = fmt.Sprintf("%s/index.php/vod/show/by/%s/id/%s/page/%d.html", c.baseURL, url.PathEscape(by), url.PathEscape(catID), page)
	case mode == "recommend":
		target = fmt.Sprintf("%s/index.php/vod/show/by/up/id/%s/page/%d.html", c.baseURL, url.PathEscape(catID), page)
	default:
		if page > 1 {
			target = fmt.Sprintf("%s/index.php/vod/type/id/%s/page/%d.html", c.baseURL, url.PathEscape(catID), page)
		} else {
			target = fmt.Sprintf("%s/index.php/vod/type/id/%s.html", c.baseURL, url.PathEscape(catID))
		}
	}

	pageHTML, err := c.get(target, c.baseURL+"/")
	if err != nil {
		return nil, false, err
	}
	matches := reCardBlock.FindAllStringSubmatch(pageHTML, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]Drama, 0, len(matches))
	for _, m := range matches {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		block := m[2]
		cover := ""
		if cm := reDataSrc.FindStringSubmatch(block); len(cm) == 2 {
			cover = c.absURL(cm[1])
		}
		dur := ""
		if dm := reDuration.FindStringSubmatch(block); len(dm) == 2 {
			dur = strings.TrimSpace(dm[1])
		}
		title := id
		if tm := reDesc.FindStringSubmatch(block); len(tm) == 2 {
			title = strings.TrimSpace(html.UnescapeString(tm[1]))
		}
		out = append(out, Drama{
			ID:       id,
			Title:    title,
			Cover:    cover,
			Duration: dur,
		})
	}
	hasMore := strings.Contains(pageHTML, fmt.Sprintf("/page/%d.html", page+1))
	return out, hasMore, nil
}
