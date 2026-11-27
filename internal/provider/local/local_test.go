package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiangkunzhang/Velan/internal/core"
)

func TestNaturalSortAndSubtitles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"ep10.mp4", "ep2.mp4", "ep1.mp4", "ep2.srt", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	subDir := filepath.Join(dir, "Subs")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "chs.ass"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New()
	page, err := p.List(context.Background(), core.ItemRef{Source: SourceID, Kind: core.KindFolder, ID: dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Expect ep1.mp4, ep2.mp4, ep10.mp4 (Subs folder is a dir, comes first)
	var videos []string
	for _, it := range page.Items {
		if it.Ref.Kind == core.KindFile {
			videos = append(videos, it.Title)
		}
	}
	if len(videos) != 3 || videos[0] != "ep1.mp4" || videos[1] != "ep2.mp4" || videos[2] != "ep10.mp4" {
		t.Fatalf("got natural order %v", videos)
	}

	src, err := p.Resolve(context.Background(), core.ItemRef{Source: SourceID, Kind: core.KindFile, ID: filepath.Join(dir, "ep2.mp4")}, core.ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Subtitles) != 2 {
		t.Fatalf("got %d subtitles, want 2: %+v", len(src.Subtitles), src.Subtitles)
	}

	q := p.BuildQueue(filepath.Join(dir, "ep2.mp4"))
	if q.Current != 1 || len(q.Items) != 3 {
		t.Fatalf("queue current=%d len=%d", q.Current, len(q.Items))
	}
}
