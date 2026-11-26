package hongguo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// passportParams is the parameter set the h5 account SDK sends.
//
// Notably neither a_bogus nor msToken is required: the anonymous web path
// ("web_id" visitor) is accepted as long as account_sdk_source_info — which
// is what ties a msToken to a device profile — is left out as well.
func (c *Client) passportParams(next string) url.Values {
	return url.Values{
		"next":                   {next},
		"passport_jssdk_version": {"2.4.13"},
		"passport_jssdk_type":    {"normal"},
		"is_from_ttaccountsdk":   {"1"},
		"aid":                    {"513755"},
		"language":               {"zh"},
		"account_sdk_source":     {"web"},
		"p_js_v":                 {"2.4.13"},
		"p_js_t":                 {"pro"},
		"p_zt":                   {"3.3.5"},
		"p_ver":                  {"1.0.29"},
		"p_bd":                   {"1.0.1.20"},
		"request_host":           {c.BaseURL},
	}
}

// QRCode is the login challenge returned by /passport/web/get_qrcode/.
type QRCode struct {
	Token      string
	PNG        []byte
	ExpireTime int64
	WebName    string
	Copying    string
}

// RequestQRCode asks the passport service for a login QR code.
func (c *Client) RequestQRCode(next string) (*QRCode, error) {
	if next == "" {
		next = c.BaseURL + "/"
	}
	body, err := c.get("/passport/web/get_qrcode/", c.passportParams(next), c.BaseURL+"/")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Token       string `json:"token"`
			QRCode      string `json:"qrcode"`
			ExpireTime  int64  `json:"expire_time"`
			WebName     string `json:"web_name"`
			Copywriting string `json:"copywriting"`
			ErrorCode   int    `json:"error_code"`
			Description string `json:"description"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("hongguu: decode qrcode: %w", err)
	}
	if resp.Data.ErrorCode != 0 {
		return nil, fmt.Errorf("hongguo: get_qrcode error %d: %s", resp.Data.ErrorCode, resp.Data.Description)
	}
	if resp.Data.Token == "" || resp.Data.QRCode == "" {
		return nil, fmt.Errorf("hongguo: get_qrcode returned no challenge")
	}

	png, err := decodeDataURL(resp.Data.QRCode)
	if err != nil {
		return nil, err
	}
	return &QRCode{
		Token:      resp.Data.Token,
		PNG:        png,
		ExpireTime: resp.Data.ExpireTime,
		WebName:    resp.Data.WebName,
		Copying:    resp.Data.Copywriting,
	}, nil
}

// decodeDataURL extracts the payload of a data: URL.
func decodeDataURL(s string) ([]byte, error) {
	const marker = "base64,"
	i := strings.Index(s, marker)
	if i < 0 {
		return nil, fmt.Errorf("hongguo: unsupported qrcode encoding")
	}
	return base64.StdEncoding.DecodeString(s[i+len(marker):])
}

// SaveQRCode writes the QR PNG to disk and returns the path.
func (q *QRCode) SaveQRCode(dir string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "hongguo-login-qr.png")
	if err := os.WriteFile(path, q.PNG, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// OpenQRCode opens the QR PNG with the system viewer (macOS "open").
func OpenQRCode(path string) error {
	return exec.Command("open", path).Start()
}

// PollStatus is one polling result of /passport/web/check_qrconnect/.
type PollStatus struct {
	Status      string
	Description string
	Extra       string
	Redirect    string
	Done        bool
}

// PollLogin polls the passport service until the QR code is scanned and
// confirmed, or the deadline passes.
//
// The confirmed session is stored in the client's cookie jar, so subsequent
// Resolve calls are authenticated.
func (c *Client) PollLogin(qr *QRCode, next string, timeout time.Duration) (*PollStatus, error) {
	if next == "" {
		next = c.BaseURL + "/"
	}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		params := c.passportParams(next)
		params.Set("token", qr.Token)

		body, err := c.get("/passport/web/check_qrconnect/", params, c.BaseURL+"/")
		if err != nil {
			return nil, err
		}

		var resp struct {
			Data struct {
				Status      string `json:"status"`
				Description string `json:"description"`
				Extra       string `json:"extra"`
				ErrorCode   int    `json:"error_code"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("hongguo: decode check_qrconnect: %w", err)
		}

		st := &PollStatus{
			Status:      resp.Data.Status,
			Description: resp.Data.Description,
			Extra:       resp.Data.Extra,
		}
		switch resp.Data.Status {
		case "scanned", "scanned_wait_confirm":
			return st, nil
		case "confirmed", "success":
			st.Done = true
			st.Redirect = resp.Data.Extra
			c.finishLogin(st)
			return st, nil
		case "cancel", "expired", "closed":
			return st, fmt.Errorf("hongguo: login %s", resp.Data.Status)
		}

		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("hongguo: login timed out after %s", timeout)
}

// finishLogin follows the confirmation redirect so the session cookie
// actually gets issued.
func (c *Client) finishLogin(st *PollStatus) {
	target := st.Redirect
	if target == "" {
		return
	}
	if !strings.HasPrefix(target, "http") {
		target = c.BaseURL + "/" + strings.TrimPrefix(target, "/")
	}
	// Best effort: the cookie jar records whatever the server sets.
	c.getURL(target, c.BaseURL+"/")
}

// PollOnce performs a single check_qrconnect poll and returns the state.
//
// The web UI polls this from the browser, so the login flow does not need a
// long-running process.
func (c *Client) PollOnce(token, next string) (*PollStatus, error) {
	if next == "" {
		next = c.BaseURL + "/"
	}
	params := c.passportParams(next)
	params.Set("token", token)

	body, err := c.get("/passport/web/check_qrconnect/", params, c.BaseURL+"/")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Status      string `json:"status"`
			Description string `json:"description"`
			Extra       string `json:"extra"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("hongguo: decode check_qrconnect: %w", err)
	}
	st := &PollStatus{
		Status:      resp.Data.Status,
		Description: resp.Data.Description,
		Extra:       resp.Data.Extra,
	}
	switch resp.Data.Status {
	case "confirmed", "success":
		st.Done = true
		st.Redirect = resp.Data.Extra
		c.finishLogin(st)
	}
	return st, nil
}

// WaitForScan polls until the QR code has been scanned (but not necessarily
// confirmed), so the caller can show "please confirm on your phone".
func (c *Client) WaitForScan(qr *QRCode, next string, timeout time.Duration) (*PollStatus, error) {
	if next == "" {
		next = c.BaseURL + "/"
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		params := c.passportParams(next)
		params.Set("token", qr.Token)

		body, err := c.get("/passport/web/check_qrconnect/", params, c.BaseURL+"/")
		if err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				Status      string `json:"status"`
				Description string `json:"description"`
				Extra       string `json:"extra"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("hongguo: decode check_qrconnect: %w", err)
		}
		st := &PollStatus{
			Status:      resp.Data.Status,
			Description: resp.Data.Description,
			Extra:       resp.Data.Extra,
		}
		switch resp.Data.Status {
		case "scanned", "scanned_wait_confirm":
			return st, nil
		case "confirmed", "success":
			st.Done = true
			st.Redirect = resp.Data.Extra
			c.finishLogin(st)
			return st, nil
		case "cancel", "expired", "closed":
			return st, fmt.Errorf("hongguo: login %s", resp.Data.Status)
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("hongguo: scan timed out after %s", timeout)
}

// LoggedIn reports whether the cookie jar holds a real account session.
//
// Note: ttwid is deliberately NOT part of the check. The server hands out a
// ttwid device id to anonymous visitors as well, so counting it would mark
// every guest as logged in. A real session carries one of the passport
// account cookies below.
func (c *Client) LoggedIn() bool {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return false
	}
	for _, ck := range c.HTTP.Jar.Cookies(u) {
		switch ck.Name {
		case "sessionid", "sessionid_ss", "sessionid_ss_1", "sid_tt", "sid_guard":
			if ck.Value != "" {
				return true
			}
		}
	}
	return false
}

// CookieValue returns the value of a cookie for the base URL.
func (c *Client) CookieValue(name string) string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	for _, ck := range c.HTTP.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}
