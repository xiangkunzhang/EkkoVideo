package core

// KeymapScope distinguishes shortcut bindings across player profiles.
type KeymapScope string

const (
	ScopeVOD      KeymapScope = "vod"
	ScopeIPTV     KeymapScope = "iptv"
	ScopeLiveRoom KeymapScope = "live"
)

// Action names a logical player command.
type Action string

const (
	ActionPlayPause   Action = "playPause"
	ActionSeekBack5   Action = "seekBack5"
	ActionSeekFwd5    Action = "seekFwd5"
	ActionSeekBack30  Action = "seekBack30"
	ActionSeekFwd30   Action = "seekFwd30"
	ActionVolUp       Action = "volUp"
	ActionVolDown     Action = "volDown"
	ActionMute        Action = "mute"
	ActionFullscreen  Action = "fullscreen"
	ActionSpeedDown   Action = "speedDown"
	ActionSpeedUp     Action = "speedUp"
	ActionSpeedReset  Action = "speedReset"
	ActionPrevItem    Action = "prevItem"
	ActionNextItem    Action = "nextItem"
	ActionCycleSub    Action = "cycleSub"
	ActionCycleAudio  Action = "cycleAudio"
	ActionScreenshot  Action = "screenshot"
	ActionAlwaysOnTop Action = "alwaysOnTop"
	ActionToggleList  Action = "toggleList"
)

func DefaultShortcuts() map[string]string {
	return map[string]string{
		string(ActionPlayPause):   "Space",
		string(ActionSeekBack5):   "Left",
		string(ActionSeekFwd5):    "Right",
		string(ActionSeekBack30):  "Shift+Left",
		string(ActionSeekFwd30):   "Shift+Right",
		string(ActionVolUp):       "Up",
		string(ActionVolDown):     "Down",
		string(ActionMute):        "M",
		string(ActionFullscreen):  "F",
		string(ActionSpeedDown):   "[",
		string(ActionSpeedUp):     "]",
		string(ActionSpeedReset):  `\`,
		string(ActionPrevItem):    "PageUp",
		string(ActionNextItem):    "PageDown",
		string(ActionCycleSub):    "V",
		string(ActionCycleAudio):  "A",
		string(ActionScreenshot):  "S",
		string(ActionAlwaysOnTop): "T",
		string(ActionToggleList):  "L",
	}
}
