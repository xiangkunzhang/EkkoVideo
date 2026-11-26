package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/infra/logx"
)

func TestShellNavigationHeadless(t *testing.T) {
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, "movies")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "demo.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp("0.1.0-test", dir, logx.Open("-"))
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

	tt := ui.NewTester(a.view, 1180, 740)
	if !tt.HasText("首页") || !tt.HasText("demo.mp4") {
		t.Fatalf("home missing expected texts: %v", tt.Texts())
	}

	if err := tt.Click("本地视频"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(1e9)
	if a.router.Path() != "/local" || !tt.HasText("demo.mp4") {
		t.Fatalf("local browser path=%q texts=%v", a.router.Path(), tt.Texts())
	}

	if err := tt.Click("设置与诊断"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(1e9)
	if a.router.Path() != "/settings" || !tt.HasText("EkkoGateway 子进程") {
		t.Fatalf("settings path=%q texts=%v", a.router.Path(), tt.Texts())
	}
}
