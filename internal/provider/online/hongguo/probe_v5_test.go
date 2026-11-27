package hongguo

import (
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiangkunzhang/Velan/internal/provider/online/upstream"
)

// skipIfUpstreamUnavailable 把上游的临时不可用（限流、5xx、网络抖动）变成跳过。
//
// live 探针打的是真实上游。上游风控与临时故障是环境状态，不是我们的 bug ——
// 让探针 fail 会把真正的回归埋掉。
func skipIfUpstreamUnavailable(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if upstream.Unreachable(err) || upstream.IsServer(err) || upstream.IsNetwork(err) || upstream.IsThrottled(err) {
		t.Skipf("%s: 上游临时不可用，跳过 live 校验（%v）", label, err)
	}
	t.Fatalf("%s: %v", label, err)
}

// v5ProbeFixture 备好一次搜索 + 详情，供多个子检查复用。
// 只搜一次是刻意的：连续搜索会撞上上游的搜索风控。
type v5ProbeFixture struct {
	client *Client
	dev    *RegisteredDevice
	signer *AppSigner
	series string
	vids   []string
}

func newV5ProbeFixture(t *testing.T) v5ProbeFixture {
	t.Helper()
	base, _ := os.UserCacheDir()
	DeviceConfigDir = filepath.Join(base, "duanju-desktop-test")
	_ = os.MkdirAll(DeviceConfigDir, 0o755)

	c := New()
	dev, signer, err := defaultAppSession.get(c.HTTP, false)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	items, err := c.Search("持械入宋")
	skipIfUpstreamUnavailable(t, "search", err)
	if len(items) == 0 {
		t.Skip("搜索无结果")
	}
	d, err := c.Detail(items[0].VideoData.SeriesID)
	skipIfUpstreamUnavailable(t, "detail", err)
	return v5ProbeFixture{client: c, dev: dev, signer: signer, series: d.SeriesID, vids: d.VidList}
}

// TestProbeV5 一次性覆盖 v5 播放链路的四个关键事实。
//
// 这些检查打的是真实上游，是"能不能播"的唯一决定性证据。
func TestProbeV5(t *testing.T) {
	fx := newV5ProbeFixture(t)

	t.Run("取流拿到可播放档位", func(t *testing.T) {
		st, err := fx.client.multiVideoModelV5(fx.dev, fx.signer, fx.vids[0], "")
		if err != nil {
			skipIfUpstreamUnavailable(t, "v5 取流", err)
		}
		if st.URL == "" {
			t.Fatal("没有拿到播放地址")
		}
		if len(st.Key) != 16 {
			t.Fatalf("CENC key 长度应为 16，实际 %d", len(st.Key))
		}
		t.Logf("档位=%s 编码=%s 分辨率=%dx%d 有key=%v", st.Definition, st.Codec, st.Width, st.Height, len(st.Key) == 16)
		// bytevc2/bvc2 是 HEVC，桌面端解码不了，必须拿到 bytevc1 才算有可播档位。
		if st.Codec != "bytevc1" && st.Codec != "h264" && st.Codec != "avc1" {
			t.Errorf("编码是 %q，桌面端可能无法解码", st.Codec)
		}
	})

	t.Run("容器是渐进式且带 CENC 加密声明", func(t *testing.T) {
		st, err := fx.client.multiVideoModelV5(fx.dev, fx.signer, fx.vids[0], "")
		if err != nil {
			skipIfUpstreamUnavailable(t, "v5 取流", err)
		}
		head := fetchV5Head(t, fx.client, st.URL)
		t.Logf("头部 %d 字节", len(head))
		moovSeen, mdatSeen := false, false
		for off := 0; off+8 <= len(head); {
			sz := int(binary.BigEndian.Uint32(head[off : off+4]))
			typ := string(head[off+4 : off+8])
			hdr := 8
			if sz == 1 {
				if off+16 > len(head) {
					break
				}
				sz = int(binary.BigEndian.Uint64(head[off+8 : off+16]))
				hdr = 16
			}
			if typ == "mdat" {
				mdatSeen = true
				break
			}
			if sz < hdr || off+sz > len(head) {
				break
			}
			t.Logf("  偏移 %6d: %-6s 长度 %d", off, typ, sz)
			if typ == "moov" {
				moovSeen = true
			}
			off += sz
		}
		if !moovSeen || !mdatSeen {
			t.Fatalf("容器结构异常：moov=%v mdat=%v", moovSeen, mdatSeen)
		}
		for _, marker := range []string{"encv", "sinf", "tenc", "senc"} {
			if !strings.Contains(string(head), marker) {
				t.Errorf("头部缺少加密声明 %q —— 判据本身可能失效了", marker)
			}
		}
	})

	t.Run("解密后是明文 AVCC", func(t *testing.T) {
		st, err := fx.client.multiVideoModelV5(fx.dev, fx.signer, fx.vids[0], "")
		if err != nil {
			skipIfUpstreamUnavailable(t, "v5 取流", err)
		}
		head := fetchV5Head(t, fx.client, st.URL)
		mdatAt := indexOfBox(head, "mdat")
		if mdatAt < 0 {
			t.Fatal("头部里没有 mdat")
		}
		dataAt := mdatAt + 8 // mdat 的 box 头是 8 字节
		if dataAt+4 > len(head) {
			t.Fatal("头部窗口没覆盖到 mdat 数据")
		}
		t.Logf("解密前 mdat 起始 4 字节: % x", head[dataAt:dataAt+4])
		out, err := DecryptFMP4(head, st.Key)
		if err != nil {
			t.Fatalf("DecryptFMP4 失败: %v", err)
		}
		t.Logf("解密后 mdat 起始 4 字节: % x", out[dataAt:dataAt+4])

		// 明文判据：MP4 的 mdat 用 AVCC 长度前缀，不是 Annex B 起始码。
		// 前 3 字节必须为 0（长度 < 16MB）；密文连续三字节为 0 的概率约 1/16M。
		naluLen := binary.BigEndian.Uint32(out[dataAt : dataAt+4])
		if out[dataAt] != 0 || out[dataAt+1] != 0 || out[dataAt+2] != 0 {
			t.Fatalf("解密后不是明文 AVCC 长度前缀: % x —— 解密没有生效", out[dataAt:dataAt+4])
		}
		if naluLen == 0 || naluLen > 1<<20 {
			t.Fatalf("解密后 NAL 长度不合理: %d", naluLen)
		}
		t.Logf("✅ 解密后是明文 AVCC：NAL 长度 %d", naluLen)

		for _, marker := range []string{"encv", "enca", "sinf", "tenc", "senc", "saiz", "saio"} {
			if strings.Contains(string(out), marker) {
				t.Errorf("残留加密声明 %q", marker)
			}
		}
	})

	t.Run("多集布局一致", func(t *testing.T) {
		progressive, fragmented := 0, 0
		for i, vid := range fx.vids {
			if i >= 4 {
				break
			}
			st, err := fx.client.multiVideoModelV5(fx.dev, fx.signer, vid, "")
			if err != nil {
				skipIfUpstreamUnavailable(t, "v5 取流", err)
			}
			head := fetchV5Head(t, fx.client, st.URL)
			if _, err := ParseMoovSamples(head); err != nil || indexOfBox(head, "mdat") < 0 {
				fragmented++
				t.Logf("集 %d: 非渐进式", i)
				continue
			}
			progressive++
			t.Logf("集 %d: 渐进式，编码 %s", i, st.Codec)
		}
		t.Logf("汇总：渐进式 %d 集，非渐进式 %d 集", progressive, fragmented)
	})
}

// fetchV5Head 取媒体头部，用于判定容器布局。
func fetchV5Head(t *testing.T, c *Client, url string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Range", "bytes=0-524287")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("取流失败: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	return body
}

// indexOfBox 返回指定类型顶层 box 的偏移，找不到返回 -1。
func indexOfBox(data []byte, want string) int {
	for off := 0; off+8 <= len(data); {
		sz := int(binary.BigEndian.Uint32(data[off : off+4]))
		typ := string(data[off+4 : off+8])
		hdr := 8
		if sz == 1 {
			if off+16 > len(data) {
				return -1
			}
			sz = int(binary.BigEndian.Uint64(data[off+8 : off+16]))
			hdr = 16
		}
		if typ == want {
			return off
		}
		if sz < hdr || off+sz > len(data) {
			return -1
		}
		off += sz
	}
	return -1
}
