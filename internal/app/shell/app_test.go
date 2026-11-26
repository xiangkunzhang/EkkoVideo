package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/app/pages/list"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/logx"
)

func TestShellNavigationAndOnlineFlowHeadless(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	mediaDir := filepath.Join(dir, "movies")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "demo.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp("0.1.0-test", dir, cacheDir, logx.Open("-"))
	t.Cleanup(func() { a.shutdown() })

	a.history.Record(core.WatchRecord{
		Ref:      core.ItemRef{Source: "local", Kind: core.KindFile, ID: filepath.Join(mediaDir, "demo.mp4")},
		Title:    "demo.mp4",
		Position: 45,
		Duration: 300,
	})
	a.fileBrowser.CurrentDir = mediaDir
	a.fileBrowser.Items = []core.Item{{
		Ref:      core.ItemRef{Source: "local", Kind: core.KindFile, ID: filepath.Join(mediaDir, "demo.mp4")},
		Title:    "demo.mp4",
		Playable: true,
	}}

	// Pre-populate one online poster grid so no network is needed
	a.posterGrids["online:hongguo"] = &list.PosterGridState{
		SourceID: "online:hongguo",
		Title:    "红果短剧",
		Mode:     "recommend",
		Started:  true,
		Items: []core.Item{{
			Ref:      core.ItemRef{Source: "online:hongguo", Kind: core.KindSeries, ID: "s1"},
			Title:    "测试短剧第一部",
			Subtitle: "全 80 集",
			Children: 80,
		}},
	}

	tt := ui.NewTester(a.view, 1180, 740)
	if !tt.HasText("首页") || !tt.HasText("红果短剧") {
		t.Fatalf("home missing expected texts: %v", tt.Texts())
	}

	if err := tt.Click("本地视频"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(1e9)
	if a.router.Path() != "/local" || !tt.HasText("demo.mp4") {
		t.Fatalf("local browser path=%q texts=%v", a.router.Path(), tt.Texts())
	}

	if err := tt.Click("红果短剧"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(1e9)
	if a.router.Path() != "/online/hongguo" || !tt.HasText("测试短剧第一部") {
		t.Fatalf("online grid path=%q texts=%v", a.router.Path(), tt.Texts())
	}

	if err := tt.Click("设置与诊断"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(1e9)
	if a.router.Path() != "/settings" || !tt.HasText("EkkoGateway 子进程") {
		t.Fatalf("settings path=%q texts=%v", a.router.Path(), tt.Texts())
	}
}
