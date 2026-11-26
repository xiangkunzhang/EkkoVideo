package hongguo

import (
	"fmt"
	"testing"
)

func TestSeenRingCapacityAndDedup(t *testing.T) {
	ring := newSeenRing(3)
	ring.add("a")
	ring.add("b")
	ring.add("a") // duplicate should be ignored
	ring.add("c")

	if got := ring.filterIDs(10); got != "a,b,c" {
		t.Fatalf("unexpected filterIDs: %q", got)
	}

	ring.add("d") // evicts "a"
	if ring.has("a") {
		t.Fatalf("expected 'a' to be evicted")
	}
	if !ring.has("b") || !ring.has("c") || !ring.has("d") {
		t.Fatalf("expected b, c, d to remain in ring")
	}
	if got := ring.filterIDs(2); got != "c,d" {
		t.Fatalf("unexpected limited filterIDs: %q", got)
	}
}

func TestParseBookmallFeedTabAndChangeShapes(t *testing.T) {
	tabPayload := []byte(`{
		"code": 0,
		"data": {
			"session_id": "sess-1",
			"offset": 12,
			"tab_item": [{
				"cell_data": [{
					"cell_id": "cell-99",
					"plan_id": "plan-1",
					"version_tag": "v1",
					"cell_data": [{
						"video_data": [
							{
								"series_id": "1001",
								"series_title": "第一部",
								"series_cover": "https://example.com/1.jpg",
								"episode_cnt": 60,
								"category_list": [{"name": "都市"}]
							},
							{
								"series_id": "1001",
								"series_title": "重复部"
							}
						]
					}]
				}]
			}]
		}
	}`)

	items, cur, err := parseBookmallFeed(tabPayload, feedCursor{})
	if err != nil {
		t.Fatalf("parseBookmallFeed failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 deduplicated item, got %d", len(items))
	}
	if items[0].SeriesID != "1001" || items[0].RightText != "共60集" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	if !cur.initialized || cur.sessionID != "sess-1" || cur.cellID != "cell-99" || cur.offset != 12 {
		t.Fatalf("unexpected cursor: %+v", cur)
	}

	changePayload := []byte(`{
		"code": 0,
		"data": {
			"session_id": "sess-1",
			"offset": 24,
			"cell_data": [{
				"video_data": [
					{"series_id": "1002", "series_title": "第二部", "episode_cnt": 45}
				]
			}]
		}
	}`)
	items2, cur2, err := parseBookmallFeed(changePayload, cur)
	if err != nil {
		t.Fatalf("parseBookmallFeed change failed: %v", err)
	}
	if len(items2) != 1 || items2[0].SeriesID != "1002" || cur2.offset != 24 {
		t.Fatalf("unexpected change result: items=%+v cur=%+v", items2, cur2)
	}
}

func TestParseBookmallFeedIdentityRejected(t *testing.T) {
	raw := []byte(fmt.Sprintf(`{"code": %d}`, 1011))
	_, _, err := parseBookmallFeed(raw, feedCursor{})
	if err == nil {
		t.Fatalf("expected error for rejected identity code")
	}
}
