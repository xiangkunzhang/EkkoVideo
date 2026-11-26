package hongguo

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// v5 播放签名的协议参数（Android v04.10.03 线格式）。
//
// 这一版与旧版（v3）的差别不只是换几个常量：摘要分支的准入条件、
// 种子、轮表、校验和位都不同，请求体的编码方式与签名头集合也不同。
const (
	// v5HashSeed 是 v5 分支的固定轮函数种子（旧版按分支号查表得到）。
	v5HashSeed = 0x9d4a97c2

	// v5ChecksumBit 追加在校验和上，用于标记摘要属于 v5 线格式。
	v5ChecksumBit = 0x20

	// v5BranchValue 是 v5 唯一接受的摘要分支号。
	// 签名前必须先把请求票据调整到落在该分支，否则服务端直接丢弃。
	v5BranchValue = 12

	// v5TicketSearchLimit 是请求票据的搜索上界。
	v5TicketSearchLimit = 1024

	// v5PlayerUA 是 v5 播放接口使用的 UA。UA 里的机型必须与查询参数声明的一致。
	v5PlayerUA = "com.phoenix.read/73932 (Linux; U; Android 12; zh_CN_#Hans; PGT-AN10; Build/V417IR;tt-ok/3.12.13.20)"
)

// v5HashChosen 是 v5 分支固定使用的轮表下标。
var v5HashChosen = [11]int{96, 1, 3, 4, 0, 6, 5, 2, 0, 7, 7}

// v5Identity 是写入 medusa 明文的设备声明。
// 服务端会核对这里的 device_id 与查询参数里的 device_id 是否一致。
type v5Identity struct {
	DeviceID    string
	VersionName string
	Channel     string
	OSVersion   string
	DeviceType  string
}

// v5PlaybackQuery 描述一次 v5 播放请求的查询参数。
// 参数顺序即签名顺序，不能随意调整。
type v5PlaybackQuery struct {
	DeviceID    string
	InstallID   string
	CDID        string
	VersionName string
	Channel     string
	DeviceType  string
	OSVersion   string
}

// identity 从查询参数导出 medusa 明文里要用的设备声明。
func (q v5PlaybackQuery) identity() v5Identity {
	return v5Identity{
		DeviceID:    q.DeviceID,
		VersionName: q.VersionName,
		Channel:     q.Channel,
		OSVersion:   q.OSVersion,
		DeviceType:  q.DeviceType,
	}
}

// orderedPairs 返回固定顺序的查询参数。_rticket 不在其中 —— 它在签名时才追加到末尾。
func (q v5PlaybackQuery) orderedPairs() [][2]string {
	if q.VersionName == "7.4.5.32" {
		return [][2]string{
			{"aid", "8662"},
			{"app_name", "novelread"},
			{"version_code", "74532"},
			{"version_name", "7.4.5.32"},
			{"update_version_code", "74532"},
			{"manifest_version_code", "74532"},
			{"os", "android"},
			{"ssmix", "a"},
			{"channel", q.Channel},
			{"device_platform", "android"},
			{"device_type", q.DeviceType},
			{"device_brand", "Redmi"},
			{"os_version", q.OSVersion},
			{"os_api", "36"},
			{"ac", "wifi"},
			{"language", "zh"},
			{"host_abi", "arm64-v8a"},
			{"iid", q.InstallID},
			{"cdid", q.CDID},
			{"device_id", q.DeviceID},
		}
	}
	return [][2]string{
		{"aid", "8662"},
		{"app_name", "novelread"},
		{"version_code", "74332"},
		{"version_name", q.VersionName},
		{"channel", q.Channel},
		{"device_platform", "android"},
		{"device_type", q.DeviceType},
		{"os_version", q.OSVersion},
		{"update_version_code", "74332"},
		{"iid", q.InstallID},
		{"cdid", q.CDID},
		{"device_id", q.DeviceID},
	}
}

// hashBranchV5 返回摘要分支号：只有落在 12 时才允许签发。
// 返回值 0 表示可签发。
func hashBranchV5(query string, body []byte, ts uint32) int {
	qSm3 := sm3Sum([]byte(query))
	bMd5 := md5.Sum(body)
	v := calcIV(qSm3[:], bMd5[:], ts)
	if (v & 15) == v5BranchValue {
		return 0
	}
	return 1
}

// hashF13V5 计算 v5 分支的请求摘要。
//
// 注意入参是 SM3(query) 与 MD5(body)，不是原始字节 —— 调用方负责先算好。
func hashF13V5(query, body []byte, ts uint32) ([]byte, error) {
	v := calcIV(query, body, ts)
	if (v & 15) != v5BranchValue {
		return nil, errors.New("hongguo: v5 unsupported hash branch")
	}
	return hashF13Spec(query, body, ts, v5HashSeed, v5HashChosen, v5ChecksumBit)
}

// medusaV5 生成 x-medusa 头。
//
// 结构上分三段：
//  1. 明文：设备信息（deviceInfo）、运行环境（env）、消息体（message）三层嵌套
//  2. 混淆：明文与 SM3 摘要异或后整体反转，再与种子异或
//  3. 封装：加上校验字与随机数，用 v5 分支的字节变换加密
func medusaV5(rawURL string, body []byte, ts uint32, who v5Identity, processNonce string, processStarted int64) (string, error) {
	bMd5 := md5.Sum(body)
	q := ""
	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		q = rawURL[idx+1:]
	}
	qSm3 := sm3Sum([]byte(q))
	f13, err := hashF13V5(qSm3[:], bMd5[:], ts)
	if err != nil {
		return "", err
	}

	// medusa 明文里的 f5/f16 设备令牌：24 字符 base64url nonce 前置 'A' 补足 25 位。
	deviceToken := processNonce
	if len(deviceToken) != 25 || deviceToken[0] != 'A' {
		padded := processNonce + "AAAAAAAAAAAAAAAAAAAAAAAA"
		deviceToken = "A" + padded[:24]
	}

	brand := "HONOR"
	if who.VersionName == "7.4.5.32" {
		brand = "Redmi"
	}
	envLaunch := int64(randUint32()%21) + 100
	envPid := int64(randUint32()%2000) + 10001

	// 补齐设备信封中截至字段 38 的 36 个已观测字段（对齐 hongguoTV 0.16.15）。
	deviceInfo := encodeProto(
		pbSint(1, 1), pbSint(2, 2), pbStr(3, "8662"), pbStr(4, who.DeviceID),
		pbStr(5, deviceToken), pbStr(6, "!noperm!"),
		pbSint(7, -888888), pbSint(8, -888888), pbSint(9, 3), pbSint(10, -888888),
		pbStr(11, "!notset!"), pbStr(12, "Asia/Shanghai,8"), pbStr(13, "zh_CN"), pbSint(14, 4),
		pbFloat(16, 0), pbFloat(17, 0), pbFloat(18, 0),
		pbFloat(19, 0), pbFloat(20, 0), pbFloat(21, 0),
		pbStr(22, who.OSVersion), pbSint(23, 41), pbSint(24, 36),
		pbSint(25, processStarted), pbSint(26, processStarted),
		pbSint(27, processStarted), pbSint(28, int64(ts)),
		pbSint(29, -1), pbStr(30, who.DeviceType), pbStr(31, brand),
		pbStr(32, who.DeviceType), pbStr(33, who.DeviceType),
		pbStr(34, brand), pbStr(35, brand), pbStr(36, brand), pbSint(38, 31),
	)

	env := encodeProto(
		pbSint(1, envLaunch), pbSint(2, 247235599), pbSint(3, 247235596),
		pbSint(5, 7), pbStr(6, "v04.10.03.04-bugfix"), pbSint(7, envPid),
		pbStr(8, who.Channel), pbBytes(12, deviceInfo),
		pbBytes(13, encodeProto(pbSint(1, int64(ts)), pbSint(2, -2), pbSint(4, 200))),
		pbStr(14, who.VersionName),
	)

	field21 := int64(377)
	if who.VersionName == "7.4.5.32" {
		field21 = 312
	}
	flags := []byte{0x44, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	msg := encodeProto(
		pbBytes(1, v5MagicBytes), pbSint(2, 5), pbSint(3, int64(randUint32())),
		pbStr(4, "8662"), pbStr(5, who.DeviceID), pbStr(6, v5License),
		pbStr(7, who.VersionName), pbStr(8, "v04.10.03-ml-android"), pbSint(9, 0x040a0300),
		pbBytes(10, flags), pbSint(12, int64(ts)), pbBytes(13, f13),
		pbBytes(14, qSm3[:6]),
		pbBytes(15, encodeProto(pbSint(1, 305), pbSint(2, 4), pbSint(3, 694367), pbSint(5, 1621072674))),
		pbStr(16, deviceToken),
		// 7.4.5.32 使用 field 21=312，7.4.3.32 保持 377
		pbSint(17, int64(ts)), pbStr(20, "none"), pbSint(21, field21),
		pbBytes(23, env),
		pbStr(24, `{"cmr":16777216,"cmr2":16777216,"un_h":11,"vpn":0,"kd":0,"dyn":"","do":1,"tk":true}`),
	)

	// 混淆：SM3(signKey ‖ nonce ‖ signKey) 作为掩码，与明文异或。
	nonce := randUint32()
	hashInput := make([]byte, 0, len(v5SignKey)*2+4)
	hashInput = append(hashInput, v5SignKey...)
	hashInput = append(hashInput, le32(nonce)...)
	hashInput = append(hashInput, v5SignKey...)
	hash := sm3Sum(hashInput)

	high := (nonce >> 16) & 255
	seed := le32(^(((high << 11) | (nonce >> 24)) ^ (high >> 5) ^ high))

	transformed := make([]byte, 0, len(flags)+len(msg))
	transformed = append(transformed, flags...)
	transformed = append(transformed, xmxor(msg, hash[:])...)
	for i, j := 0, len(transformed)-1; i < j; i, j = i+1, j-1 {
		transformed[i], transformed[j] = transformed[j], transformed[i]
	}
	for i := range transformed {
		transformed[i] ^= seed[(^i)&3]
	}

	check := uint32(0x18000001) | ((uint32(qSm3[0]) & 63) << 14) | ((uint32(f13[0]) & 63) << 8)
	packed := make([]byte, 0, 9+len(transformed)+2)
	packed = append(packed, 0xca)
	packed = append(packed, le32(randUint32())...)
	packed = append(packed, le32(check)...)
	packed = append(packed, transformed...)
	packed = append(packed, byte(nonce>>16), byte(nonce>>24))

	// 密钥与向量都从 signKey 派生，不是固定值。
	aesKey := md5.Sum(v5SignKey[:16])
	aesIV := md5.Sum(v5SignKey[16:32])
	encrypted := newAesV3V5(aesKey[:], ts).encrypt(packed, aesIV[:])

	prefix := append(le32(5), v5MagicBytes...)
	tsLE := le32(ts)
	for i := range prefix {
		prefix[i] ^= tsLE[i&3]
	}

	out := make([]byte, 0, len(prefix)+4+len(encrypted))
	out = append(out, prefix...)
	out = append(out, byte(nonce), byte(nonce>>8), 0, 1)
	out = append(out, encrypted...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// SignPlaybackV5 为红果播放接口生成 v5 签名请求。
//
// 与旧版签名的关键差异：
//   - 请求体必须先 gzip 再签名（x-ss-stub 也按 gzip 后的字节算）
//   - 只携带 x-khronos / x-helios / x-medusa 三个签名头
//   - 票据搜索上界 1024，且只有摘要分支落在 12 时才可签发
//   - 不携带任何 Cookie
func (s *AppSigner) SignPlaybackV5(base string, q v5PlaybackQuery, body []byte, nowMs int64) (string, map[string]string, error) {
	return s.SignPlaybackV5WithExtra(base, q, nil, body, nowMs)
}

// SignPlaybackV5WithExtra 与 SignPlaybackV5 相同，但允许在 _rticket 之前追加接口专属查询参数（如 video_tab）。
func (s *AppSigner) SignPlaybackV5WithExtra(base string, q v5PlaybackQuery, extra [][2]string, body []byte, nowMs int64) (string, map[string]string, error) {
	ts := uint32(nowMs / 1000)
	basePairs := q.orderedPairs()
	if len(extra) > 0 {
		basePairs = append(basePairs, extra...)
	}
	who := q.identity()

	for offset := int64(0); offset <= v5TicketSearchLimit; offset++ {
		ticket := nowMs + offset
		pairs := make([][2]string, 0, len(basePairs)+1)
		pairs = append(pairs, basePairs...)
		pairs = append(pairs, [2]string{"_rticket", strconv.FormatInt(ticket, 10)})

		query := encodeQueryOrdered(pairs)
		if hashBranchV5(query, body, ts) != 0 {
			continue
		}
		fullURL := base + "?" + query
		medusa, err := medusaV5(fullURL, body, ts, who, s.processNonce, s.processStarted)
		if err != nil {
			continue
		}
		headers := map[string]string{
			"User-Agent": v5PlayerUA,
			"Accept":     "application/json; charset=utf-8,application/x-protobuf",
			"x-khronos":  strconv.FormatUint(uint64(ts), 10),
			"x-helios":   signHeliosWith(ts, v5License),
			"x-medusa":   medusa,
		}
		if body != nil {
			headers["Content-Type"] = "application/json; charset=utf-8"
			sum := md5.Sum(body)
			headers["x-ss-stub"] = strings.ToUpper(hex.EncodeToString(sum[:]))
		}
		return fullURL, headers, nil
	}
	return "", nil, errors.New("hongguo: v5 failed to find a signable ticket")
}
