//go:build darwin

package mediakeys

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type Handlers struct {
	OnPlay   func()
	OnPause  func()
	OnToggle func()
	OnNext   func()
	OnPrev   func()
}

var (
	initOnce sync.Once
	handlers Handlers
	targetID objc.ID

	selSharedCmdCenter objc.SEL
	selPlayCmd         objc.SEL
	selPauseCmd        objc.SEL
	selToggleCmd       objc.SEL
	selNextCmd         objc.SEL
	selPrevCmd         objc.SEL
	selSetEnabled      objc.SEL
	selAddTargetAction objc.SEL

	selDefaultCenter   objc.SEL
	selSetPlaybackStat objc.SEL
	selSetNowPlaying   objc.SEL
	selDict            objc.SEL
	selSetObjForKey    objc.SEL
	selStrWithUTF8     objc.SEL
	selNumWithDouble   objc.SEL

	clsNowPlayingCenter objc.Class
	clsNSMutableDict    objc.Class
	clsNSString         objc.Class
	clsNSNumber         objc.Class
)

// Init registers macOS MPRemoteCommandCenter handlers so hardware Play/Pause/Next/Prev
// media keys and Control Center Now Playing work natively via purego.
func Init(h Handlers) {
	handlers = h
	initOnce.Do(func() {
		_, err := purego.Dlopen("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			return
		}
		clsRemoteCenter := objc.GetClass("MPRemoteCommandCenter")
		clsNowPlayingCenter = objc.GetClass("MPNowPlayingInfoCenter")
		clsNSMutableDict = objc.GetClass("NSMutableDictionary")
		clsNSString = objc.GetClass("NSString")
		clsNSNumber = objc.GetClass("NSNumber")
		if clsRemoteCenter == 0 || clsNowPlayingCenter == 0 {
			return
		}

		selSharedCmdCenter = objc.RegisterName("sharedCommandCenter")
		selPlayCmd = objc.RegisterName("playCommand")
		selPauseCmd = objc.RegisterName("pauseCommand")
		selToggleCmd = objc.RegisterName("togglePlayPauseCommand")
		selNextCmd = objc.RegisterName("nextTrackCommand")
		selPrevCmd = objc.RegisterName("previousTrackCommand")
		selSetEnabled = objc.RegisterName("setEnabled:")
		selAddTargetAction = objc.RegisterName("addTarget:action:")

		selDefaultCenter = objc.RegisterName("defaultCenter")
		selSetPlaybackStat = objc.RegisterName("setPlaybackState:")
		selSetNowPlaying = objc.RegisterName("setNowPlayingInfo:")
		selDict = objc.RegisterName("dictionary")
		selSetObjForKey = objc.RegisterName("setObject:forKey:")
		selStrWithUTF8 = objc.RegisterName("stringWithUTF8String:")
		selNumWithDouble = objc.RegisterName("numberWithDouble:")

		selOnPlay := objc.RegisterName("onPlay:")
		selOnPause := objc.RegisterName("onPause:")
		selOnToggle := objc.RegisterName("onTogglePlayPause:")
		selOnNext := objc.RegisterName("onNextTrack:")
		selOnPrev := objc.RegisterName("onPreviousTrack:")

		cls, err := objc.RegisterClass(
			"VelanRemoteTarget",
			objc.GetClass("NSObject"),
			nil,
			nil,
			[]objc.MethodDef{
				{
					Cmd: selOnPlay,
					Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) int64 {
						if handlers.OnPlay != nil {
							handlers.OnPlay()
						}
						return 0
					},
				},
				{
					Cmd: selOnPause,
					Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) int64 {
						if handlers.OnPause != nil {
							handlers.OnPause()
						}
						return 0
					},
				},
				{
					Cmd: selOnToggle,
					Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) int64 {
						if handlers.OnToggle != nil {
							handlers.OnToggle()
						}
						return 0
					},
				},
				{
					Cmd: selOnNext,
					Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) int64 {
						if handlers.OnNext != nil {
							handlers.OnNext()
						}
						return 0
					},
				},
				{
					Cmd: selOnPrev,
					Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) int64 {
						if handlers.OnPrev != nil {
							handlers.OnPrev()
						}
						return 0
					},
				},
			},
		)
		if err != nil {
			return
		}
		targetID = objc.ID(cls).Send(objc.RegisterName("new"))
		center := objc.ID(clsRemoteCenter).Send(selSharedCmdCenter)
		if center == 0 || targetID == 0 {
			return
		}

		bindCmd := func(cmdSel, actSel objc.SEL) {
			cmd := center.Send(cmdSel)
			if cmd != 0 {
				cmd.Send(selSetEnabled, true)
				cmd.Send(selAddTargetAction, targetID, actSel)
			}
		}
		bindCmd(selPlayCmd, selOnPlay)
		bindCmd(selPauseCmd, selOnPause)
		bindCmd(selToggleCmd, selOnToggle)
		bindCmd(selNextCmd, selOnNext)
		bindCmd(selPrevCmd, selOnPrev)
	})
}

func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	return objc.ID(clsNSString).Send(selStrWithUTF8, unsafe.Pointer(&b[0]))
}

func nsNumberDouble(v float64) objc.ID {
	return objc.ID(clsNSNumber).Send(selNumWithDouble, v)
}

// UpdateNowPlaying updates MPNowPlayingInfoCenter so macOS routes hardware media
// keys to Velan and displays current title/progress in Control Center.
func UpdateNowPlaying(title string, active, playing bool, elapsed, duration float64) {
	if clsNowPlayingCenter == 0 {
		return
	}
	center := objc.ID(clsNowPlayingCenter).Send(selDefaultCenter)
	if center == 0 {
		return
	}
	if !active {
		center.Send(selSetPlaybackStat, int64(0)) // MPNowPlayingPlaybackStateStopped
		center.Send(selSetNowPlaying, objc.ID(0))
		return
	}
	state := int64(2) // MPNowPlayingPlaybackStatePaused
	rate := 0.0
	if playing {
		state = 1 // MPNowPlayingPlaybackStatePlaying
		rate = 1.0
	}
	center.Send(selSetPlaybackStat, state)

	dict := objc.ID(clsNSMutableDict).Send(selDict)
	if title != "" {
		dict.Send(selSetObjForKey, nsString(title), nsString("title"))
	}
	if duration > 0 {
		dict.Send(selSetObjForKey, nsNumberDouble(duration), nsString("playbackDuration"))
	}
	if elapsed >= 0 {
		dict.Send(selSetObjForKey, nsNumberDouble(elapsed), nsString("MPNowPlayingInfoPropertyElapsedPlaybackTime"))
	}
	dict.Send(selSetObjForKey, nsNumberDouble(rate), nsString("MPNowPlayingInfoPropertyPlaybackRate"))
	center.Send(selSetNowPlaying, dict)
}
