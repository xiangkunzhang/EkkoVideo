package core

// Settings holds persisted application preferences.
type Settings struct {
	Volume        float64           `json:"volume"`
	Muted         bool              `json:"muted"`
	PlaybackSpeed float64           `json:"playbackSpeed"`
	AutoNext      bool              `json:"autoNext"`
	AspectRatio   string            `json:"aspectRatio"` // auto | 16:9 | 4:3 | fill
	Hwdec         string            `json:"hwdec"`       // auto-safe | no
	LastFolder    string            `json:"lastFolder,omitempty"`
	Shortcuts     map[string]string `json:"shortcuts,omitempty"`

	DanmakuEnabled  bool     `json:"danmakuEnabled"`
	DanmakuFontSize float64  `json:"danmakuFontSize"`
	DanmakuOpacity  float64  `json:"danmakuOpacity"`
	DanmakuSpeed    float64  `json:"danmakuSpeed"`
	DanmakuArea     float64  `json:"danmakuArea"`
	DanmakuKeywords []string `json:"danmakuKeywords,omitempty"`

	ShowExtraOnlinePlatforms bool `json:"showExtraOnlinePlatforms,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		Volume:          80,
		Muted:           false,
		PlaybackSpeed:   1.0,
		AutoNext:        true,
		AspectRatio:     "auto",
		Hwdec:           "auto-safe",
		Shortcuts:       DefaultShortcuts(),
		DanmakuEnabled:  true,
		DanmakuFontSize: 20,
		DanmakuOpacity:  0.88,
		DanmakuSpeed:    1.0,
		DanmakuArea:     0.50,
	}
}
