package live

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/xiangkunzhang/Velan/internal/core"
)

func TestManagerPlatforms(t *testing.T) {
	m := NewManager()
	plats := m.Platforms()
	if len(plats) != 3 {
		t.Fatalf("expected 3 live platforms, got %d", len(plats))
	}
	if plats[0].ID != "bilibili" || plats[1].ID != "huya" || plats[2].ID != "douyu" {
		t.Fatalf("unexpected platform order: %+v", plats)
	}
}

func TestStripHTMLTagsAndFormatOnline(t *testing.T) {
	if got := stripHTMLTags(`<em class="keyword">英雄</em>联盟`); got != "英雄联盟" {
		t.Fatalf("stripHTMLTags=%q", got)
	}
	if got := formatOnline(25600); got != "2.6万" {
		t.Fatalf("formatOnline=%q", got)
	}
	if got := formatOnline(512); got != "512" {
		t.Fatalf("formatOnline=%q", got)
	}
}

func TestLiveProbeReal(t *testing.T) {
	if os.Getenv("VELAN_LIVE_PROBE") != "1" {
		t.Skip("set VELAN_LIVE_PROBE=1 to probe real Douyu/Huya/Bilibili endpoints")
	}
	m := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, plat := range []string{"bilibili", "huya", "douyu"} {
		rooms, _, err := m.Rooms(ctx, plat, "", 1)
		if err != nil {
			t.Fatalf("%s Rooms err: %v", plat, err)
		}
		if len(rooms) == 0 {
			t.Fatalf("%s returned 0 rooms", plat)
		}
		src, err := m.ResolveRoom(ctx, core.ItemRef{Source: SourcePrefix + plat, Kind: core.KindLiveRoom, ID: rooms[0].Ref.ID})
		if err != nil {
			t.Fatalf("%s ResolveRoom(%s) err: %v", plat, rooms[0].Ref.ID, err)
		}
		if src.URL == "" || len(src.Lines) == 0 {
			t.Fatalf("%s empty stream: %+v", plat, src)
		}
		t.Logf("%s OK: room=%s title=%q lines=%d url=%.60s...", plat, rooms[0].Ref.ID, src.Title, len(src.Lines), src.URL)
	}
}

func TestWSLiveDanmakuProbe(t *testing.T) {
	if os.Getenv("VELAN_LIVE_PROBE") != "1" {
		t.Skip("set VELAN_LIVE_PROBE=1")
	}
	m := NewManager()
	for _, plat := range []string{"bilibili", "douyu", "huya"} {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		rooms, _, err := m.Rooms(ctx, plat, "", 1)
		if err != nil || len(rooms) == 0 {
			cancel()
			t.Logf("%s Rooms err: %v", plat, err)
			continue
		}
		ch := make(chan ChatMessage, 32)
		errCh := make(chan error, 1)
		go func() {
			errCh <- m.StreamDanmaku(ctx, rooms[0].Ref, ch)
		}()
		var got int
		var sample string
		for got == 0 {
			select {
			case msg := <-ch:
				got++
				sample = msg.Author + ": " + msg.Text
			case werr := <-errCh:
				t.Logf("%s WS exited: %v", plat, werr)
				goto donePlat
			case <-ctx.Done():
				goto donePlat
			}
		}
	donePlat:
		cancel()
		t.Logf("%s WS danmaku got=%d sample=%q (room=%s)", plat, got, sample, rooms[0].Ref.ID)
	}
}

func TestPickPreferredLine(t *testing.T) {
	lines := []core.Line{
		{ID: "0", Label: "原画 · 线路1"},
		{ID: "1", Label: "蓝光 · 线路2"},
		{ID: "2", Label: "高清 · 线路1"},
	}
	if got := PickPreferredLine(lines, "蓝光 · 线路2"); got != 1 {
		t.Fatalf("exact match = %d, want 1", got)
	}
	if got := PickPreferredLine(lines, "高清 · 线路9"); got != 2 {
		t.Fatalf("prefix match = %d, want 2", got)
	}
	if got := PickPreferredLine(lines, "超清"); got != 0 {
		t.Fatalf("fallback = %d, want 0", got)
	}
	if got := PickPreferredLine(nil, "原画"); got != 0 {
		t.Fatalf("empty lines = %d, want 0", got)
	}
}
