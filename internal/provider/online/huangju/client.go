package huangju

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.huangju.net"

// Client talks to api.huangju.net and persists CloudFront media cookies
// returned by /play/{id} so video.huangju.net HLS segments can be fetched.
type Client struct {
	BaseURL string
	HTTP    *http.Client

	mu       sync.Mutex
	token    string
	deviceID string
}

type Drama struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	CoverURL      string `json:"coverUrl"`
	TotalEpisodes int    `json:"totalEpisodes"`
}

type Episode struct {
	ID          int64  `json:"id"`
	EpNo        int    `json:"epNo"`
	Title       string `json:"title"`
	DurationSec int    `json:"durationSec"`
	Playable    bool   `json:"playable"`
}

type DramaDetail struct {
	Drama
	Episodes []Episode `json:"episodes"`
}

type PlayResult struct {
	EpisodeID int64  `json:"episodeId"`
	DramaID   int64  `json:"dramaId"`
	EpNo      int    `json:"epNo"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt"`
}

func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP: &http.Client{
			Jar:     jar,
			Timeout: 25 * time.Second,
		},
		deviceID: newUUIDv4(),
	}
}

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (c *Client) ensureToken() (string, error) {
	c.mu.Lock()
	tok := c.token
	devID := c.deviceID
	c.mu.Unlock()
	if tok != "" {
		return tok, nil
	}

	body, _ := json.Marshal(map[string]string{"deviceId": devID})
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/auth/guest", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("huangju: auth/guest: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("huangju: auth/guest http %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("huangju: invalid auth/guest response: %s", string(raw))
	}

	c.mu.Lock()
	c.token = out.Token
	c.mu.Unlock()
	return out.Token, nil
}

func (c *Client) doGet(path string) ([]byte, error) {
	tok, err := c.ensureToken()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		tok, err = c.ensureToken()
		if err != nil {
			return nil, err
		}
		req2, _ := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
		req2.Header.Set("Authorization", "Bearer "+tok)
		req2.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
		resp2, err := c.HTTP.Do(req2)
		if err != nil {
			return nil, err
		}
		defer resp2.Body.Close()
		raw, _ := io.ReadAll(resp2.Body)
		if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
			return nil, fmt.Errorf("huangju: %s http %d: %s", path, resp2.StatusCode, string(raw))
		}
		return raw, nil
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("huangju: %s http %d: %s", path, resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (c *Client) Search(keyword string) ([]Drama, error) {
	q := url.Values{}
	if keyword != "" {
		q.Set("q", keyword)
	}
	q.Set("pageNum", "1")
	q.Set("pageSize", "20")
	raw, err := c.doGet("/dramas?" + q.Encode())
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []Drama `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) Detail(slug string) (*DramaDetail, error) {
	raw, err := c.doGet("/dramas/" + url.PathEscape(slug))
	if err != nil {
		return nil, err
	}
	var d DramaDetail
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) Play(episodeID int64) (*PlayResult, error) {
	raw, err := c.doGet(fmt.Sprintf("/play/%d", episodeID))
	if err != nil {
		return nil, err
	}
	var res PlayResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// FetchMedia fetches an HLS playlist or segment with the client's CloudFront cookies.
func (c *Client) FetchMedia(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	return c.HTTP.Do(req)
}
