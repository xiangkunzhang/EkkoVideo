package hongguo

// UserRelationInfo carries social engagement metrics for an actor.
type UserRelationInfo struct {
	CanFollow     bool   `json:"can_follow"`
	FansNum       int64  `json:"fans_num"`
	FollowUserNum int64  `json:"follow_user_num"`
	DiggedCount   string `json:"digged_count"`
}

// UserInfo is an actor or creator profile returned by the search API.
type UserInfo struct {
	UserID           string           `json:"user_id"`
	UserName         string           `json:"user_name"`
	UserAvatar       string           `json:"user_avatar"`
	Description      string           `json:"description"`
	SubtitleList     []string         `json:"subtitle_list"`
	UserRelationInfo UserRelationInfo `json:"user_relation_info"`
}

// Celebrity is a cast member entry in video_data or SSR detail.
type Celebrity struct {
	CelebrityID string `json:"celebrity_id"`
	Nickname    string `json:"nickname"`
	Avatar      string `json:"avatar"`
	SubTitle    string `json:"sub_title"`
	IsLabel     bool   `json:"is_label"`
}

// SearchItem is one entry of GET /api/search/result.
type SearchItem struct {
	DocType   int         `json:"doc_type"`
	Name      string      `json:"name"`
	Keyword   string      `json:"keyword"`
	UserInfo  *UserInfo   `json:"user_info"`
	VideoList []VideoData `json:"video_list"`
	VideoData VideoData   `json:"video_data"`
}

// VideoData carries the series metadata returned by the search API.
type VideoData struct {
	SeriesID         string   `json:"series_id"`
	EpisodeCnt       int      `json:"episode_cnt"`
	CreateTime       string   `json:"create_time"`
	SeriesStatus     int      `json:"series_status"`
	PayType          int      `json:"pay_type"`
	SeriesTitle      string   `json:"series_title"`
	SeriesIntro      string   `json:"series_intro"`
	SeriesCover      string   `json:"series_cover"`
	EpisodeRightText string   `json:"episode_right_text"`
	VidList          []string `json:"vid_list"`
	HotScoreData     struct {
		Score int64  `json:"score"`
		Text  string `json:"text"`
	} `json:"hot_score_data"`
	CategoryList []struct {
		CategoryID string `json:"category_id"`
		Name       string `json:"name"`
	} `json:"category_list"`
	Celebrities  []Celebrity `json:"celebrities"`
	Genre        string      `json:"genre"`
	FavoriteText string      `json:"favorite_text"`
	LikeText     string      `json:"like_text"`
}

// Detail is the series object embedded in the player page's SSR payload.
// It is the richest free source of episode information: it carries the
// complete vid_list (all episodes, not just the free ones).
type Detail struct {
	EpisodeCnt           int         `json:"episode_cnt"`
	AccessibleEpisodeCnt int         `json:"accessible_episode_cnt"`
	PayType              int         `json:"pay_type"`
	SeriesID             string      `json:"series_id"`
	SeriesName           string      `json:"series_name"`
	SeriesCover          string      `json:"series_cover"`
	SeriesIntro          string      `json:"series_intro"`
	Tags                 []string    `json:"tags"`
	EpisodeRightText     string      `json:"episode_right_text"`
	Celebrities          []Celebrity `json:"celebrities"`
	SeriesEpisodeInfo    struct {
		EpisodeCnt      int `json:"episode_cnt"`
		EpisodeTotalCnt int `json:"episode_total_cnt"`
		SeriesStatus    int `json:"series_status"`
	} `json:"series_episode_info"`
	VidList          []string `json:"vid_list"`
	FirstVisibleTime string   `json:"first_visible_time"`
	SeasonInfo       struct {
		SeriesGroupID string `json:"series_group_id"`
	} `json:"season_info"`
	RecordNumber string `json:"record_number"`
}

// PlayInfo is the response of POST /api/video/protected/play-info.
//
// The BFF answers in one of two shapes:
//
//	legacy_play_info — a single progressive MP4 URL, directly playable.
//	play_info        — a DASH manifest split into an audio and a video track,
//	                   CENC encrypted; the decryption key travels in
//	                   player_access_key.
type PlayInfo struct {
	LegacyPlayInfo struct {
		MainURL   string  `json:"main_url"`
		BackupURL string  `json:"backup_url"`
		Width     string  `json:"width"`
		Height    string  `json:"height"`
		Duration  float64 `json:"duration"`
	} `json:"legacy_play_info"`
	PlayInfo *DashInfo `json:"play_info"`
	BaseResp struct {
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	} `json:"base_resp"`
	Error string `json:"error"`
}

// DashInfo is the fragmented-MP4 description of one episode.
type DashInfo struct {
	VID             string     `json:"vid"`
	KID             string     `json:"kid"`
	MainURL         string     `json:"main_url"`
	BackupURL       string     `json:"backup_url"`
	PlayerAccessKey string     `json:"player_access_key"`
	Definition      string     `json:"definition"`
	Format          string     `json:"format"`
	CodecType       string     `json:"codec_type"`
	Width           string     `json:"width"`
	Height          string     `json:"height"`
	Duration        float64    `json:"duration"`
	Size            string     `json:"size"`
	Bitrate         string     `json:"bitrate"`
	InitRange       string     `json:"init_range"`
	IndexRange      string     `json:"index_range"`
	FirstMoofRange  string     `json:"first_moof_range"`
	ExpiresAt       string     `json:"expires_at"`
	Audio           *DashTrack `json:"audio"`
	Video           *DashTrack `json:"video"`
}

// DashTrack is one elementary stream of a DASH episode.
type DashTrack struct {
	MainURL         string  `json:"main_url"`
	BackupURL       string  `json:"backup_url"`
	PlayerAccessKey string  `json:"player_access_key"`
	CodecType       string  `json:"codec_type"`
	Format          string  `json:"format"`
	Duration        float64 `json:"duration"`
	Size            string  `json:"size"`
	Bitrate         string  `json:"bitrate"`
	InitRange       string  `json:"init_range"`
	IndexRange      string  `json:"index_range"`
	KID             string  `json:"kid"`
	ExpiresAt       string  `json:"expires_at"`
}

// FallbackPlayInfo is the response of POST /api/video/protected/fallback-play-info.
type FallbackPlayInfo struct {
	MainURL   string `json:"main_url"`
	URLExpire int64  `json:"url_expire"`
	BaseResp  struct {
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	} `json:"base_resp"`
	Error string `json:"error"`
}
