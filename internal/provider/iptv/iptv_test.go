package iptv

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiangkunzhang/Velan/internal/core"
)

func TestParseM3UMergeLinesAndHeaders(t *testing.T) {
	sample := `#EXTM3U x-tvg-url="https://example.com/epg.xml.gz"
#EXTINF:-1 tvg-id="cctv1" tvg-logo="http://logo/1.png" group-title="央视",CCTV-1 综合
#EXTVLCOPT:http-user-agent=CustomAgent/1.0
http://stream1/cctv1.m3u8
#EXTINF:-1 tvg-id="cctv1" group-title="央视",CCTV-1 综合
http://stream2/cctv1.m3u8
#EXTINF:-1 tvg-id="hunan" group-title="卫视",湖南卫视
http://stream1/hunan.m3u8
`
	pl, err := ParseM3U(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if pl.EPGURL != "https://example.com/epg.xml.gz" {
		t.Fatalf("epg url=%q", pl.EPGURL)
	}
	if len(pl.Channels) != 2 {
		t.Fatalf("got %d channels, want 2", len(pl.Channels))
	}
	c1 := pl.Channels[0]
	if len(c1.Lines) != 2 {
		t.Fatalf("cctv1 lines=%d, want 2", len(c1.Lines))
	}
	if c1.Lines[0].Headers["User-Agent"] != "CustomAgent/1.0" {
		t.Fatalf("missing EXTVLCOPT UA: %+v", c1.Lines[0].Headers)
	}
}

func TestParseXMLTVAndGzip(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 26, 15, 0, 0, 0, cst)
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="cctv1"><display-name>CCTV-1 综合</display-name></channel>
  <programme start="20261126143000 +0800" stop="20261126153000 +0800" channel="cctv1">
    <title>新闻联播</title>
  </programme>
  <programme start="20261126153000 +0800" stop="20261126163000 +0800" channel="cctv1">
    <title>焦点访谈</title>
  </programme>
</tv>`

	var gzBuf bytes.Buffer
	zw := gzip.NewWriter(&gzBuf)
	_, _ = zw.Write([]byte(xmlBody))
	_ = zw.Close()

	store, err := ParseXMLTV(gzBuf.Bytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	cur, next := store.NowAndNext("cctv1", "CCTV-1 综合", now)
	if cur == nil || cur.Title != "新闻联播" {
		t.Fatalf("cur=%+v", cur)
	}
	if next == nil || next.Title != "焦点访谈" {
		t.Fatalf("next=%+v", next)
	}
}

func TestManagerResolveAndRememberLine(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	m.mu.Lock()
	m.channels = []Channel{{
		ID:     "ch1",
		Number: 1,
		Name:   "CCTV-1",
		Group:  "央视",
		Lines: []core.Line{
			{ID: "line-1", Label: "线路 1", URL: "http://a.m3u8"},
			{ID: "line-2", Label: "线路 2", URL: "http://b.m3u8"},
		},
	}}
	m.mu.Unlock()

	m.RememberWorkingLine("ch1", "line-2")
	src, err := m.Resolve(context.Background(), core.ItemRef{Source: SourceID, Kind: core.KindChannel, ID: "ch1"}, core.ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if src.URL != "http://b.m3u8" || len(src.Lines) != 2 {
		t.Fatalf("expected remembered line-2 first, got %+v", src)
	}
}
