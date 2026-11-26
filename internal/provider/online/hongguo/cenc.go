package hongguo

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"strings"
)

// DecodeAccessKey derives the 16-byte AES-128-CTR ClearKey from a Hongguo
// player_access_key string (XGSecretKey openBox algorithm).
func DecodeAccessKey(playerAccessKey string) ([]byte, error) {
	s := strings.TrimSpace(playerAccessKey)
	if s == "" {
		return nil, errors.New("hongguo: empty player_access_key")
	}
	// Accept URL-safe or standard base64 with or without padding.
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("hongguo: decode player_access_key base64: %w", err)
	}
	if len(raw) < 4 {
		return nil, fmt.Errorf("hongguo: player_access_key too short (%d)", len(raw))
	}

	t := len(raw)
	methodLen := int(raw[0]^raw[1]^raw[2]) - '0'
	if methodLen <= 0 || t-methodLen-1 <= 0 || t-methodLen-2 < 0 {
		return nil, fmt.Errorf("hongguo: invalid player_access_key header (methodLen=%d, len=%d)", methodLen, t)
	}
	convertLen := t - methodLen - 1
	methodBytes := make([]byte, methodLen)
	for n := methodLen; n > 0; n-- {
		methodBytes[methodLen-n] = raw[t-methodLen-2] ^ raw[t-methodLen-1] ^ raw[t-n]
	}
	method := string(methodBytes)

	buf := make([]byte, convertLen)
	copy(buf, raw[1:1+convertLen])

	isApp := method == "11" || method == "appdevice" || method == "app_v2"
	switch method {
	case "11", "22", "appdevice", "webdevice":
		r, a := byte(85), byte(250)
		for o := 0; o < convertLen; o++ {
			cur := buf[o]
			var x byte
			if o%2 == 1 {
				x = r ^ cur
				r = cur
			} else {
				x = a ^ cur
				a = cur
			}
			pc := byte(bits.OnesCount(uint(o)))
			if isApp {
				buf[o] = x - pc - 21
			} else {
				buf[o] = x + pc + 21
			}
		}
	case "app_v2", "web_v2":
		swapAdjacent(buf)
		i, r := byte(85), byte(250)
		for a := 0; a < convertLen; a++ {
			cur := buf[a]
			var l, prev byte
			if a%2 == 1 {
				l = i ^ cur
				i = cur
				prev = r
			} else {
				l = r ^ cur
				r = cur
				prev = i
			}
			pc := byte(bits.OnesCount(uint(a)))
			var v byte
			if isApp {
				v = l - pc - 21
			} else {
				v = l + pc + 21
			}
			buf[a] = v - prev
		}
		swapAdjacent(buf)
	default:
		return nil, fmt.Errorf("hongguo: unsupported player_access_key method %q", method)
	}

	var pad int
	switch n := buf[0]; {
	case n >= '0' && n <= '9':
		pad = int(n - '0')
	case n >= 'a' && n <= 'z':
		pad = int(n-'a') + 10
	default:
		pad = 255
	}
	outLen := convertLen - pad - 1
	if outLen <= 0 || 1+outLen > len(buf) {
		return nil, fmt.Errorf("hongguo: invalid decoded key length %d", outLen)
	}
	hexStr := string(buf[1 : 1+outLen])
	key, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("hongguo: decode key hex %q: %w", hexStr, err)
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("hongguo: unexpected key size %d", len(key))
	}
	return key, nil
}

func swapAdjacent(b []byte) {
	for i := 0; i+1 < len(b); i += 2 {
		b[i], b[i+1] = b[i+1], b[i]
	}
}

// DecryptFMP4 decrypts a CENC-encrypted MP4 buffer (fragmented or plain) in
// place on a copy and rewrites encryption boxes (encv→avc1, enca→mp4a,
// sinf/senc/saiz/saio→free) without changing box sizes or offsets.
//
// Implementation note — the hongguo CDN always places the full CENC index
// (saio + saiz + stbl stco/stsc/stsz) inside the moov header, even when the
// container is fragmented. We compute every encrypted byte's absolute file
// offset from the moov alone — no moof→mdat pairing required.
func DecryptFMP4(src []byte, key []byte) ([]byte, error) {
	out := make([]byte, len(src))
	copy(out, src)

	samples, err := ParseMoovSamples(out)
	if err != nil {
		// moov 级样本表解析失败：分片布局的样本表在 moof 里，改从那里取。
		frag, ferr := ParseFragmentSamples(out, 0)
		if ferr != nil || len(frag) == 0 {
			return nil, err
		}
		samples = frag
	} else if len(samples) == 0 {
		// moov 里没有样本表 —— 分片布局：样本表在每个 moof 的 traf 里。
		frag, ferr := ParseFragmentSamples(out, 0)
		if ferr != nil {
			return nil, ferr
		}
		samples = frag
	}
	if err := DecryptBlock(out, 0, samples, key); err != nil {
		return nil, err
	}
	neutralizeAllProtectionBoxes(out)
	return out, nil
}

// neutralizeAllProtectionBoxes walks every container box in the full file and
// rewrites encv→avc1 / enca→mp4a and sinf/senc/saio/saiz→free in-place.
func neutralizeAllProtectionBoxes(data []byte) {
	var walk func(b []byte)
	walk = func(b []byte) {
		for off := 0; off+8 <= len(b); {
			sz := int(binary.BigEndian.Uint32(b[off : off+4]))
			if sz < 8 || off+sz > len(b) {
				break
			}
			typ := string(b[off+4 : off+8])
			switch typ {
			case "moov", "trak", "mdia", "minf", "stbl", "moof", "traf":
				walk(b[off+8 : off+sz])
			case "stsd":
				// stsd body = [version:1][flags:3][entry_count:4][entry0, entry1, ...]
				// entries are at +16 past the box start.
				if off+16 <= off+sz {
					stripProtectionInStsd(b[off+16 : off+sz])
				}
			case "sinf", "senc", "saio", "saiz":
				copy(b[off+4:off+8], "free")
				for z := off + 8; z < off+sz; z++ {
					b[z] = 0
				}
			}
			off += sz
		}
	}
	walk(data)
}

func stripProtectionInStsd(b []byte) {
	for off := 0; off+8 <= len(b); {
		sz := int(binary.BigEndian.Uint32(b[off : off+4]))
		if sz < 8 || off+sz > len(b) {
			break
		}
		typ := string(b[off+4 : off+8])
		var subOff int
		switch typ {
		case "encv":
			subOff = 86
			codecTag := "avc1"
			if off+subOff <= off+sz {
				codecTag = detectOriginalVideoCodec(b[off+subOff : off+sz])
			}
			copy(b[off+4:off+8], codecTag)
		case "enca":
			copy(b[off+4:off+8], "mp4a")
			subOff = 36
		}
		if subOff > 0 && off+subOff <= off+sz {
			renameChildBox(b[off+subOff:off+sz], "sinf", "free")
		}
		off += sz
	}
}

// detectOriginalVideoCodec inspects child boxes of an `encv` sample entry
// (e.g. avcC, hvcC, av1C, vpcC, or sinf->frma) so that HEVC/bytevc1 streams
// are restored to "hvc1" instead of being mislabeled as "avc1".
func detectOriginalVideoCodec(children []byte) string {
	var hasAVCC, hasHVCC, hasAV1C, hasVPCC bool
	var frmaTag string
	for off := 0; off+8 <= len(children); {
		sz := int(binary.BigEndian.Uint32(children[off : off+4]))
		if sz < 8 || off+sz > len(children) {
			break
		}
		typ := string(children[off+4 : off+8])
		switch typ {
		case "avcC":
			hasAVCC = true
		case "hvcC":
			hasHVCC = true
		case "av1C":
			hasAV1C = true
		case "vpcC":
			hasVPCC = true
		case "sinf":
			frmaTag = readFrmaFormat(children[off+8 : off+sz])
		}
		off += sz
	}
	switch {
	case hasHVCC || frmaTag == "hvc1" || frmaTag == "hev1":
		// Apple AVFoundation / WKWebView requires "hvc1" (not "hev1") for MP4 HEVC.
		return "hvc1"
	case hasAVCC || frmaTag == "avc1" || frmaTag == "avc3":
		return "avc1"
	case hasAV1C || frmaTag == "av01":
		return "av01"
	case hasVPCC || frmaTag == "vp09":
		return "vp09"
	case len(frmaTag) == 4:
		return frmaTag
	default:
		return "avc1"
	}
}

func readFrmaFormat(sinfBody []byte) string {
	for off := 0; off+8 <= len(sinfBody); {
		sz := int(binary.BigEndian.Uint32(sinfBody[off : off+4]))
		if sz < 8 || off+sz > len(sinfBody) {
			break
		}
		if string(sinfBody[off+4:off+8]) == "frma" && off+12 <= off+sz {
			return string(sinfBody[off+8 : off+12])
		}
		off += sz
	}
	return ""
}

// renameChildBox 把指定的子 box 改名，并清零它的内容。
//
// 清零是必要的：sinf 改名成 free 之后，里面的 tenc 等加密声明仍以字节形式留在
// 流里。播放器会忽略 free box 的内容（不影响播放），但"流里还带着加密声明"
// 这件事会让验收判据失真 —— 残留检测分不清"free box 里的历史字节"和
// "真正还生效的加密声明"。清零不改变 box 大小与偏移。
func renameChildBox(b []byte, from, to string) {
	for off := 0; off+8 <= len(b); {
		sz := int(binary.BigEndian.Uint32(b[off : off+4]))
		if sz < 8 || off+sz > len(b) {
			break
		}
		if string(b[off+4:off+8]) == from {
			copy(b[off+4:off+8], to)
			for z := off + 8; z < off+sz; z++ {
				b[z] = 0
			}
		}
		off += sz
	}
}

// --- Audio/video fMP4 muxing (fallback path only — AppResolve already returns
// a single audio+video stream; this is used when the fallback PlayInfo path
// kicks in and the CDN returns separate audio/video DASH tracks).

// MuxFMP4 combines a decrypted video fMP4 and a decrypted audio fMP4 into a
// single two-track fragmented MP4 stream (track 1 = video, track 2 = audio).
func MuxFMP4(videoData, audioData []byte) ([]byte, error) {
	if len(audioData) == 0 {
		return videoData, nil
	}
	vFtyp, vMoov, vFrags := splitFMP4(videoData)
	_, aMoov, aFrags := splitFMP4(audioData)
	if vMoov == nil || aMoov == nil || len(vFrags) == 0 {
		return videoData, nil
	}

	combinedMoov, err := mergeMoov(vMoov, aMoov)
	if err != nil {
		return videoData, nil
	}

	var out bytes.Buffer
	out.Grow(len(videoData) + len(audioData))
	out.Write(vFtyp)
	out.Write(combinedMoov)

	maxLen := len(vFrags)
	if len(aFrags) > maxLen {
		maxLen = len(aFrags)
	}
	for i := 0; i < maxLen; i++ {
		if i < len(vFrags) {
			out.Write(vFrags[i])
		}
		if i < len(aFrags) {
			frag := make([]byte, len(aFrags[i]))
			copy(frag, aFrags[i])
			rewriteMoofTrackID(frag, 2)
			out.Write(frag)
		}
	}
	return out.Bytes(), nil
}

func splitFMP4(data []byte) (ftyp, moov []byte, frags [][]byte) {
	var curMoof []byte
	for off := 0; off+8 <= len(data); {
		sz := int(binary.BigEndian.Uint32(data[off : off+4]))
		if sz < 8 || off+sz > len(data) {
			break
		}
		box := data[off : off+sz]
		typ := string(box[4:8])
		switch typ {
		case "ftyp":
			ftyp = box
		case "moov":
			moov = box
		case "moof":
			curMoof = box
		case "mdat":
			if curMoof != nil {
				frag := make([]byte, len(curMoof)+len(box))
				copy(frag, curMoof)
				copy(frag[len(curMoof):], box)
				frags = append(frags, frag)
				curMoof = nil
			}
		}
		off += sz
	}
	return ftyp, moov, frags
}

func mergeMoov(vMoov, aMoov []byte) ([]byte, error) {
	var mvhd, vTrak, vMvex []byte
	var aTrak, aMvex []byte

	for _, b := range childBoxes(vMoov[8:]) {
		switch string(b[4:8]) {
		case "mvhd":
			mvhd = append([]byte(nil), b...)
		case "trak":
			if vTrak == nil {
				vTrak = append([]byte(nil), b...)
			}
		case "mvex":
			vMvex = b
		}
	}
	for _, b := range childBoxes(aMoov[8:]) {
		switch string(b[4:8]) {
		case "trak":
			if aTrak == nil {
				aTrak = append([]byte(nil), b...)
			}
		case "mvex":
			aMvex = b
		}
	}
	if mvhd == nil || vTrak == nil || aTrak == nil || vMvex == nil || aMvex == nil {
		return nil, errors.New("hongguo: incomplete moov boxes")
	}

	if len(mvhd) >= 12 {
		ver := mvhd[8]
		nextIDOff := len(mvhd) - 4
		if ver == 0 && len(mvhd) >= 108 {
			nextIDOff = 104
		} else if ver == 1 && len(mvhd) >= 120 {
			nextIDOff = 116
		}
		binary.BigEndian.PutUint32(mvhd[nextIDOff:nextIDOff+4], 3)
	}

	rewriteTrakID(aTrak, 2)

	var mvexBody bytes.Buffer
	for _, b := range childBoxes(vMvex[8:]) {
		mvexBody.Write(b)
	}
	for _, b := range childBoxes(aMvex[8:]) {
		if string(b[4:8]) == "trex" && len(b) >= 16 {
			trex := append([]byte(nil), b...)
			binary.BigEndian.PutUint32(trex[12:16], 2)
			mvexBody.Write(trex)
		}
	}
	mvex := wrapBox("mvex", mvexBody.Bytes())

	var moovBody bytes.Buffer
	moovBody.Write(mvhd)
	moovBody.Write(vTrak)
	moovBody.Write(aTrak)
	moovBody.Write(mvex)
	return wrapBox("moov", moovBody.Bytes()), nil
}

func childBoxes(b []byte) [][]byte {
	var out [][]byte
	for off := 0; off+8 <= len(b); {
		sz := int(binary.BigEndian.Uint32(b[off : off+4]))
		if sz < 8 || off+sz > len(b) {
			break
		}
		out = append(out, b[off:off+sz])
		off += sz
	}
	return out
}

func wrapBox(typ string, body []byte) []byte {
	out := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(out)))
	copy(out[4:8], typ)
	copy(out[8:], body)
	return out
}

func rewriteTrakID(trak []byte, id uint32) {
	for off := 8; off+8 <= len(trak); {
		sz := int(binary.BigEndian.Uint32(trak[off : off+4]))
		if sz < 8 || off+sz > len(trak) {
			break
		}
		if string(trak[off+4:off+8]) == "tkhd" && sz >= 24 {
			ver := trak[off+8]
			idOff := off + 20
			if ver == 1 {
				idOff = off + 28
			}
			if idOff+4 <= off+sz {
				binary.BigEndian.PutUint32(trak[idOff:idOff+4], id)
			}
			return
		}
		off += sz
	}
}

func rewriteMoofTrackID(frag []byte, id uint32) {
	if len(frag) < 8 || string(frag[4:8]) != "moof" {
		return
	}
	moofSz := int(binary.BigEndian.Uint32(frag[0:4]))
	if moofSz > len(frag) {
		return
	}
	for off := 8; off+8 <= moofSz; {
		sz := int(binary.BigEndian.Uint32(frag[off : off+4]))
		if sz < 8 || off+sz > moofSz {
			break
		}
		if string(frag[off+4:off+8]) == "traf" {
			for tOff := off + 8; tOff+8 <= off+sz; {
				tSz := int(binary.BigEndian.Uint32(frag[tOff : tOff+4]))
				if tSz < 8 || tOff+tSz > off+sz {
					break
				}
				if string(frag[tOff+4:tOff+8]) == "tfhd" && tSz >= 16 {
					binary.BigEndian.PutUint32(frag[tOff+12:tOff+16], id)
					return
				}
				tOff += tSz
			}
		}
		off += sz
	}
}
