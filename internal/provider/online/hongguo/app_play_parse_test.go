package hongguo

import (
	"encoding/json"
	"testing"
)

// 这些用例锁定 v5 响应解析的兼容边界。上游 Kotlin 端 PlaybackModels.renditions
// 是判据来源：编码标在三处、URL 有四个候选、video_list 有对象/数组两种形态。

func TestPickStreamCodecFields(t *testing.T) {
	t.Run("entry 层 codec_type 参与 HEVC 私有档排除", func(t *testing.T) {
		vm := []byte(`{"video_list":[` +
			`{"codec_type":"bytevc2","main_url":"https://cdn/hevc.mp4","video_meta":{"definition":"720p"}},` +
			`{"codec_type":"avc1","main_url":"https://cdn/avc.mp4","video_meta":{"definition":"480p"}}]}`)
		st, err := pickStreamFromVideoModel(vm, "")
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if st.URL != "https://cdn/avc.mp4" {
			t.Fatalf("应跳过 bytevc2 档，实际选中 %q", st.URL)
		}
	})

	t.Run("gear_des_key 中的 bvc2 作为独立 token 才排除", func(t *testing.T) {
		// 形如 xbvc2x 不是 token，不应误伤。
		if !isExcludedCodec("bvc2") {
			t.Fatal("bvc2 应被排除")
		}
		if isExcludedCodec("xbvc2x") {
			t.Fatal("xbvc2x 不是独立 token，不应排除")
		}
		if !isExcludedCodec("bytevc2") {
			t.Fatal("bytevc2 应被排除")
		}
	})

	t.Run("真正的 H.264 档优先于 bytevc1", func(t *testing.T) {
		vm := []byte(`{"video_list":[` +
			`{"video_meta":{"codec_type":"bytevc1","definition":"1080p","vwidth":1080,"vheight":1920,"main_url":"https://cdn/hevc.mp4"}},` +
			`{"video_meta":{"codec_type":"avc1","definition":"360p","vwidth":360,"vheight":640,"main_url":"https://cdn/avc.mp4"}}]}`)
		// main_url 在 video_meta 内层时旧实现读不到；这里同时放外层验证优先级。
		vm = []byte(`{"video_list":[` +
			`{"main_url":"https://cdn/hevc.mp4","video_meta":{"codec_type":"bytevc1","definition":"1080p"}},` +
			`{"main_url":"https://cdn/avc.mp4","video_meta":{"codec_type":"avc1","definition":"360p"}}]}`)
		st, err := pickStreamFromVideoModel(vm, "")
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if st.URL != "https://cdn/avc.mp4" {
			t.Fatalf("可硬解的 avc1 应优先，实际选中 %q (codec=%s)", st.URL, st.Codec)
		}
	})
}

func TestPickStreamURLCandidates(t *testing.T) {
	t.Run("main_url 缺失时用 play_url", func(t *testing.T) {
		vm := []byte(`{"video_list":[{"play_url":"https://cdn/play.mp4","video_meta":{"definition":"720p","codec_type":"avc1"}}]}`)
		st, err := pickStreamFromVideoModel(vm, "")
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if st.URL != "https://cdn/play.mp4" {
			t.Fatalf("play_url 未被采纳: %q", st.URL)
		}
	})

	t.Run("backup_url_1 作为最后候选", func(t *testing.T) {
		vm := []byte(`{"video_list":[{"backup_url_1":"https://cdn/bak1.mp4","video_meta":{"definition":"720p","codec_type":"avc1"}}]}`)
		st, err := pickStreamFromVideoModel(vm, "")
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if st.URL != "https://cdn/bak1.mp4" {
			t.Fatalf("backup_url_1 未被采纳: %q", st.URL)
		}
	})
}

func TestDecodeRenditionsObjectForm(t *testing.T) {
	raw := json.RawMessage(`{"720p":{"main_url":"https://cdn/a.mp4","video_meta":{"codec_type":"avc1"}},"1080p":{"main_url":"https://cdn/b.mp4","video_meta":{"codec_type":"avc1"}}}`)
	rows := decodeRenditions(raw)
	if len(rows) != 2 {
		t.Fatalf("对象形态应解析出 2 档，实际 %d", len(rows))
	}
	if rows[0].VideoMeta.Definition != "" || rows[0].MainURL == "" {
		t.Fatalf("对象形态档位内容异常: %+v", rows[0])
	}
}

func TestLocateVideoModelRowStrict(t *testing.T) {
	t.Run("精确匹配 episode id", func(t *testing.T) {
		data := map[string]json.RawMessage{
			"1004": json.RawMessage(`{"ep-a":{"video_model":{}},"ep-b":{"video_model":{}}}`),
		}
		got := locateVideoModelRow(data, "ep-b")
		if len(got) == 0 {
			t.Fatal("ep-b 应命中")
		}
	})

	t.Run("分组内无精确匹配时不得取任意一条", func(t *testing.T) {
		data := map[string]json.RawMessage{
			"1004": json.RawMessage(`{"ep-a":{"video_model":{}},"ep-b":{"video_model":{}}}`),
		}
		if got := locateVideoModelRow(data, "ep-z"); len(got) != 0 {
			t.Fatalf("不应回退到其它集，实际拿到 %s", got)
		}
	})
}
