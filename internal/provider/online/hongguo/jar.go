package hongguo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

// FileJar is a minimal http.CookieJar that persists to disk.
//
// The standard library's cookiejar cannot be serialised, and the login
// session has to survive across process invocations: the user scans a QR
// code in one command and plays episodes in the next one.
type FileJar struct {
	mu      sync.Mutex
	path    string
	cookies []*http.Cookie
}

// NewFileJar loads (or creates) a cookie jar at path.
func NewFileJar(path string) (*FileJar, error) {
	if path == "" {
		return nil, os.ErrInvalid
	}
	j := &FileJar{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &j.cookies)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return j, nil
}

// SetCookies stores cookies for the given URL.
func (j *FileJar) SetCookies(u *url.URL, cs []*http.Cookie) {
	if len(cs) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	for _, c := range cs {
		replaced := false
		for i, existing := range j.cookies {
			if existing.Name == c.Name && existing.Domain == c.Domain && existing.Path == c.Path {
				j.cookies[i] = c
				replaced = true
				break
			}
		}
		if !replaced {
			j.cookies = append(j.cookies, c)
		}
	}
	j.save()
}

// Cookies returns the cookies that apply to the given URL.
func (j *FileJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	var out []*http.Cookie
	for _, c := range j.cookies {
		if c.MaxAge < 0 {
			continue
		}
		if c.Domain != "" && !hostMatch(u.Hostname(), c.Domain) {
			continue
		}
		if c.Path != "" && !pathMatch(u.Path, c.Path) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Save writes the jar to disk.
func (j *FileJar) Save() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.save()
}

// Clear removes every stored cookie.
func (j *FileJar) Clear() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cookies = nil
	return j.save()
}

// Names lists the stored cookie names.
func (j *FileJar) Names() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.cookies))
	for _, c := range j.cookies {
		out = append(out, c.Name)
	}
	return out
}

// Raw returns the cookies as a "name=value; name2=value2" header string.
func (j *FileJar) Raw() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := ""
	for i, c := range j.cookies {
		if i > 0 {
			out += "; "
		}
		out += c.Name + "=" + c.Value
	}
	return out
}

func (j *FileJar) save() error {
	b, err := json.MarshalIndent(j.cookies, "", "  ")
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}

func hostMatch(host, domain string) bool {
	if domain == "" {
		return true
	}
	domain = trimDot(domain)
	host = trimDot(host)
	if host == domain {
		return true
	}
	return len(host) > len(domain) && host[len(host)-len(domain)-1:] == "."+domain
}

func trimDot(s string) string {
	if len(s) > 0 && s[0] == '.' {
		return s[1:]
	}
	return s
}

func pathMatch(p, path string) bool {
	if path == "" || path == "/" {
		return true
	}
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	if p == path {
		return true
	}
	if len(p) > len(path) && p[:len(path)] == path && p[len(path)] == '/' {
		return true
	}
	return false
}
