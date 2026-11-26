package huangju

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Category struct {
	ID         int64  `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	DramaCount int    `json:"dramaCount"`
}

func (c *Client) Categories() ([]Category, error) {
	raw, err := c.doGet("/categories")
	if err != nil {
		return nil, err
	}
	var out []Category
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Browse(keyword, category, sort string, page, pageSize int) ([]Drama, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 24
	}
	q := url.Values{}
	if strings.TrimSpace(keyword) != "" {
		q.Set("q", strings.TrimSpace(keyword))
	}
	if strings.TrimSpace(category) != "" && category != "all" {
		q.Set("category", strings.TrimSpace(category))
	}
	if strings.TrimSpace(sort) != "" {
		q.Set("sort", strings.TrimSpace(sort))
	}
	q.Set("pageNum", strconv.Itoa(page))
	q.Set("pageSize", strconv.Itoa(pageSize))
	raw, err := c.doGet("/dramas?" + q.Encode())
	if err != nil {
		return nil, 0, err
	}
	var out struct {
		Total int     `json:"total"`
		Items []Drama `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, 0, fmt.Errorf("huangju browse unmarshal: %w", err)
	}
	return out.Items, out.Total, nil
}
