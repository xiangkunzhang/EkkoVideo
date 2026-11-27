package player

import (
	"testing"
	"time"

	"github.com/xiangkunzhang/Velan/internal/core"
	"github.com/xiangkunzhang/Velan/internal/engine/mpv"
)

func TestPlayerViewStateTouchAndOSD(t *testing.T) {
	st := &PlayerViewState{}
	now := time.Unix(1000, 0)

	st.Touch(now)
	if !st.LastActivity.Equal(now) {
		t.Fatalf("LastActivity = %v, want %v", st.LastActivity, now)
	}

	st.ShowOSD(now, "已静音")
	if st.OSDMessage != "已静音" {
		t.Fatalf("OSDMessage = %q, want %q", st.OSDMessage, "已静音")
	}
	if !st.OSDUntil.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("OSDUntil = %v, want %v", st.OSDUntil, now.Add(2*time.Second))
	}
}

func TestPlayerViewStateChromeVisible(t *testing.T) {
	base := time.Unix(2000, 0)

	tests := []struct {
		name string
		st   PlayerViewState
		now  time.Time
		want bool
	}{
		{
			name: "zero activity stays visible",
			st:   PlayerViewState{},
			now:  base,
			want: true,
		},
		{
			name: "within hide delay stays visible",
			st:   PlayerViewState{LastActivity: base},
			now:  base.Add(controlsHideDelay - time.Millisecond),
			want: true,
		},
		{
			name: "past hide delay hides chrome",
			st:   PlayerViewState{LastActivity: base},
			now:  base.Add(controlsHideDelay),
			want: false,
		},
		{
			name: "paused keeps chrome visible",
			st:   PlayerViewState{LastActivity: base, Snap: mpv.Snapshot{Paused: true}},
			now:  base.Add(10 * time.Second),
			want: true,
		},
		{
			name: "drawer open keeps chrome visible",
			st:   PlayerViewState{LastActivity: base, DrawerOpen: true},
			now:  base.Add(10 * time.Second),
			want: true,
		},
		{
			name: "active menu keeps chrome visible",
			st:   PlayerViewState{LastActivity: base, ActiveMenu: "sub"},
			now:  base.Add(10 * time.Second),
			want: true,
		},
		{
			name: "seeking keeps chrome visible",
			st:   PlayerViewState{LastActivity: base, Seeking: true},
			now:  base.Add(10 * time.Second),
			want: true,
		},
		{
			name: "recovering phase keeps chrome visible",
			st: PlayerViewState{
				LastActivity: base,
				Session:      &core.PlaybackSession{Phase: core.PhaseRecovering},
			},
			now:  base.Add(10 * time.Second),
			want: true,
		},
		{
			name: "playing phase past delay hides chrome",
			st: PlayerViewState{
				LastActivity: base,
				Session:      &core.PlaybackSession{Phase: core.PhasePlaying},
			},
			now:  base.Add(10 * time.Second),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.ChromeVisible(tc.now); got != tc.want {
				t.Fatalf("ChromeVisible() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterAndFormatTracks(t *testing.T) {
	tracks := []mpv.Track{
		{ID: 1, Type: "video", Title: "HEVC"},
		{ID: 1, Type: "audio", Title: "Japanese", Lang: "jpn", Selected: true},
		{ID: 2, Type: "audio", Lang: "eng"},
		{ID: 1, Type: "sub", Title: "简体中文", Lang: "chi", External: true, Selected: true},
		{ID: 2, Type: "sub"},
	}

	audio := filterTracks(tracks, "audio")
	if len(audio) != 2 {
		t.Fatalf("audio count = %d, want 2", len(audio))
	}
	subs := filterTracks(tracks, "sub")
	if len(subs) != 2 {
		t.Fatalf("sub count = %d, want 2", len(subs))
	}

	if got := formatTrackLabel(audio[0]); got != "Japanese [jpn]" {
		t.Fatalf("formatTrackLabel(audio[0]) = %q", got)
	}
	if got := formatTrackLabel(audio[1]); got != "轨道 #2 (eng)" {
		t.Fatalf("formatTrackLabel(audio[1]) = %q", got)
	}
	if got := formatTrackLabel(subs[0]); got != "简体中文 [chi] · 外挂" {
		t.Fatalf("formatTrackLabel(subs[0]) = %q", got)
	}
	if got := formatTrackLabel(subs[1]); got != "轨道 #2" {
		t.Fatalf("formatTrackLabel(subs[1]) = %q", got)
	}
}

func TestProfileIDs(t *testing.T) {
	if got := NewVODSingleProfile().ID(); got != "vod_single" {
		t.Fatalf("VODSingleProfile.ID() = %q", got)
	}
	if got := NewLiveRoomProfile().ID(); got != "live_room" {
		t.Fatalf("LiveRoomProfile.ID() = %q", got)
	}
}
