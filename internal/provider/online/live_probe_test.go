package online

import (
	"os"
	"testing"

	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

func TestProbeLiveHongguo(t *testing.T) {
	if os.Getenv("EKKO_LIVE") != "1" {
		t.Skip("set EKKO_LIVE=1 to run")
	}
	m := NewManager("")
	page, err := m.Browse("online:hongguo", core.Query{Section: "recommend"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("empty browse")
	}
	first := page.Items[0]
	t.Logf("First: id=%s title=%q", first.Ref.ID, first.Title)

	det, err := m.Detail("online:hongguo", first.Ref.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	t.Logf("Detail eps=%d", len(det.Episodes))

	rel, err := m.Related("online:hongguo", first.Ref.ID, det.Item.Title)
	t.Logf("Related len=%d err=%v", len(rel), err)

	if len(det.Episodes) > 0 {
		dm, err := m.Danmaku("online:hongguo", first.Ref.ID, det.Episodes[0].Ref.ID)
		t.Logf("Danmaku ep1 len=%d err=%v", len(dm), err)
	}
}
