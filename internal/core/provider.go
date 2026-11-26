package core

import (
	"context"
	"sort"
	"sync"
	"time"
)

// ListStyle determines which list page template renders a provider's browse view.
type ListStyle string

const (
	ListPosterGrid  ListStyle = "poster_grid"
	ListFileBrowser ListStyle = "file_browser"
	ListChannelList ListStyle = "channel_list"
	ListLiveGrid    ListStyle = "live_grid"
)

// ProviderType groups providers in the sidebar navigation.
type ProviderType string

const (
	ProviderLocal  ProviderType = "local"
	ProviderOnline ProviderType = "online"
	ProviderIPTV   ProviderType = "iptv"
	ProviderMedia  ProviderType = "mediaserver"
)

// ProviderInfo describes static metadata and UI routing hints for a provider.
type ProviderInfo struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Type      ProviderType `json:"type"`
	ListStyle ListStyle    `json:"listStyle"`
}

// Provider is the mandatory base interface for all media sources.
type Provider interface {
	Info() ProviderInfo
}

// Optional capability interfaces discovered via type assertion.

type Cataloger interface {
	Sections(ctx context.Context) ([]Section, error)
	Browse(ctx context.Context, q Query) (Page, error)
}

type Searcher interface {
	Search(ctx context.Context, query, cursor string) (Page, error)
}

type Detailer interface {
	Detail(ctx context.Context, ref ItemRef) (*Detail, error)
}

type Recommender interface {
	Related(ctx context.Context, ref ItemRef) ([]Item, error)
}

type ResolveOpts struct {
	Quality string
	LineID  string
}

type Resolver interface {
	Resolve(ctx context.Context, ref ItemRef, opts ResolveOpts) (*PlaySource, error)
}

type Lister interface {
	List(ctx context.Context, folder ItemRef, cursor string) (Page, error)
}

type Programme struct {
	Title string    `json:"title"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type LiveGuide interface {
	Channels(ctx context.Context) ([]Item, error)
	EPG(ctx context.Context, ch ItemRef, day time.Time) ([]Programme, error)
}

type Comment struct {
	TimeSec float64 `json:"timeSec"`
	Text    string  `json:"text"`
	Color   uint32  `json:"color,omitempty"`
}

type DanmakuSrc interface {
	Danmaku(ctx context.Context, ref DanmakuRef, from, to time.Duration) ([]Comment, error)
}

type LiveChat interface {
	Connect(ctx context.Context, ref DanmakuRef) (<-chan Comment, error)
}

type Progresser interface {
	Report(ctx context.Context, ref ItemRef, pos, dur time.Duration, state PlayState) error
}

type Credentials struct {
	ServerURL string `json:"serverUrl,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Token     string `json:"token,omitempty"`
}

type Authn interface {
	Login(ctx context.Context, cred Credentials) error
	LoggedIn() bool
	Logout()
}

// Registry holds registered provider instances by ID.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

func (r *Registry) Register(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Info().ID] = p
}

func (r *Registry) Get(id string) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[id]
}

func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Info().ID < out[j].Info().ID
	})
	return out
}
