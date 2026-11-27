// Package live implements the isolated live-streaming module (Bilibili Live,
// Huya, Douyu) with real-time chat danmaku and strictly zero gift/revenue UI.
package live

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xiangkunzhang/Velan/internal/core"
)

const SourcePrefix = "live:"

// PlatformInfo describes a supported live streaming platform and its area tabs.
type PlatformInfo struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Categories []core.Section `json:"categories"`
}

// ChatMessage represents a plain live chat / danmaku message (no gifts or superchats).
type ChatMessage struct {
	Author string    `json:"author"`
	Text   string    `json:"text"`
	Color  uint32    `json:"color,omitempty"`
	Time   time.Time `json:"time"`
}

// Adapter is implemented by each live platform provider.
type Adapter interface {
	Platform() PlatformInfo
	Rooms(ctx context.Context, category string, page int) ([]core.Item, bool, error)
	Search(ctx context.Context, query string) ([]core.Item, error)
	ResolveRoom(ctx context.Context, roomID string) (*core.PlaySource, error)
	StreamDanmaku(ctx context.Context, roomID string, out chan<- ChatMessage) error
}

// Manager coordinates all live streaming platform adapters.
type Manager struct {
	http     *http.Client
	adapters map[string]Adapter
	order    []string
}

func NewManager() *Manager {
	hc := &http.Client{Timeout: 12 * time.Second}
	m := &Manager{
		http:     hc,
		adapters: make(map[string]Adapter),
	}
	for _, a := range []Adapter{
		newBilibiliAdapter(hc),
		newHuyaAdapter(hc),
		newDouyuAdapter(hc),
	} {
		id := a.Platform().ID
		m.adapters[id] = a
		m.order = append(m.order, id)
	}
	return m
}

func (m *Manager) Platforms() []PlatformInfo {
	out := make([]PlatformInfo, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.adapters[id].Platform())
	}
	return out
}

func (m *Manager) adapterFor(source string) (Adapter, error) {
	id := strings.TrimPrefix(source, SourcePrefix)
	if a, ok := m.adapters[id]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("live: unknown platform %q", source)
}

func (m *Manager) Rooms(ctx context.Context, source, category string, page int) ([]core.Item, bool, error) {
	a, err := m.adapterFor(source)
	if err != nil {
		return nil, false, err
	}
	if page < 1 {
		page = 1
	}
	return a.Rooms(ctx, category, page)
}

func (m *Manager) Search(ctx context.Context, source, query string) ([]core.Item, error) {
	a, err := m.adapterFor(source)
	if err != nil {
		return nil, err
	}
	return a.Search(ctx, query)
}

func (m *Manager) ResolveRoom(ctx context.Context, ref core.ItemRef) (*core.PlaySource, error) {
	a, err := m.adapterFor(ref.Source)
	if err != nil {
		return nil, err
	}
	return a.ResolveRoom(ctx, ref.ID)
}

func (m *Manager) StreamDanmaku(ctx context.Context, ref core.ItemRef, out chan<- ChatMessage) error {
	a, err := m.adapterFor(ref.Source)
	if err != nil {
		return err
	}
	return a.StreamDanmaku(ctx, ref.ID, out)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
