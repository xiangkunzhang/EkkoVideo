package yeguo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://delta.ygrwdsgt.cc/api.php"
	aesKey         = "2acf7e91e9864673"
	aesIV          = "1c29882d3ddfcfd6"
	mediaKey       = "f5d965df75336270"
	mediaIV        = "97b60394abc2fbe1"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type Drama struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	CoverURL      string `json:"coverUrl"`
	TotalEpisodes int    `json:"totalEpisodes"`
}

type Episode struct {
	ID          string `json:"id"`
	EpNo        int    `json:"epNo"`
	Title       string `json:"title"`
	DurationSec int    `json:"durationSec"`
	Playable    bool   `json:"playable"`
}

type DramaDetail struct {
	Drama
	Episodes []Episode `json:"episodes"`
}

func NewClient() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 25 * time.Second},
	}
}

func (c *Client) post(ep string, params url.Values) (json.RawMessage, error) {
	u := strings.TrimRight(c.BaseURL, "/") + ep
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://delta.ygrwdsgt.cc")
	req.Header.Set("Referer", "https://delta.ygrwdsgt.cc/")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("yeguo http %d: %s", resp.StatusCode, string(raw))
	}

	var env struct {
		Errcode int             `json:"errcode"`
		Errmsg  string          `json:"errmsg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Errcode != 0 {
		return nil, fmt.Errorf("yeguo api %d: %s", env.Errcode, env.Errmsg)
	}

	// data is usually an AES-128-CBC base64 string, or already a JSON object.
	trimmed := strings.TrimSpace(string(env.Data))
	if strings.HasPrefix(trimmed, `"`) {
		var b64 string
		if err := json.Unmarshal(env.Data, &b64); err != nil {
			return nil, err
		}
		plain, err := decryptCBC(b64)
		if err != nil {
			return nil, err
		}
		var inner struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(plain, &inner); err == nil && len(inner.Data) > 0 {
			return inner.Data, nil
		}
		return plain, nil
	}
	return env.Data, nil
}

func decryptCBC(b64 string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	return decryptCBCBytes(ct, aesKey, aesIV)
}

func decryptCBCBytes(ct []byte, key, iv string) ([]byte, error) {
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid yeguo ciphertext length %d", len(ct))
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, []byte(iv)).CryptBlocks(plain, ct)
	pad := int(plain[len(plain)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, fmt.Errorf("invalid yeguo pkcs7 padding %d", pad)
	}
	return plain[:len(plain)-pad], nil
}

func isImageMagic(b []byte) bool {
	if len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff {
		return true // JPEG
	}
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4e && b[3] == 0x47 {
		return true // PNG
	}
	if len(b) >= 4 && b[0] == 0x47 && b[1] == 0x49 && b[2] == 0x46 && b[3] == 0x38 {
		return true // GIF
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return true // WebP
	}
	return false
}

// DecryptMedia decrypts Yeguo's AES-128-CBC encrypted cover image blobs when
// the payload does not already begin with a standard image magic header.
func DecryptMedia(raw []byte) ([]byte, error) {
	if isImageMagic(raw) {
		return raw, nil
	}
	plain, err := decryptCBCBytes(raw, mediaKey, mediaIV)
	if err != nil {
		return nil, err
	}
	if !isImageMagic(plain) {
		return nil, fmt.Errorf("yeguo decrypted media is not a valid image")
	}
	return plain, nil
}

// FetchCover downloads and decrypts a Yeguo cover image, returning its bytes and MIME type.
func (c *Client) FetchCover(rawURL string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Referer", "https://delta.ygrwdsgt.cc/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("yeguo cover http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	plain, err := DecryptMedia(raw)
	if err != nil {
		return nil, "", err
	}
	return plain, http.DetectContentType(plain), nil
}

func (c *Client) Search(q string) ([]Drama, error) {
	params := url.Values{}
	params.Set("page", "1")
	params.Set("limit", "24")
	ep := "/api/theater/exploreList"
	if strings.TrimSpace(q) != "" {
		ep = "/api/search/result"
		params.Set("keyword", strings.TrimSpace(q))
	}
	data, err := c.post(ep, params)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		List []struct {
			ID          int64  `json:"id"`
			VideoID     int64  `json:"video_id"`
			Title       string `json:"title"`
			Cover       string `json:"cover"`
			Description string `json:"description"`
			Episodes    int    `json:"episodes"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	out := make([]Drama, 0, len(parsed.List))
	for _, it := range parsed.List {
		id := it.ID
		if id == 0 {
			id = it.VideoID
		}
		out = append(out, Drama{
			ID:            strconv.FormatInt(id, 10),
			Title:         it.Title,
			Description:   it.Description,
			CoverURL:      it.Cover,
			TotalEpisodes: it.Episodes,
		})
	}
	return out, nil
}

func (c *Client) Detail(id string) (*DramaDetail, error) {
	params := url.Values{}
	params.Set("playlet_id", id)
	params.Set("video_id", id)
	data, err := c.post("/api/playlet/detail", params)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		VideoID      int64  `json:"video_id"`
		Title        string `json:"title"`
		Cover        string `json:"cover"`
		Description  string `json:"description"`
		EpisodeCount int    `json:"episode_count"`
		Episodes     []struct {
			ID       int64  `json:"id"`
			Sort     int    `json:"sort"`
			Title    string `json:"title"`
			Duration int    `json:"duration"`
			Access   struct {
				CanPlay bool `json:"can_play"`
			} `json:"access"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	eps := make([]Episode, 0, len(parsed.Episodes))
	for i, ep := range parsed.Episodes {
		no := ep.Sort
		if no <= 0 {
			no = i + 1
		}
		eps = append(eps, Episode{
			ID:          strconv.FormatInt(ep.ID, 10),
			EpNo:        no,
			Title:       ep.Title,
			DurationSec: ep.Duration,
			Playable:    ep.Access.CanPlay,
		})
	}
	return &DramaDetail{
		Drama: Drama{
			ID:            id,
			Title:         parsed.Title,
			Description:   parsed.Description,
			CoverURL:      parsed.Cover,
			TotalEpisodes: len(eps),
		},
		Episodes: eps,
	}, nil
}

func (c *Client) Play(dramaID, epID string) (string, error) {
	params := url.Values{}
	params.Set("playlet_id", dramaID)
	params.Set("video_id", dramaID)
	params.Set("episode_id", epID)
	data, err := c.post("/api/playlet/play", params)
	if err != nil {
		return "", err
	}
	var parsed struct {
		VideoURL     string `json:"video_url"`
		VideoURLH265 string `json:"video_url_h265"`
		PlayURL      string `json:"play_url"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	for _, u := range []string{parsed.VideoURL, parsed.PlayURL, parsed.VideoURLH265} {
		if strings.TrimSpace(u) != "" {
			return strings.TrimSpace(u), nil
		}
	}
	return "", fmt.Errorf("yeguo returned empty video_url")
}
