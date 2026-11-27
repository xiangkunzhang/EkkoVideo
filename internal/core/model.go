// Package core defines Velan's UI-independent domain models, provider
// contracts, playback queue, history, settings, keymap, and session state.
package core

import "time"

// Kind identifies the semantic type of a media item.
type Kind int

const (
	KindSeries   Kind = iota // Short drama / TV series
	KindMovie                // Single movie
	KindSeason               // Season within a series
	KindEpisode              // Episode within a series/season
	KindChannel              // IPTV channel (has EPG)
	KindLiveRoom             // Live streaming room (no episodes, live chat)
	KindFolder               // Local / Alist / WebDAV folder
	KindFile                 // Single media file
)

// ItemRef uniquely identifies a media item across all providers.
type ItemRef struct {
	Source string `json:"source"`
	Kind   Kind   `json:"kind"`
	ID     string `json:"id"`
}

// Item is the unified display model for grids, lists, queues, and history.
type Item struct {
	Ref      ItemRef           `json:"ref"`
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle,omitempty"`
	Poster   string            `json:"poster,omitempty"`
	Backdrop string            `json:"backdrop,omitempty"`
	Overview string            `json:"overview,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Rating   float32           `json:"rating,omitempty"`
	Duration time.Duration     `json:"duration,omitempty"`
	Index    int               `json:"index,omitempty"`
	Playable bool              `json:"playable"`
	Locked   bool              `json:"locked,omitempty"`
	Children int               `json:"children,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// StreamKind classifies the media transport format.
type StreamKind int

const (
	StreamMP4 StreamKind = iota
	StreamHLS
	StreamDASH
	StreamFLV
	StreamRTMP
	StreamLocalFile
)

// Line represents a selectable CDN/quality/backup stream line.
type Line struct {
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	URL     string            `json:"url"`
	Kind    StreamKind        `json:"kind"`
	Headers map[string]string `json:"headers,omitempty"`
}

// SubtitleTrack describes an external subtitle file or URL.
type SubtitleTrack struct {
	Label    string `json:"label"`
	Language string `json:"language,omitempty"`
	URL      string `json:"url"`
}

// DecryptSpec describes stream decryption parameters handled by the gateway.
type DecryptSpec struct {
	Scheme string `json:"scheme"` // e.g. "cenc"
	KeyHex string `json:"keyHex,omitempty"`
}

// DanmakuRef identifies where timed comments or live chat come from.
type DanmakuRef struct {
	Source   string `json:"source"`
	SeriesID string `json:"seriesId,omitempty"`
	ItemID   string `json:"itemId,omitempty"`
	RoomID   string `json:"roomId,omitempty"`
}

// PlaySource is the single normalized playback descriptor consumed by the mpv engine.
type PlaySource struct {
	Ref       ItemRef           `json:"ref"`
	Title     string            `json:"title"`
	URL       string            `json:"url"`
	Kind      StreamKind        `json:"kind"`
	Headers   map[string]string `json:"headers,omitempty"`
	Decrypt   *DecryptSpec      `json:"decrypt,omitempty"`
	Rewrite   bool              `json:"rewrite,omitempty"`
	Lines     []Line            `json:"lines,omitempty"`
	Subtitles []SubtitleTrack   `json:"subtitles,omitempty"`
	StartAt   time.Duration     `json:"startAt,omitempty"`
	ExpiresAt time.Time         `json:"expiresAt,omitempty"`
	Live      bool              `json:"live,omitempty"`
	Danmaku   *DanmakuRef       `json:"danmaku,omitempty"`
}

// Detail holds full series/movie metadata, seasons, and episodes.
type Detail struct {
	Item     Item     `json:"item"`
	Seasons  []Item   `json:"seasons,omitempty"`
	Episodes []Item   `json:"episodes,omitempty"`
	Actors   []string `json:"actors,omitempty"`
}

// Section represents a top-level browse tab or category within a provider.
type Section struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Query describes a paginated catalog request.
type Query struct {
	Section  string            `json:"section,omitempty"`
	Category string            `json:"category,omitempty"`
	Sort     string            `json:"sort,omitempty"`
	Cursor   string            `json:"cursor,omitempty"`
	Filters  map[string]string `json:"filters,omitempty"`
}

// Page is a paginated list response. Next == "" indicates the end of results.
type Page struct {
	Items []Item `json:"items"`
	Next  string `json:"next,omitempty"`
	Total int    `json:"total,omitempty"`
}

// PlayState reports playback lifecycle state for progress reporting.
type PlayState int

const (
	PlayStatePlaying PlayState = iota
	PlayStatePaused
	PlayStateStopped
)
