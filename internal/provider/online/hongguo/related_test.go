package hongguo

import (
	"testing"
)

func TestParseRelatedWorks(t *testing.T) {
	raw := []byte(`{
		"code": 0,
		"data": [
			{"show_type": 100, "cell_name": "other", "video_data": [{"series_id": "111", "title": "ignored", "content_type": 1004, "episode_cnt": 10}]},
			{
				"show_type": 547,
				"cell_name": "相关作品",
				"video_data": [
					{"series_id": "999", "title": "当前剧第一季", "content_type": 1004, "episode_cnt": 50},
					{"series_id": "2002", "title": "同类型推荐剧", "content_type": 1004, "episode_cnt": 60, "cover": "https://img/2.jpg", "video_desc": "desc2"},
					{"series_id": "2003", "title": "当前剧 第三季", "content_type": 1007, "season_index": 3, "tag_info": {"text": "第三季"}},
					{"series_id": "2001", "title": "当前剧第二季", "content_type": 1004, "episode_cnt": 80, "season_index": 2, "cover": "https://img/1.jpg"},
					{"series_id": "2004", "title": "不可播", "content_type": 1004, "disable_play": true, "episode_cnt": 10},
					{"series_id": "2005", "title": "小说类型", "content_type": 1, "episode_cnt": 10}
				]
			}
		]
	}`)

	items, err := parseRelatedWorks(raw, "999", "当前剧第一季")
	if err != nil {
		t.Fatalf("parseRelatedWorks: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(items), items)
	}
	if items[0].SeriesID != "2001" || !items[0].SameSeries || items[0].SeasonIndex != 2 || items[0].Season != "第2季" {
		t.Fatalf("unexpected first item (want season 2): %+v", items[0])
	}
	if items[1].SeriesID != "2003" || !items[1].SameSeries || !items[1].Upcoming || items[1].RightText != "待上线" {
		t.Fatalf("unexpected second item (want upcoming season 3): %+v", items[1])
	}
	if items[2].SeriesID != "2002" || items[2].SameSeries {
		t.Fatalf("unexpected third item (want non-same-series): %+v", items[2])
	}

	// Verify 「第X季完结篇」 matches same-series stem while 「爱在第三季的我们」 does not get misread.
	if relatedTitleStem("和离后我带女儿横扫魔域第三季完结篇") != relatedTitleStem("和离后我带女儿横扫魔域第二季") {
		t.Fatalf("expected 完结篇 stem to match season 2 stem")
	}
	if relatedTitleStem("爱在第三季的我们") != "爱在第三季的我们" {
		t.Fatalf("mid-title 第X季 should not be stripped: got %q", relatedTitleStem("爱在第三季的我们"))
	}
}

func TestLiveRelated(t *testing.T) {
	c := New()
	items, err := c.Search("持械入宋")
	skipIfUpstreamUnavailable(t, "search", err)
	if len(items) == 0 {
		t.Skip("no search results")
	}
	card := items[0].VideoData
	rel, err := c.Related(card.SeriesID, card.SeriesTitle)
	skipIfUpstreamUnavailable(t, "related", err)
	t.Logf("Related for %s (%s): %d items", card.SeriesTitle, card.SeriesID, len(rel))
	for i, r := range rel {
		if i >= 8 {
			break
		}
		t.Logf("  [%d] id=%s title=%q sameSeries=%v season=%q ep=%d upcoming=%v",
			i, r.SeriesID, r.Title, r.SameSeries, r.Season, r.EpisodeCnt, r.Upcoming)
	}
}
