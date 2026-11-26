package model

// DramaCard represents a unified drama item across all 5 platforms.
type DramaCard struct {
	Source     string   `json:"source"`
	SeriesID   string   `json:"seriesId"`
	Title      string   `json:"title"`
	Cover      string   `json:"cover"`
	Intro      string   `json:"intro"`
	EpisodeCnt int      `json:"episodeCnt"`
	RightText  string   `json:"rightText"`
	Metric     string   `json:"metric"`
	Categories []string `json:"categories"`
	Rank       int      `json:"rank,omitempty"`
}

// Episode represents a single playable episode.
type Episode struct {
	Index int    `json:"index"`
	VID   string `json:"vid"`
	Title string `json:"title,omitempty"`
	Free  bool   `json:"free"`
}

// DramaDetail represents full drama metadata, cast, and episode list.
type DramaDetail struct {
	Source       string    `json:"source"`
	SeriesID     string    `json:"seriesId"`
	Title        string    `json:"title"`
	Cover        string    `json:"cover"`
	Intro        string    `json:"intro"`
	Tags         []string  `json:"tags"`
	Actors       []string  `json:"actors"`
	RecordNo     string    `json:"recordNo"`
	EpisodeCnt   int       `json:"episodeCnt"`
	Accessible   int       `json:"accessible"`
	CommentCount int       `json:"commentCount"`
	Episodes     []Episode `json:"episodes"`
}

// CategoryOption represents a selectable filter/category/rank chip.
type CategoryOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PlatformModes describes supported browse modes & category chips for a platform.
type PlatformModes struct {
	Source     string           `json:"source"`
	Modes      []CategoryOption `json:"modes"`      // recommend, rank, category
	RankBoards []CategoryOption `json:"rankBoards"` // e.g. hot-drama, hot-real-drama, ...
	Categories []CategoryOption `json:"categories"` // category chips
	Sorts      []CategoryOption `json:"sorts"`      // sort options if applicable
	Supports   struct {
		Danmaku  bool `json:"danmaku"`
		Comments bool `json:"comments"`
		Actors   bool `json:"actors"`
	} `json:"supports"`
}

// ActorProfile describes an actor/celebrity profile returned from search.
type ActorProfile struct {
	Name        string `json:"name"`
	Avatar      string `json:"avatar,omitempty"`
	Description string `json:"description,omitempty"`
	Fans        string `json:"fans,omitempty"`
	Role        string `json:"role,omitempty"`
}

// BrowseResult is the unified paginated list response.
type BrowseResult struct {
	Items      []DramaCard   `json:"items"`
	Page       int           `json:"page"`
	TotalPages int           `json:"totalPages"`
	Total      int           `json:"total"`
	HasMore    bool          `json:"hasMore"`
	NextCursor string        `json:"nextCursor,omitempty"`
	Actor      *ActorProfile `json:"actor,omitempty"`
}

// RelatedWork represents a sequel/season or similar recommended drama for a given series.
type RelatedWork struct {
	DramaCard
	Season      string `json:"season,omitempty"`
	SeasonIndex int    `json:"seasonIndex,omitempty"`
	Upcoming    bool   `json:"upcoming,omitempty"`
	SameSeries  bool   `json:"sameSeries,omitempty"`
}

// CommentItem represents a comment and optional replies.
type CommentItem struct {
	ID       string        `json:"id"`
	Author   string        `json:"author"`
	Avatar   string        `json:"avatar,omitempty"`
	Content  string        `json:"content"`
	Likes    int           `json:"likes"`
	TimeText string        `json:"timeText,omitempty"`
	Replies  []CommentItem `json:"replies,omitempty"`
}

// DanmakuItem represents a timed comment for canvas danmaku rendering.
type DanmakuItem struct {
	TimeSec float64 `json:"timeSec"`
	Text    string  `json:"text"`
	Color   string  `json:"color,omitempty"`
}

// Settings holds persisted player & UI preferences in ./data/config/settings.json.
type Settings struct {
	PlaybackRate    float64           `json:"playbackRate"`
	AutoNext        bool              `json:"autoNext"`
	PrefetchNext    bool              `json:"prefetchNext"`
	DanmakuEnabled  bool              `json:"danmakuEnabled"`
	DanmakuOpacity  float64           `json:"danmakuOpacity"`
	DanmakuFontSize int               `json:"danmakuFontSize"`
	DanmakuSpeed    float64           `json:"danmakuSpeed"`
	DanmakuArea     string            `json:"danmakuArea"` // quarter | half | full
	DanmakuNoSub    bool              `json:"danmakuNoSub"`
	PlayerMode      string            `json:"playerMode"` // web | mpv
	MPVPath         string            `json:"mpvPath,omitempty"`
	Volume          float64           `json:"volume"`
	Muted           bool              `json:"muted"`
	Shortcuts       map[string]string `json:"shortcuts,omitempty"`
}

// DefaultShortcuts returns default keyboard bindings for the player.
func DefaultShortcuts() map[string]string {
	return map[string]string{
		"playPause":  "Space",
		"stop":       "x",
		"prevEp":     "[",
		"nextEp":     "]",
		"mute":       "m",
		"volUp":      "ArrowUp",
		"volDown":    "ArrowDown",
		"danmaku":    "d",
		"fullscreen": "f",
		"pin":        "t",
		"mini":       "w",
	}
}

// DefaultSettings returns sensible initial settings.
func DefaultSettings() Settings {
	return Settings{
		PlaybackRate:    1.0,
		AutoNext:        true,
		PrefetchNext:    true,
		DanmakuEnabled:  true,
		DanmakuOpacity:  0.85,
		DanmakuFontSize: 20,
		DanmakuSpeed:    1.0,
		DanmakuArea:     "half",
		DanmakuNoSub:    true,
		PlayerMode:      "web",
		Volume:          1.0,
		Shortcuts:       DefaultShortcuts(),
	}
}
