// Package imageloader downloads, caches on disk as JPEG, and manages in-memory
// *ui.Bitmap instances with automatic idle eviction and playback-time clearing.
package imageloader

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/Velan/internal/infra/safego"
)

const (
	posterWidth = 360
	idleTTL     = time.Minute
)

type entry struct {
	bmp  *ui.Bitmap
	at   time.Time
	used time.Time
}

// Loader is main-thread owned for Get/ClearMemory; network/decoding runs in bounded goroutines.
type Loader struct {
	dir     string
	update  func(func())
	client  *http.Client
	sem     chan struct{}
	loaded  map[string]*entry
	pending map[string]bool
	swept   time.Time
}

func New(dir string, update func(func())) *Loader {
	_ = os.MkdirAll(dir, 0o755)
	return &Loader{
		dir:     dir,
		update:  update,
		client:  &http.Client{Timeout: 20 * time.Second},
		sem:     make(chan struct{}, 4),
		loaded:  make(map[string]*entry),
		pending: make(map[string]bool),
	}
}

// ClearMemory drops all in-memory GPU/CPU bitmaps (called when entering playback).
func (l *Loader) ClearMemory() {
	if l == nil {
		return
	}
	clear(l.loaded)
}

func (l *Loader) Get(c *ui.Context, rawURL string) (*ui.Bitmap, float32) {
	if l == nil || rawURL == "" {
		return nil, 0
	}
	now := time.Now()
	if now.Sub(l.swept) > 10*time.Second {
		l.sweep(now)
	}
	k := keyOf(rawURL)
	if e := l.loaded[k]; e != nil {
		e.used = now
		if !e.at.IsZero() {
			alpha := float32(time.Since(e.at)) / float32(180*time.Millisecond)
			if alpha < 1 {
				c.Invalidate()
				return e.bmp, alpha
			}
		}
		return e.bmp, 1
	}
	if !l.pending[k] {
		l.pending[k] = true
		safego.Go("img-load", nil, func() { l.load(k, rawURL) })
	}
	return nil, 0
}

func (l *Loader) sweep(now time.Time) {
	l.swept = now
	for k, e := range l.loaded {
		if now.Sub(e.used) > idleTTL {
			delete(l.loaded, k)
		}
	}
}

func (l *Loader) load(k, rawURL string) {
	l.sem <- struct{}{}
	defer func() { <-l.sem }()

	bmp, fresh, err := l.fetchOrDisk(k, rawURL)
	l.update(func() {
		delete(l.pending, k)
		if err == nil && bmp != nil {
			e := &entry{bmp: bmp, used: time.Now()}
			if fresh {
				e.at = time.Now()
			}
			l.loaded[k] = e
		}
	})
}

func (l *Loader) fetchOrDisk(k, rawURL string) (*ui.Bitmap, bool, error) {
	p := filepath.Join(l.dir, k+".jpg")
	if data, err := os.ReadFile(p); err == nil {
		if bmp, err := ui.DecodeBitmap(data); err == nil {
			return bmp, false, nil
		}
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, true, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, true, errors.New(resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	// Try standard Go/MyGo decode first, fall back to macOS ImageIO for HEIC.
	if bmp, err := ui.DecodeBitmap(raw); err == nil {
		_ = os.WriteFile(p, raw, 0o644)
		return bmp, true, nil
	}
	img, err := decodeAndScalePlatform(raw, posterWidth)
	if err != nil {
		return nil, true, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err == nil {
		_ = os.WriteFile(p, buf.Bytes(), 0o644)
	}
	return ui.NewBitmap(img), true, nil
}

func keyOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		raw = u.Host + u.Path
	}
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}
