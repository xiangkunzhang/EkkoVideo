// Package hongguo implements a read-only client for the public Hongguo (红果短剧)
// web front-end.
//
// The whole chain is reachable without any ByteDance request signing:
//
//	search   GET  /api/search/result?query=...
//	detail   GET  /player/{series_id}   -> SSR JSON embedded in the HTML
//	play     POST /api/video/protected/play-info
//	                {series_id, item_id, definition}
//	                -> legacy_play_info.main_url (signed CDN URL, no Referer needed)
//
// Everything is served by the h5 BFF; the X-Gorgon / X-Argus / X-Ladon signed
// App API is only required for the native app and is deliberately not used.
package hongguo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// BaseURL is the Hongguo h5 origin.
const BaseURL = "https://hongguoduanju.com"

// DefaultUserAgent is a desktop Chrome UA. The BFF answers differently to
// obvious bots, so a plain UA is used.
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client talks to the Hongguo h5 BFF.
type Client struct {
	BaseURL string
	UA      string
	HTTP    *http.Client

	mu     sync.Mutex
	Routes *PlaybackRoutes
	feed   *recommendFeedState
}

// New builds a Client with an in-memory cookie jar.
func New() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		BaseURL: BaseURL,
		UA:      DefaultUserAgent,
		HTTP: &http.Client{
			Timeout:   30 * time.Second,
			Jar:       jar,
			Transport: newTransport(),
		},
		Routes: NewPlaybackRoutes(appPlayHosts),
	}
}

// NewWithJar builds a Client that keeps its cookies in the given jar.
// Use NewFileJar to make the session survive across invocations.
func NewWithJar(jar http.CookieJar) *Client {
	return &Client{
		BaseURL: BaseURL,
		UA:      DefaultUserAgent,
		HTTP: &http.Client{
			Timeout:   30 * time.Second,
			Jar:       jar,
			Transport: newTransport(),
		},
		Routes: NewPlaybackRoutes(appPlayHosts),
	}
}

// Jar exposes the client's cookie jar.
func (c *Client) Jar() http.CookieJar { return c.HTTP.Jar }

// newTransport keeps compression on and sets sane browser-ish headers.
func newTransport() http.RoundTripper {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 8
	tr.ResponseHeaderTimeout = 20 * time.Second
	return tr
}

func (c *Client) url(path string, query url.Values) string {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// get performs a GET with browser-ish headers.
func (c *Client) get(path string, query url.Values, referer string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.url(path, query), nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req, referer)
	return c.do(req)
}

// postJSON performs a POST with a JSON body.
func (c *Client) postJSON(path string, body any, referer string) ([]byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.url(path, nil), strings.NewReader(string(buf)))
	if err != nil {
		return nil, err
	}
	c.decorate(req, referer)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// newGet builds a GET request for an absolute URL.
func newGet(full string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, full, nil)
}

func (c *Client) decorate(req *http.Request, referer string) {
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("sec-ch-ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	c.mu.Lock()
	client := c.HTTP
	c.mu.Unlock()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 180 {
		s = s[:180] + "..."
	}
	return s
}
