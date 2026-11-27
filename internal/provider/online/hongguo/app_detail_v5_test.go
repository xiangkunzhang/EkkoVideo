package hongguo

import "testing"

func TestParseAppVideoDetail(t *testing.T) {
	raw := []byte(`{
		"code": 0,
		"data": {
			"video_data": {
				"series_id": 7491234567890123456,
				"series_title": "测试全集短剧",
				"series_cover": "https://cdn.example.com/cover.jpg",
				"series_intro": "简介",
				"episode_cnt": 3,
				"video_list": [
					{"vid": 7491234567890000002, "vid_index": 2, "series_id": 7491234567890123456},
					{"vid": "7491234567890000001", "vid_index": 1, "series_id": "7491234567890123456"},
					{"vid": "7491234567890000003", "vid_index": 3, "series_id": 7491234567890123456}
				]
			}
		}
	}`)

	d, err := parseAppVideoDetail(raw, "7491234567890123456")
	if err != nil {
		t.Fatalf("parseAppVideoDetail: %v", err)
	}
	if d.SeriesName != "测试全集短剧" || len(d.VidList) != 3 {
		t.Fatalf("unexpected detail: %+v", d)
	}
	want := []string{"7491234567890000001", "7491234567890000002", "7491234567890000003"}
	for i, w := range want {
		if d.VidList[i] != w {
			t.Fatalf("vid[%d] = %q, want %q", i, d.VidList[i], w)
		}
	}
}
