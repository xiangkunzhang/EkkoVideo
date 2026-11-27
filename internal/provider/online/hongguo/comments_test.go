package hongguo

import (
	"net/url"
	"strings"
	"testing"

	"github.com/xiangkunzhang/Velan/internal/provider/online/upstream"
)

// samplePlayerHTML mirrors the shape of the public h5 player page: the router
// payload is embedded as JSON and the review counters arrive as strings.
const samplePlayerHTML = `<!doctype html><html><body><script>
window.__DATA__ = {"loaderData":{"player_layout":{},"player_(series_id)/(vid)/page":{
  "isSuccess":true,
  "series_id":"7704532495357725721",
  "vid":"7704532501426867224",
  "seriesDetail":{"series_id":"7704532495357725721","series_name":"持械入宋：第十一季"},
  "episodeSocialInfo":{
    "episode_like_count":"77728",
    "reviews":{"total_count":"15285","reviews":[
      {"create_time":"1794676750","digg_count":"331","user_name":"倾城契约孟小姐","user_avatar":"https://p3-novel.byteimg.com/a.png","content":"终于等到了[飞吻]"},
      {"create_time":"1794702141","digg_count":"203","user_name":"旁观者8385","user_avatar":"http://insecure.example/b.png","content":"终于上了哈哈哈哈"},
      {"create_time":"1794676750","digg_count":"331","user_name":"倾城契约孟小姐","user_avatar":"https://p3-novel.byteimg.com/a.png","content":"终于等到了[飞吻]"},
      {"create_time":"0","digg_count":"0","user_name":"","user_avatar":"","content":"   "}
    ]}
  },
  "seriesSocialInfo":{"series_favorite_count":"1200"}
}},"errors":null};
</script></body></html>`

const sampleDetailHTML = `<!doctype html><html><body><script>
window.__DATA__ = {"loaderData":{"detail_layout":{},"detail_page":{
  "isSuccess":true,
  "series_id":"7704532495357725721",
  "seriesSocialInfo":{
    "rating":9.5,
    "rating_count":8932,
    "reviews":{"total_count":"1677","reviews":[
      {"create_time":"1794712164","digg_count":"365","rating":10,"user_name":"ᴿ悦动影视","user_avatar":"https://cdn.example/c.jpeg","content":"挺好的一部剧"}
    ]}
  }
}},"errors":null};
</script></body></html>`

// TestParsePublicReviews pins the review parsing rules.
func TestParsePublicReviews(t *testing.T) {
	node, err := findLoaderNode(samplePlayerHTML)
	if err != nil {
		t.Fatalf("findLoaderNode: %v", err)
	}
	if node.Vid != "7704532501426867224" {
		t.Fatalf("vid = %q", node.Vid)
	}
	if node.EpisodeSocialInfo == nil {
		t.Fatalf("episodeSocialInfo missing")
	}
	items := toCommentItems(node.EpisodeSocialInfo.Reviews, 100)
	if len(items) != 2 {
		t.Fatalf("expected 2 items after dedupe and blank filter, got %d: %+v", len(items), items)
	}
	if items[0].Author != "倾城契约孟小姐" {
		t.Fatalf("author = %q", items[0].Author)
	}
	if items[0].Likes != 331 {
		t.Fatalf("likes = %d, want 331 (string counter must be parsed)", items[0].Likes)
	}
	if items[0].Avatar != "https://p3-novel.byteimg.com/a.png" {
		t.Fatalf("avatar = %q", items[0].Avatar)
	}
	// The insecure avatar must be dropped, not rendered as a broken image.
	if items[1].Avatar != "" {
		t.Fatalf("non-https avatar should be dropped, got %q", items[1].Avatar)
	}
	if items[0].TimeText == "" {
		t.Fatalf("time text should be formatted")
	}
}

func TestParseSeriesReviews(t *testing.T) {
	node, err := findLoaderNode(sampleDetailHTML)
	if err != nil {
		t.Fatalf("findLoaderNode: %v", err)
	}
	if node.SeriesSocialInfo == nil {
		t.Fatalf("seriesSocialInfo missing")
	}
	items := toCommentItems(node.SeriesSocialInfo.Reviews, 100)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Content != "挺好的一部剧" {
		t.Fatalf("content = %q", items[0].Content)
	}
}

func TestFlexInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{float64(42), 42},
		{"42", 42},
		{" 42 ", 42},
		{"", 0},
		{nil, 0},
		{true, 0},
	}
	for _, tc := range cases {
		if got := flexInt(tc.in); got != tc.want {
			t.Fatalf("flexInt(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestLiveHongguoCommentsAndDanmaku exercises the two new endpoints against
// the live platform.
func TestLiveHongguoCommentsAndDanmaku(t *testing.T) {
	useStableTestDeviceDir(t)
	c := New()

	const (
		series = "7704532495357725721"
	)
	items, err := c.Search("持械入宋")
	if err != nil {
		if upstream.Unreachable(err) {
			t.Skipf("红果上游当前不可达，跳过 live 校验（%v）", err)
		}
		t.Fatalf("Search: %v", err)
	}
	sid := series
	for _, it := range items {
		if it.VideoData.SeriesID == series {
			sid = it.VideoData.SeriesID
			break
		}
	}
	d, err := c.Detail(sid)
	if err != nil {
		if upstream.Unreachable(err) {
			t.Skipf("红果上游当前不可达，跳过 live 校验（%v）", err)
		}
		t.Fatalf("Detail: %v", err)
	}
	if len(d.VidList) == 0 {
		t.Skip("该剧集没有可播放分集")
	}
	vid := d.VidList[0]

	t.Run("episode comments", func(t *testing.T) {
		comments, err := c.EpisodeComments(sid, vid, 100)
		if err != nil {
			if upstream.Unreachable(err) {
				t.Skipf("上游当前不可达，跳过 live 校验（%v）", err)
			}
			t.Fatalf("EpisodeComments: %v", err)
		}
		t.Logf("分集评论 %d 条", len(comments))
		if len(comments) == 0 {
			t.Skip("该集暂无公开评论")
		}
		if comments[0].Author == "" || comments[0].Content == "" {
			t.Fatalf("评论字段为空: %+v", comments[0])
		}
	})

	t.Run("series comments", func(t *testing.T) {
		comments, rating, ratingCount, err := c.SeriesComments(sid, 100)
		if err != nil {
			if upstream.Unreachable(err) {
				t.Skipf("上游当前不可达，跳过 live 校验（%v）", err)
			}
			t.Fatalf("SeriesComments: %v", err)
		}
		t.Logf("剧集评论 %d 条，评分 %.1f（%d 人）", len(comments), rating, ratingCount)
		if len(comments) == 0 {
			t.Skip("该剧暂无公开剧评")
		}
	})

	t.Run("timed danmaku", func(t *testing.T) {
		dm, err := c.Danmaku(sid, vid)
		if err != nil {
			if upstream.Unreachable(err) {
				t.Skipf("上游当前不可达，跳过 live 校验（%v）", err)
			}
			t.Fatalf("Danmaku: %v", err)
		}
		t.Logf("弹幕 %d 条", len(dm))
		if len(dm) == 0 {
			t.Skip("该集暂无弹幕")
		}
		for i, item := range dm {
			if strings.TrimSpace(item.Text) == "" {
				t.Fatalf("第 %d 条弹幕文本为空", i)
			}
			if item.TimeSec < 0 {
				t.Fatalf("第 %d 条弹幕时间非法: %v", i, item.TimeSec)
			}
			if i > 0 && dm[i-1].TimeSec > item.TimeSec {
				t.Fatalf("弹幕未按时间排序: %v > %v", dm[i-1].TimeSec, item.TimeSec)
			}
		}
		t.Logf("首条弹幕 @%.2fs: %s", dm[0].TimeSec, dm[0].Text)
	})
}

func TestLiveSearchActor(t *testing.T) {
	c := New()
	body, err := c.get("/api/search/result", url.Values{"query": {"马秋元"}}, c.BaseURL+"/")
	if err != nil {
		if upstream.Unreachable(err) || upstream.IsThrottled(err) {
			t.Skipf("上游暂时不可用，跳过: %v", err)
		}
		t.Fatalf("get search: %v", err)
	}
	t.Logf("Search result for actor (len %d): %s", len(body), string(body[:min(len(body), 2000)]))
}
