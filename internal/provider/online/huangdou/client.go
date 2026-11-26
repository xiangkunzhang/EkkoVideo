package huangdou

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://xqjurgek.top"
	hmacSecret     = "7961beb44246e3012ce228d6b5ced05a"
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
	ID       string `json:"id"`
	EpNo     int    `json:"epNo"`
	Title    string `json:"title"`
	Playable bool   `json:"playable"`
}

type DramaDetail struct {
	Drama
	Episodes []Episode `json:"episodes"`
}

func NewClient() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP: &http.Client{
			Timeout: 25 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
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

func randomHex16() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func deriveKey(reqID string) ([]byte, error) {
	rawID, err := hex.DecodeString(strings.ReplaceAll(reqID, "-", ""))
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write(rawID)
	return mac.Sum(nil), nil
}

func (c *Client) call(path string, data any) (json.RawMessage, error) {
	reqID := newUUIDv4()
	sessID := randomHex16()
	key, err := deriveKey(reqID)
	if err != nil {
		return nil, err
	}

	wrapper := map[string]any{
		"token":    "",
		"deviceId": reqID,
		"data":     data,
	}
	plainJSON, err := json.Marshal(wrapper)
	if err != nil {
		return nil, err
	}

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(plainJSON); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	iv := make([]byte, aes.BlockSize)
	_, _ = rand.Read(iv)
	ct, err := encryptCBC(gzBuf.Bytes(), key, iv)
	if err != nil {
		return nil, err
	}
	body := append(iv, ct...)

	fullURL := strings.TrimRight(c.BaseURL, "/") + "/api" + path
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	hostPath := strings.TrimPrefix(strings.TrimPrefix(fullURL, "https://"), "http://")
	signInput := fmt.Sprintf("Dart|%s|%s|%s|%s", sessID, reqID, ts, hostPath)
	sum := md5.Sum([]byte(signInput))
	sign := hex.EncodeToString(sum[:]) + "-" + ts

	req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", c.BaseURL)
	req.Header.Set("Referer", c.BaseURL+"/")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("version", "2.0.0")
	req.Header.Set("deviceType", "web")
	req.Header.Set("time", ts)
	req.Header.Set("sign", sign)
	req.Header.Set("requestId", reqID)
	req.Header.Set("sessionId", sessID)

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
		return nil, fmt.Errorf("huangdou http %d: %s", resp.StatusCode, string(raw))
	}

	var decrypted []byte
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		decrypted = raw
	} else {
		if len(raw) <= aes.BlockSize {
			return nil, fmt.Errorf("huangdou response too short (%d)", len(raw))
		}
		plainGZ, err := decryptCBC(raw[aes.BlockSize:], key, raw[:aes.BlockSize])
		if err != nil {
			return nil, err
		}
		gr, err := gzip.NewReader(bytes.NewReader(plainGZ))
		if err != nil {
			return nil, err
		}
		decrypted, err = io.ReadAll(gr)
		_ = gr.Close()
		if err != nil {
			return nil, err
		}
	}

	var env struct {
		Status string          `json:"status"`
		Error  string          `json:"error"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(decrypted, &env); err != nil {
		return nil, err
	}
	if env.Status != "y" {
		return nil, fmt.Errorf("huangdou api error: %s", env.Error)
	}
	return env.Data, nil
}

func encryptCBC(plain, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - (len(plain) % aes.BlockSize)
	padded := make([]byte, len(plain)+pad)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

func decryptCBC(ct, key, iv []byte) ([]byte, error) {
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid huangdou ciphertext length %d", len(ct))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
	pad := int(plain[len(plain)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, fmt.Errorf("invalid huangdou pkcs7 padding %d", pad)
	}
	return plain[:len(plain)-pad], nil
}

func (c *Client) Search(q string) ([]Drama, error) {
	q = strings.TrimSpace(q)
	data, err := c.call("/drama/list", map[string]any{
		"keyword": q,
		"page":    1,
		"limit":   30,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		List []struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			Name         string `json:"name"`
			Description  string `json:"description"`
			Img          string `json:"img"`
			EpisodeCount any    `json:"episode_count"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	out := make([]Drama, 0, len(parsed.List))
	ql := strings.ToLower(q)
	for _, it := range parsed.List {
		title := it.Title
		if title == "" {
			title = it.Name
		}
		if ql != "" && !strings.Contains(strings.ToLower(title), ql) && !strings.Contains(strings.ToLower(it.Description), ql) {
			continue
		}
		epCnt := anyInt(it.EpisodeCount)
		out = append(out, Drama{
			ID:            it.ID,
			Title:         title,
			Description:   it.Description,
			CoverURL:      it.Img,
			TotalEpisodes: epCnt,
		})
	}
	// If local filter matched nothing on page 1, still return page 1 items if q == ""
	if len(out) == 0 && ql != "" {
		for _, it := range parsed.List {
			title := it.Title
			if title == "" {
				title = it.Name
			}
			out = append(out, Drama{
				ID:            it.ID,
				Title:         title,
				Description:   it.Description,
				CoverURL:      it.Img,
				TotalEpisodes: anyInt(it.EpisodeCount),
			})
		}
	}
	return out, nil
}

func anyInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}

func (c *Client) Detail(id string) (*DramaDetail, error) {
	data, err := c.call("/drama/detail", map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		Img          string `json:"img"`
		EpisodeCount int    `json:"episode_count"`
		Episodes     []struct {
			ID      any    `json:"id"`
			Episode int    `json:"episode"`
			Title   string `json:"title"`
			IsVIP   int    `json:"is_vip"`
			Bought  bool   `json:"bought"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	title := parsed.Title
	if title == "" {
		title = parsed.Name
	}
	eps := make([]Episode, 0, len(parsed.Episodes))
	for i, ep := range parsed.Episodes {
		no := ep.Episode
		if no <= 0 {
			no = i + 1
		}
		eps = append(eps, Episode{
			ID:       fmt.Sprintf("%v", ep.ID),
			EpNo:     no,
			Title:    ep.Title,
			Playable: ep.IsVIP == 0 || ep.Bought,
		})
	}
	return &DramaDetail{
		Drama: Drama{
			ID:            id,
			Title:         title,
			Description:   parsed.Description,
			CoverURL:      parsed.Img,
			TotalEpisodes: len(eps),
		},
		Episodes: eps,
	}, nil
}

func (c *Client) Play(dramaID string, ep int) (string, error) {
	data, err := c.call("/drama/play", map[string]any{
		"id":      dramaID,
		"episode": ep,
	})
	if err != nil {
		return "", err
	}
	var parsed struct {
		PlayURL string `json:"play_url"`
		Lines   []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	for _, ln := range parsed.Lines {
		if strings.TrimSpace(ln.URL) != "" {
			return strings.TrimSpace(ln.URL), nil
		}
	}
	if strings.TrimSpace(parsed.PlayURL) != "" {
		return strings.TrimSpace(parsed.PlayURL), nil
	}
	return "", fmt.Errorf("huangdou returned no playable lines")
}
