package dsd

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	BaseURL   = "https://www.dsd.com.se"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Drama struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Cover    string `json:"cover"`
	Duration string `json:"duration"`
}

type DramaDetail struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Cover    string `json:"cover"`
	M3U8Path string `json:"m3u8Path"`
}

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		baseURL:    BaseURL,
	}
}

func (c *Client) get(rawURL, referer string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("dsd http %d", resp.StatusCode)
	}
	return string(body), nil
}

// FetchMedia fetches an upstream DSD playlist or media segment with the required Referer and desktop UA.
func (c *Client) FetchMedia(rawURL string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", c.baseURL+"/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("dsd media http %d", resp.StatusCode)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

var (
	reCardBlock = regexp.MustCompile(`(?s)<a\s+href="/index\.php/vod/play/id/(\d+)/sid/\d+/nid/\d+\.html"[^>]*class="video-item"[^>]*>(.*?)</a>`)
	reDataSrc   = regexp.MustCompile(`data-src="([^"]+)"`)
	reDuration  = regexp.MustCompile(`video-item-tag-duration">([^<]+)<`)
	reDesc      = regexp.MustCompile(`class="video-desc[^"]*"[^>]*>([^<]+)<`)
)

// Search lists DSD videos matching keyword, or browses category 1 when keyword is empty.
func (c *Client) Search(keyword string) ([]Drama, error) {
	keyword = strings.TrimSpace(keyword)
	target := c.baseURL + "/index.php/vod/type/id/1.html"
	if keyword != "" {
		target = c.baseURL + "/index.php/vod/search/wd/" + url.PathEscape(keyword) + ".html"
	}
	page, err := c.get(target, c.baseURL+"/")
	if err != nil {
		return nil, err
	}
	matches := reCardBlock.FindAllStringSubmatch(page, -1)
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
	return out, nil
}

var (
	reTitleTag = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	reBgImg    = regexp.MustCompile(`background-image:\s*url\(([^)]+)\)`)
)

// Detail fetches a DSD play page and extracts title, cover, and base m3u8 path.
func (c *Client) Detail(id string) (*DramaDetail, error) {
	playPageURL := fmt.Sprintf("%s/index.php/vod/play/id/%s/sid/1/nid/1.html", c.baseURL, id)
	page, err := c.get(playPageURL, c.baseURL+"/")
	if err != nil {
		return nil, err
	}
	title := id
	if tm := reTitleTag.FindStringSubmatch(page); len(tm) == 2 {
		t := strings.TrimSpace(html.UnescapeString(tm[1]))
		t = strings.TrimPrefix(t, "在线播放")
		if idx := strings.Index(t, " 第"); idx > 0 {
			t = t[:idx]
		} else if idx := strings.Index(t, " - "); idx > 0 {
			t = t[:idx]
		}
		if t != "" {
			title = strings.TrimSpace(t)
		}
	}
	coverPath := ""
	if bm := reBgImg.FindStringSubmatch(page); len(bm) == 2 {
		coverPath = strings.Trim(strings.TrimSpace(bm[1]), `"'`)
	}
	if coverPath == "" {
		return nil, fmt.Errorf("dsd: cover/video path not found for id %s", id)
	}
	slash := strings.LastIndex(coverPath, "/")
	if slash <= 0 {
		return nil, fmt.Errorf("dsd: invalid media path %q", coverPath)
	}
	m3u8Path := coverPath[:slash] + "/1000k/index.m3u8"
	return &DramaDetail{
		ID:       id,
		Title:    title,
		Cover:    c.absURL(coverPath),
		M3U8Path: m3u8Path,
	}, nil
}

var (
	reAAEncode = regexp.MustCompile(`(?s)ﾟωﾟﾉ=.*?\('_'\);`)
	reVPath    = regexp.MustCompile(`vPath\s*=\s*"([^"]+)"`)
	wsStripper = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "")
)

// Play resolves the signed full-length HLS URL via /addons/vplayer/.
func (c *Client) Play(id string) (string, *DramaDetail, error) {
	d, err := c.Detail(id)
	if err != nil {
		return "", nil, err
	}
	vplayerURL := fmt.Sprintf("%s/addons/vplayer/?url=%s&jump=trysee", c.baseURL, url.QueryEscape(d.M3U8Path))
	page, err := c.get(vplayerURL, c.baseURL+"/")
	if err != nil {
		return "", nil, err
	}
	aa := reAAEncode.FindString(page)
	if aa == "" {
		return "", nil, fmt.Errorf("dsd: vplayer aaencode script not found")
	}
	decoded, err := decodeAAEncode(aa)
	if err != nil {
		return "", nil, fmt.Errorf("dsd: decode aaencode: %w", err)
	}
	vm := reVPath.FindStringSubmatch(decoded)
	if len(vm) != 2 {
		return "", nil, fmt.Errorf("dsd: vPath not found in decoded script")
	}
	return c.absURL(vm[1]), d, nil
}

func (c *Client) absURL(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if strings.HasPrefix(p, "//") {
		return "https:" + p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return c.baseURL + p
}

var aaTokenReplacer = strings.NewReplacer(
	"((ﾟｰﾟ)+(ﾟｰﾟ)+(ﾟΘﾟ))", "9",
	"((ﾟｰﾟ)+(ﾟｰﾟ))", "8",
	"((ﾟｰﾟ)+(o^_^o))", "7",
	"((o^_^o)+(o^_^o))", "6",
	"((ﾟｰﾟ)+(ﾟΘﾟ))", "5",
	"(ﾟｰﾟ)", "4",
	"(o^_^o)", "3",
	"((o^_^o)-(ﾟΘﾟ))", "2",
	"(ﾟΘﾟ)", "1",
	"(c^_^o)", "0",
)

// decodeAAEncode decodes aaencode obfuscated JavaScript into its underlying JS source.
func decodeAAEncode(src string) (string, error) {
	compact := wsStripper.Replace(src)
	firstSep := strings.Index(compact, "(ﾟДﾟ)[ﾟεﾟ]+")
	if firstSep < 0 {
		return "", fmt.Errorf("missing aaencode payload")
	}
	payload := compact[firstSep:]
	if end := strings.Index(payload, "(ﾟДﾟ)[ﾟoﾟ])"); end >= 0 {
		payload = payload[:end]
	}

	parts := strings.Split(payload, "(ﾟДﾟ)[ﾟεﾟ]+")
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		cleaned := aaTokenReplacer.Replace(part)
		cleaned = strings.TrimSuffix(cleaned, "+")
		if cleaned == "" {
			continue
		}
		digits := strings.ReplaceAll(cleaned, "+", "")
		if strings.HasPrefix(digits, "(ﾟДﾟ)[ﾟoﾟ]") {
			hexStr := strings.TrimPrefix(digits, "(ﾟДﾟ)[ﾟoﾟ]")
			if val, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
				out.WriteRune(rune(val))
			}
			continue
		}
		if val, err := strconv.ParseInt(digits, 8, 32); err == nil {
			out.WriteRune(rune(val))
		}
	}
	return out.String(), nil
}
