package hongguo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestDecodeAccessKey(t *testing.T) {
	got, err := DecodeAccessKey("0eYFtieVOtsklwWLTsBX2kiXV4wakgWPTZFUi0iqULRG5FyKig==")
	if err != nil {
		t.Fatalf("DecodeAccessKey failed: %v", err)
	}
	want := "ff994e6b83cb128c62e575a51259170f"
	if hex.EncodeToString(got) != want {
		t.Fatalf("got %x, want %s", got, want)
	}
}

func TestStripProtectionInStsdRestoresCodec(t *testing.T) {
	buildEncv := func(childBox string, frma string) []byte {
		// Minimal encv box: 86-byte visual sample entry header + child codec box + sinf(frma)
		childLen := 12
		frmaBox := make([]byte, 12)
		binary.BigEndian.PutUint32(frmaBox[0:4], 12)
		copy(frmaBox[4:8], "frma")
		copy(frmaBox[8:12], frma)

		sinfBox := make([]byte, 8+len(frmaBox))
		binary.BigEndian.PutUint32(sinfBox[0:4], uint32(len(sinfBox)))
		copy(sinfBox[4:8], "sinf")
		copy(sinfBox[8:], frmaBox)

		total := 86 + childLen + len(sinfBox)
		encv := make([]byte, total)
		binary.BigEndian.PutUint32(encv[0:4], uint32(total))
		copy(encv[4:8], "encv")
		binary.BigEndian.PutUint32(encv[86:90], uint32(childLen))
		copy(encv[90:94], childBox)
		copy(encv[86+childLen:], sinfBox)
		return encv
	}

	hevc := buildEncv("hvcC", "hev1")
	stripProtectionInStsd(hevc)
	if got := string(hevc[4:8]); got != "hvc1" {
		t.Fatalf("HEVC encv rewritten to %q, want hvc1", got)
	}

	avc := buildEncv("avcC", "avc1")
	stripProtectionInStsd(avc)
	if got := string(avc[4:8]); got != "avc1" {
		t.Fatalf("AVC encv rewritten to %q, want avc1", got)
	}
}

// TestDecryptBlockKeepsKeystreamContinuousAcrossChunks 锁定 CTR keystream
// 在样本内连续推进这条不变量。
//
// 真实场景：Range 代理按 1MiB 切块拉 mdat，一个样本完全可能跨块边界。
// 如果解密某个区间时没有先把 keystream 推进到该区间的起点，后半段会用
// 错误的计数器解出乱码 —— 表现是播放到某处开始花屏。
//
// 判据：分块解密的结果必须与一次性解密逐字节相同。
func TestDecryptBlockKeepsKeystreamContinuousAcrossChunks(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 11, 22, 33, 44, 55, 66}

	// 样本里前 7 字节是明文前缀（subsample clear），后面 40 字节是密文。
	const clearLen = 7
	const encLen = 40
	const sampleStart = 1000

	plain := make([]byte, clearLen+encLen)
	for i := range plain {
		plain[i] = byte(i*7 + 3)
	}

	// 用 CTR 加密密文部分。CENC 里 keystream 在样本内按字节位置连续推进，
	// 明文前缀虽然不被 XOR，但同样占用 keystream 位置 —— 所以要先跳过它。
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	enc := make([]byte, encLen)
	ctr := cipher.NewCTR(block, iv[:])
	skip := make([]byte, clearLen)
	ctr.XORKeyStream(skip, skip)
	ctr.XORKeyStream(enc, plain[clearLen:])

	// 组装"文件"：样本前是明文前缀，之后是密文
	file := make([]byte, sampleStart+clearLen+encLen)
	copy(file[sampleStart:], plain[:clearLen])
	copy(file[sampleStart+clearLen:], enc)

	samples := []CencSampleRange{{
		Start:  int64(sampleStart + clearLen),
		End:    int64(sampleStart + clearLen + encLen),
		IV:     iv,
		EncOff: int64(clearLen),
	}}

	// 基准：一次性解密整个样本
	whole := make([]byte, len(file))
	copy(whole, file)
	if err := DecryptBlock(whole, 0, samples, key); err != nil {
		t.Fatalf("DecryptBlock(whole): %v", err)
	}
	want := whole[sampleStart+clearLen : sampleStart+clearLen+encLen]
	if string(want) != string(plain[clearLen:]) {
		t.Fatalf("一次性解密结果不对:\n得到 %x\n期望 %x", want, plain[clearLen:])
	}

	// 分块：在密文中间切开，两次调用解出前后两半
	split := int64(sampleStart + clearLen + 13) // 故意切在样本内部
	chunked := make([]byte, len(file))
	copy(chunked, file)
	if err := DecryptBlock(chunked[:split], 0, samples, key); err != nil {
		t.Fatalf("DecryptBlock(head): %v", err)
	}
	if err := DecryptBlock(chunked[split:], split, samples, key); err != nil {
		t.Fatalf("DecryptBlock(tail): %v", err)
	}
	got := chunked[sampleStart+clearLen : sampleStart+clearLen+encLen]
	if string(got) != string(want) {
		t.Fatalf("分块解密与一次性解密不一致 —— keystream 没有连续推进:\n分块 %x\n整体 %x", got, want)
	}
}

func TestParseMoovSamples16ByteIV(t *testing.T) {
	// Build minimal ftyp + moov(trak(stbl(tenc(ivSize=16), stsz, stsc, stco, saiz, saio, senc)))
	makeBox := func(typ string, body []byte) []byte {
		out := make([]byte, 8+len(body))
		out[0], out[1], out[2], out[3] = byte((8+len(body))>>24), byte((8+len(body))>>16), byte((8+len(body))>>8), byte(8+len(body))
		copy(out[4:8], typ)
		copy(out[8:], body)
		return out
	}
	u32 := func(v uint32) []byte {
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	}
	u16 := func(v uint16) []byte {
		return []byte{byte(v >> 8), byte(v)}
	}

	ftyp := makeBox("ftyp", []byte("isom\x00\x00\x02\x00isom"))
	// tenc: fullbox(4) + reserved(2) + isEncrypted(1)=1 + ivSize(1)=16 + kid(16)
	tencBody := make([]byte, 24)
	tencBody[6] = 1
	tencBody[7] = 16
	tenc := makeBox("tenc", tencBody)

	// stsz: fullbox(4) + defSize(4)=64 + count(4)=1
	stsz := makeBox("stsz", append(append(make([]byte, 4), u32(64)...), u32(1)...))
	// stsc: fullbox(4) + count(4)=1 + (first=1, count=1, sdi=1)
	stscBody := make([]byte, 4)
	stscBody = append(stscBody, u32(1)...)
	stscBody = append(stscBody, u32(1)...)
	stscBody = append(stscBody, u32(1)...)
	stscBody = append(stscBody, u32(1)...)
	stsc := makeBox("stsc", stscBody)
	// stco: fullbox(4) + count(4)=1 + offset(4)=500
	stco := makeBox("stco", append(append(make([]byte, 4), u32(1)...), u32(500)...))
	// saiz: fullbox(4) + defSize(1)=24 (16 IV + 2 subs + 6*1) + count(4)=1
	saizBody := append(make([]byte, 4), byte(24))
	saizBody = append(saizBody, u32(1)...)
	saiz := makeBox("saiz", saizBody)
	// senc record: 16-byte IV + subs=1 + clear=10 + enc=54
	iv := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	sencRec := append([]byte{}, iv...)
	sencRec = append(sencRec, u16(1)...)
	sencRec = append(sencRec, u16(10)...)
	sencRec = append(sencRec, u32(54)...)
	senc := makeBox("senc", append(append(u32(2), u32(1)...), sencRec...))

	// First assemble stbl with placeholder saio=0, then compute exact offset of sencRec inside header
	saioPlaceholder := makeBox("saio", append(append(make([]byte, 4), u32(1)...), u32(0)...))
	stblBody := append(append(append(append(append(append([]byte{}, tenc...), stsz...), stsc...), stco...), saiz...), saioPlaceholder...)
	stblBody = append(stblBody, senc...)
	stbl := makeBox("stbl", stblBody)
	trak := makeBox("trak", stbl)
	moov := makeBox("moov", trak)
	header := append(append([]byte{}, ftyp...), moov...)

	// Locate sencRec inside header: len(header) - len(sencRec)
	sencRecOff := uint32(len(header) - len(sencRec))
	saioReal := makeBox("saio", append(append(make([]byte, 4), u32(1)...), u32(sencRecOff)...))
	stblBody = append(append(append(append(append(append([]byte{}, tenc...), stsz...), stsc...), stco...), saiz...), saioReal...)
	stblBody = append(stblBody, senc...)
	stbl = makeBox("stbl", stblBody)
	trak = makeBox("trak", stbl)
	moov = makeBox("moov", trak)
	header = append(append([]byte{}, ftyp...), moov...)

	samples, err := ParseMoovSamples(header)
	if err != nil {
		t.Fatalf("ParseMoovSamples: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}
	if samples[0].Start != 510 || samples[0].End != 564 {
		t.Fatalf("range = [%d,%d), want [510,564)", samples[0].Start, samples[0].End)
	}
	if string(samples[0].IV[:]) != string(iv) {
		t.Fatalf("IV = %x, want %x", samples[0].IV, iv)
	}
}
