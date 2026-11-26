package danmaku

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

func TestNormalizeAndDedup(t *testing.T) {
	got := NormalizeText("  [笑哭]\u200b 前方高能  ")
	if got != "😂 前方高能" {
		t.Fatalf("got %q", got)
	}

	in := []core.Comment{
		{TimeSec: 1.0, Text: "+1"},
		{TimeSec: 5.0, Text: "+1"},    // dup within 20s -> dropped
		{TimeSec: 10.0, Text: "剧透警告"}, // blocked keyword
		{TimeSec: 25.0, Text: "+1"},   // >20s -> kept
	}
	out := CleanAndFilter(in, []string{"剧透"}, 20)
	if len(out) != 2 || out[0].TimeSec != 1.0 || out[1].TimeSec != 25.0 {
		t.Fatalf("got %+v", out)
	}
}

func TestParseBilibiliXMLAndASS(t *testing.T) {
	xmlData := []byte(`<i><d p="12.5,1,25,16776960,0,0,0,0">第一条</d><d p="3.0,1,25,16777215,0,0,0,0">更早一条</d></i>`)
	items, err := ParseBilibiliXML(xmlData)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Text != "更早一条" || items[1].Color != 16776960 {
		t.Fatalf("xml parsed: %+v", items)
	}

	assData := []byte("[Events]\nDialogue: 0,0:01:02.50,0:01:08.00,Default,,0,0,0,,{\\move(10,20,30,40)}你好世界\\N第二行\n")
	assItems := ParseASS(assData)
	if len(assItems) != 1 || assItems[0].TimeSec != 62.5 || assItems[0].Text != "你好世界 第二行" {
		t.Fatalf("ass parsed: %+v", assItems)
	}
}

func TestSchedulerLanesAndSeek(t *testing.T) {
	s := NewScheduler()
	s.Load([]core.Comment{
		{TimeSec: 1.0, Text: "弹幕A"},
		{TimeSec: 1.1, Text: "弹幕B"},
		{TimeSec: 5.0, Text: "弹幕C"},
	}, nil)

	cfg := DefaultConfig()
	s.Step(1.2, 1280, 720, cfg, nil)
	if len(s.Active) != 2 {
		t.Fatalf("expected 2 active items, got %d", len(s.Active))
	}
	if s.Active[0].Lane == s.Active[1].Lane {
		t.Fatalf("overlapping lanes: %d == %d", s.Active[0].Lane, s.Active[1].Lane)
	}

	// Seeking forward clears active items and jumps cursor.
	s.Step(5.1, 1280, 720, cfg, nil)
	if len(s.Active) != 1 || s.Active[0].Text != "弹幕C" {
		t.Fatalf("after seek got %+v", s.Active)
	}
}

func TestVideoContentRectPortrait(t *testing.T) {
	r := VideoContentRect(ui.Rect{X: 0, Y: 0, W: 1280, H: 720}, 720, 1280, "auto")
	if r.W >= 410 || r.X <= 400 || r.H != 720 {
		t.Fatalf("unexpected portrait rect: %+v", r)
	}
}
