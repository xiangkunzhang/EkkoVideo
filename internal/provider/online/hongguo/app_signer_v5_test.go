package hongguo

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v5CarrierVector 是一条字节变换（v5 分支）的期望输出。
type v5CarrierVector struct {
	TS        int    `json:"ts"`
	Source    string `json:"source"`
	Key       string `json:"key"`
	IV        string `json:"iv"`
	Encrypted string `json:"encrypted"`
}

// v5DigestVector 是一条请求摘要的期望输出。
type v5DigestVector struct {
	TS     int    `json:"ts"`
	Query  string `json:"query"`
	Body   string `json:"body"`
	Digest string `json:"digest"`
}

func loadV5Vectors(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读取向量 %s 失败: %v", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("解析向量 %s 失败: %v", name, err)
	}
}

// TestAesV3V5MatchesCarrierVectors 用外部合成的期望输出校验 v5 字节变换。
//
// 这组向量覆盖时间戳低 2 位的全部四种取值（每种一组 sbox 分段）。
// 逐字节相等才算通过 —— 任何一位错了都会导致服务端解不开 medusa。
func TestAesV3V5MatchesCarrierVectors(t *testing.T) {
	var vectors []v5CarrierVector
	loadV5Vectors(t, "signing-v5-carrier.json", &vectors)
	if len(vectors) != 4 {
		t.Fatalf("期望 4 条载体向量，实际 %d 条", len(vectors))
	}
	for i, v := range vectors {
		key, err := hex.DecodeString(v.Key)
		if err != nil {
			t.Fatalf("载体 %d: key 非法: %v", i, err)
		}
		source, err := hex.DecodeString(v.Source)
		if err != nil {
			t.Fatalf("载体 %d: source 非法: %v", i, err)
		}
		iv, err := hex.DecodeString(v.IV)
		if err != nil {
			t.Fatalf("载体 %d: iv 非法: %v", i, err)
		}
		got := hex.EncodeToString(newAesV3V5(key, uint32(v.TS)).encrypt(source, iv))
		if got != v.Encrypted {
			t.Errorf("载体 %d (ts=%d) 不匹配:\n得到 %s\n期望 %s", i, v.TS, got, v.Encrypted)
		}
	}
}

// TestHashF13V5MatchesDigestVectors 用外部合成的期望输出校验 v5 请求摘要。
//
// 这组向量覆盖时间戳的全部 32 种轮转类别 —— 摘要里每一处与时间戳相关的
// 取表都必须正确，所以必须全类别覆盖，不能只测一条。
func TestHashF13V5MatchesDigestVectors(t *testing.T) {
	var vectors []v5DigestVector
	loadV5Vectors(t, "signing-v5-f13.json", &vectors)
	if len(vectors) != 32 {
		t.Fatalf("期望 32 条摘要向量，实际 %d 条", len(vectors))
	}
	for i, v := range vectors {
		query, err := hex.DecodeString(v.Query)
		if err != nil {
			t.Fatalf("摘要 %d: query 非法: %v", i, err)
		}
		body, err := hex.DecodeString(v.Body)
		if err != nil {
			t.Fatalf("摘要 %d: body 非法: %v", i, err)
		}
		got, err := hashF13V5(query, body, uint32(v.TS))
		if err != nil {
			t.Fatalf("摘要 %d (ts=%d) 计算失败: %v", i, v.TS, err)
		}
		if len(got) != 20 {
			t.Fatalf("摘要 %d: 长度应为 20，实际 %d", i, len(got))
		}
		if hex.EncodeToString(got[:16]) != v.Digest {
			t.Errorf("摘要 %d (ts=%d) 不匹配:\n得到 %s\n期望 %s", i, v.TS, hex.EncodeToString(got[:16]), v.Digest)
		}
		// 校验和的第 5 位是版本标记，必须置位。
		if got[16]&0x20 == 0 {
			t.Errorf("摘要 %d: 校验和缺少版本标记位", i)
		}
	}
}

// TestHashF13V5RejectsOtherBranches 确认只有分支 12 能算出摘要。
// 票据搜索正是靠这个判定来筛选的，判据放宽会导致签出无效请求。
func TestHashF13V5RejectsOtherBranches(t *testing.T) {
	var vectors []v5DigestVector
	loadV5Vectors(t, "signing-v5-f13.json", &vectors)
	accepted := 0
	for _, v := range vectors {
		query, _ := hex.DecodeString(v.Query)
		body, _ := hex.DecodeString(v.Body)
		ts := uint32(v.TS)
		_, err := hashF13V5(query, body, ts)
		if err == nil {
			accepted++
			continue
		}
		// 被拒的必须是分支号不等于 12。
		if (calcIV(query, body, ts) & 15) == v5BranchValue {
			t.Errorf("ts=%d 落在合法分支上却被拒", ts)
		}
	}
	if accepted == 0 {
		t.Fatal("没有任何向量落在合法分支上，测试数据可能选错了")
	}
}

// TestSignPlaybackV5BindsTicketAndIdentity 锁定签名的三条不变量：
//
//  1. 签名前后除票据外参数不变；
//  2. 票据必须落在可签发的分支上；
//  3. 换设备身份必须换出不同的 medusa —— 否则签名与身份脱钩，服务端会拒。
func TestSignPlaybackV5BindsTicketAndIdentity(t *testing.T) {
	signer := NewAppSigner()
	q := v5PlaybackQuery{
		DeviceID:    "synthetic-device",
		InstallID:   "synthetic-iid",
		CDID:        "synthetic-cdid",
		VersionName: "7.4.3.32",
		Channel:     "vivo_8662_64",
		DeviceType:  "PGT-AN10",
		OSVersion:   "12",
	}
	body := []byte("synthetic-body")

	url, headers, err := signer.SignPlaybackV5("https://example.test/model", q, body, 1800000000000)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	idx := indexByte(url, '?')
	if idx < 0 {
		t.Fatalf("URL 缺少查询串: %s", url)
	}
	query := url[idx+1:]

	// 1. 除票据外的参数必须逐字保留。
	for _, kv := range q.orderedPairs() {
		want := encodeQueryParam(kv[0]) + "=" + encodeQueryParam(kv[1])
		if !containsPair(query, want) {
			t.Errorf("查询串缺少或改动了 %s", want)
		}
	}
	// 票据必须存在且是可签发分支。
	ticket := queryValue(query, "_rticket")
	if ticket == "" {
		t.Fatal("查询串缺少 _rticket")
	}
	if hashBranchV5(query, body, uint32(1800000000000/1000)) != 0 {
		t.Fatal("签出的票据落在不可签发的分支上")
	}

	// 2. 头集合：只允许三个签名头加内容相关的头。
	for _, allowed := range []string{"User-Agent", "Accept", "x-khronos", "x-helios", "x-medusa", "Content-Type", "x-ss-stub"} {
		if _, ok := headers[allowed]; !ok {
			t.Errorf("缺少头 %s", allowed)
		}
	}
	for _, forbidden := range []string{"x-gorgon", "x-ladon", "x-argus", "x-ss-req-ticket", "sdk-version", "x-xs-from-web", "Cookie"} {
		if _, ok := headers[forbidden]; ok {
			t.Errorf("不应携带头 %s", forbidden)
		}
	}
	// x-ss-stub 必须按请求体算。
	if headers["x-ss-stub"] != md5Upper(body) {
		t.Errorf("x-ss-stub 不匹配: %s", headers["x-ss-stub"])
	}

	// 3. 换身份必须换出不同的 medusa。
	q2 := q
	q2.DeviceID = "another-device"
	_, headers2, err := signer.SignPlaybackV5("https://example.test/model", q2, body, 1800000000000)
	if err != nil {
		t.Fatalf("换身份后签名失败: %v", err)
	}
	if headers["x-medusa"] == headers2["x-medusa"] {
		t.Error("换设备身份后 medusa 未变化 —— 签名没有绑定身份")
	}
}

// md5Upper 返回请求体的 MD5 大写十六进制，与 x-ss-stub 的算法一致。
func md5Upper(body []byte) string {
	sum := md5.Sum(body)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// indexByte 返回字节在字符串中的下标，找不到返回 -1。
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// containsPair 判断查询串里是否存在一个完整的 key=value 片段。
func containsPair(query, pair string) bool {
	for _, part := range splitPairs(query) {
		if part == pair {
			return true
		}
	}
	return false
}

// splitPairs 按 & 切分查询串。
func splitPairs(query string) []string {
	var out []string
	start := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '&' {
			out = append(out, query[start:i])
			start = i + 1
		}
	}
	out = append(out, query[start:])
	return out
}

// queryValue 取查询串里指定 key 的值。
func queryValue(query, key string) string {
	prefix := encodeQueryParam(key) + "="
	for _, part := range splitPairs(query) {
		if len(part) > len(prefix) && part[:len(prefix)] == prefix {
			return part[len(prefix):]
		}
	}
	return ""
}
