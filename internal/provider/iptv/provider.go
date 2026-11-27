package iptv

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

const SourceID = "iptv"

// Subscription represents a user-configured M3U/M3U8 source.
type Subscription struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"` // http(s) URL or local file path
	EPGURL  string `json:"epgUrl,omitempty"`
	Enabled bool   `json:"enabled"`
}

// Manager implements core.Provider, core.LiveGuide, and core.Resolver for IPTV.
type Manager struct {
	cacheDir string
	client   *http.Client

	mu          sync.RWMutex
	subs        []Subscription
	channels    []Channel
	groups      []string
	epg         *EPGStore
	lastLineIDs map[string]string // channelID -> last successful line ID
}

func NewManager(cacheDir string, subs []Subscription) *Manager {
	_ = os.MkdirAll(cacheDir, 0o755)
	return &Manager{
		cacheDir:    cacheDir,
		client:      &http.Client{Timeout: 20 * time.Second},
		subs:        append([]Subscription(nil), subs...),
		epg:         NewEPGStore(),
		lastLineIDs: make(map[string]string),
	}
}

func (m *Manager) Info() core.ProviderInfo {
	return core.ProviderInfo{
		ID:        SourceID,
		Name:      "IPTV 电视",
		Type:      core.ProviderIPTV,
		ListStyle: core.ListChannelList,
	}
}

func (m *Manager) Subscriptions() []Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Subscription(nil), m.subs...)
}

func (m *Manager) SetSubscriptions(subs []Subscription) {
	m.mu.Lock()
	m.subs = append([]Subscription(nil), subs...)
	m.mu.Unlock()
}

// Refresh fetches all enabled M3U subscriptions and their XMLTV EPGs.
func (m *Manager) Refresh(ctx context.Context) error {
	subs := m.Subscriptions()
	var allChannels []Channel
	var allGroups []string
	groupSeen := map[string]bool{}
	var epgURLs []string
	var firstErr error

	for _, sub := range subs {
		if !sub.Enabled && len(subs) > 1 {
			continue
		}
		raw, err := m.fetchResource(ctx, sub.URL, "m3u_"+sub.ID+".m3u")
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		pl, err := ParseM3U(bytes.NewReader(raw))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, g := range pl.Groups {
			if !groupSeen[g] {
				groupSeen[g] = true
				allGroups = append(allGroups, g)
			}
		}
		for _, ch := range pl.Channels {
			ch.Number = len(allChannels) + 1
			allChannels = append(allChannels, ch)
		}
		if sub.EPGURL != "" {
			epgURLs = append(epgURLs, sub.EPGURL)
		} else if pl.EPGURL != "" {
			epgURLs = append(epgURLs, pl.EPGURL)
		}
	}

	if len(allChannels) == 0 && firstErr != nil {
		return firstErr
	}

	epgStore := NewEPGStore()
	for _, eu := range epgURLs {
		raw, err := m.fetchResource(ctx, eu, "epg_"+hashStr(eu)+".xml")
		if err != nil {
			continue
		}
		if parsed, err := ParseXMLTV(raw, time.Now()); err == nil {
			epgStore = parsed
			break
		}
	}

	m.mu.Lock()
	m.channels = allChannels
	m.groups = allGroups
	m.epg = epgStore
	m.mu.Unlock()
	return nil
}

func (m *Manager) Snapshot() ([]Channel, []string, *EPGStore) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Channel(nil), m.channels...), append([]string(nil), m.groups...), m.epg
}

func (m *Manager) Channels(_ context.Context) ([]core.Item, error) {
	chs, _, epg := m.Snapshot()
	now := time.Now()
	out := make([]core.Item, 0, len(chs))
	for _, ch := range chs {
		sub := ch.Group
		if cur, _ := epg.NowAndNext(ch.TVGID, ch.Name, now); cur != nil {
			sub = fmt.Sprintf("%s · 正在播出: %s", ch.Group, cur.Title)
		}
		out = append(out, core.Item{
			Ref:      core.ItemRef{Source: SourceID, Kind: core.KindChannel, ID: ch.ID},
			Title:    ch.Name,
			Subtitle: sub,
			Poster:   ch.Logo,
			Index:    ch.Number,
			Playable: true,
			Children: len(ch.Lines),
			Extra:    map[string]string{"group": ch.Group, "tvgId": ch.TVGID},
		})
	}
	return out, nil
}

func (m *Manager) EPG(_ context.Context, chRef core.ItemRef, day time.Time) ([]core.Programme, error) {
	m.mu.RLock()
	epg := m.epg
	var chName, tvgID string
	for _, c := range m.channels {
		if c.ID == chRef.ID {
			chName, tvgID = c.Name, c.TVGID
			break
		}
	}
	m.mu.RUnlock()

	all := epg.ForChannel(tvgID, chName)
	if day.IsZero() {
		return all, nil
	}
	y, mo, d := day.Date()
	var filtered []core.Programme
	for _, p := range all {
		py, pmo, pd := p.Start.Date()
		if py == y && pmo == mo && pd == d {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

func (m *Manager) Resolve(_ context.Context, ref core.ItemRef, opts core.ResolveOpts) (*core.PlaySource, error) {
	m.mu.RLock()
	var found *Channel
	for i := range m.channels {
		if m.channels[i].ID == ref.ID {
			c := m.channels[i]
			found = &c
			break
		}
	}
	preferredLine := opts.LineID
	if preferredLine == "" {
		preferredLine = m.lastLineIDs[ref.ID]
	}
	m.mu.RUnlock()

	if found == nil || len(found.Lines) == 0 {
		return nil, errors.New("未找到该电视频道或无可用线路")
	}

	// Reorder lines so preferredLine (remembered working line or requested line) is first.
	lines := append([]core.Line(nil), found.Lines...)
	if preferredLine != "" {
		for i, ln := range lines {
			if ln.ID == preferredLine && i > 0 {
				lines[0], lines[i] = lines[i], lines[0]
				break
			}
		}
	}

	return &core.PlaySource{
		Ref:     ref,
		Title:   fmt.Sprintf("[%03d] %s", found.Number, found.Name),
		URL:     lines[0].URL,
		Kind:    lines[0].Kind,
		Headers: lines[0].Headers,
		Lines:   lines,
		Live:    true,
	}, nil
}

// RememberWorkingLine records the line ID that succeeded for channelID.
func (m *Manager) RememberWorkingLine(channelID, lineID string) {
	if channelID == "" || lineID == "" {
		return
	}
	m.mu.Lock()
	m.lastLineIDs[channelID] = lineID
	m.mu.Unlock()
}

// BuildQueue builds a channel queue across all channels (or filtered) centered on channelID.
func (m *Manager) BuildQueue(channelID string) *core.Queue {
	items, _ := m.Channels(context.Background())
	idx := 0
	for i, it := range items {
		if it.Ref.ID == channelID {
			idx = i
			break
		}
	}
	return core.NewQueue(items, idx)
}

func (m *Manager) fetchResource(ctx context.Context, rawURL, cacheName string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("empty url")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return os.ReadFile(rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "EkkoVideo/1.0")
	resp, err := m.client.Do(req)
	if err != nil {
		if m.cacheDir != "" {
			if cached, rerr := os.ReadFile(filepath.Join(m.cacheDir, cacheName)); rerr == nil {
				return cached, nil
			}
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if m.cacheDir != "" {
		_ = os.WriteFile(filepath.Join(m.cacheDir, cacheName), data, 0o644)
	}
	return data, nil
}

func hashStr(s string) string {
	sum := sha1.Sum([]byte(s + strconv.Itoa(time.Now().YearDay())))
	return hex.EncodeToString(sum[:8])
}
