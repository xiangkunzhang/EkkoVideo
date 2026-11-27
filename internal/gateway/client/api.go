package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/provider/online"
)

func (s *Supervisor) getJSON(ctx context.Context, path string, out any) error {
	s.mu.Lock()
	addr := s.status.Addr
	token := s.token
	s.mu.Unlock()
	if addr == "" {
		return errors.New("网关服务未就绪")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return errors.New(errBody.Error)
		}
		return fmt.Errorf("gateway status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Supervisor) Providers(ctx context.Context) ([]core.ProviderInfo, error) {
	var res struct {
		Providers []core.ProviderInfo `json:"providers"`
	}
	if err := s.getJSON(ctx, "/v1/providers", &res); err != nil {
		return nil, err
	}
	return res.Providers, nil
}

func (s *Supervisor) Sections(ctx context.Context, source string) (online.SectionsResponse, error) {
	var res online.SectionsResponse
	err := s.getJSON(ctx, "/v1/sections?source="+url.QueryEscape(source), &res)
	return res, err
}

func (s *Supervisor) Browse(ctx context.Context, source string, q core.Query) (core.Page, error) {
	v := url.Values{}
	v.Set("source", source)
	if q.Section != "" {
		v.Set("section", q.Section)
	}
	if q.Category != "" {
		v.Set("category", q.Category)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	var res core.Page
	err := s.getJSON(ctx, "/v1/browse?"+v.Encode(), &res)
	return res, err
}

func (s *Supervisor) Search(ctx context.Context, source, query, cursor string) (core.Page, error) {
	v := url.Values{}
	v.Set("source", source)
	v.Set("q", query)
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	var res core.Page
	err := s.getJSON(ctx, "/v1/search?"+v.Encode(), &res)
	return res, err
}

func (s *Supervisor) Detail(ctx context.Context, source, id string) (*core.Detail, error) {
	v := url.Values{}
	v.Set("source", source)
	v.Set("id", id)
	var res core.Detail
	if err := s.getJSON(ctx, "/v1/detail?"+v.Encode(), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (s *Supervisor) Related(ctx context.Context, source, id, title string) ([]core.Item, error) {
	v := url.Values{}
	v.Set("source", source)
	v.Set("id", id)
	v.Set("title", title)
	var res struct {
		Items []core.Item `json:"items"`
	}
	if err := s.getJSON(ctx, "/v1/related?"+v.Encode(), &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (s *Supervisor) Resolve(ctx context.Context, source, seriesID, vid string, ep int, quality string) (*core.PlaySource, error) {
	v := url.Values{}
	v.Set("source", source)
	v.Set("series_id", seriesID)
	v.Set("vid", vid)
	v.Set("ep", strconv.Itoa(ep))
	if quality != "" {
		v.Set("quality", quality)
	}
	var res core.PlaySource
	if err := s.getJSON(ctx, "/v1/resolve?"+v.Encode(), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (s *Supervisor) Danmaku(ctx context.Context, source, seriesID, epID string) ([]core.Comment, error) {
	v := url.Values{}
	v.Set("source", source)
	v.Set("series_id", seriesID)
	v.Set("ep_id", epID)
	var res struct {
		Items []core.Comment `json:"items"`
	}
	if err := s.getJSON(ctx, "/v1/danmaku?"+v.Encode(), &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}
