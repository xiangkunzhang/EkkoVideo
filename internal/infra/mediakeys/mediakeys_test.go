package mediakeys

import "testing"

func TestInitAndUpdateNowPlayingDoNotPanic(t *testing.T) {
	var called string
	Init(Handlers{
		OnPlay:   func() { called = "play" },
		OnPause:  func() { called = "pause" },
		OnToggle: func() { called = "toggle" },
		OnNext:   func() { called = "next" },
		OnPrev:   func() { called = "prev" },
	})
	UpdateNowPlaying("Velan Test", true, true, 12.5, 120.0)
	UpdateNowPlaying("", false, false, 0, 0)

	if called != "" {
		t.Fatalf("unexpected callback invocation: %q", called)
	}
}

func TestHandlersCallbacks(t *testing.T) {
	var events []string
	h := Handlers{
		OnPlay:   func() { events = append(events, "play") },
		OnPause:  func() { events = append(events, "pause") },
		OnToggle: func() { events = append(events, "toggle") },
		OnNext:   func() { events = append(events, "next") },
		OnPrev:   func() { events = append(events, "prev") },
	}
	h.OnPlay()
	h.OnPause()
	h.OnToggle()
	h.OnNext()
	h.OnPrev()

	want := []string{"play", "pause", "toggle", "next", "prev"}
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d", len(events), len(want))
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q", i, events[i], want[i])
		}
	}
}
