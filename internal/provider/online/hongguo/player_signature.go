package hongguo

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"sort"
	"strings"
	"time"
)

func playerProto(fields []struct {
	tag int
	val any
}) []byte {
	var buf bytes.Buffer
	writeVar := func(v uint64) {
		for v>>7 != 0 {
			buf.WriteByte(byte((v & 127) | 128))
			v >>= 7
		}
		buf.WriteByte(byte(v))
	}
	for _, f := range fields {
		switch x := f.val.(type) {
		case int:
			writeVar(uint64(f.tag << 3))
			writeVar(uint64(x))
		case int64:
			writeVar(uint64(f.tag << 3))
			writeVar(uint64(x))
		case uint64:
			writeVar(uint64(f.tag << 3))
			writeVar(x)
		case []byte:
			writeVar(uint64((f.tag << 3) | 2))
			writeVar(uint64(len(x)))
			buf.Write(x)
		case string:
			b := []byte(x)
			writeVar(uint64((f.tag << 3) | 2))
			writeVar(uint64(len(b)))
			buf.Write(b)
		}
	}
	return buf.Bytes()
}

func pkcs7Pad16(b []byte) []byte {
	pad := 16 - (len(b) % 16)
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func signPlayerLadon(ts uint64, salt []byte) string {
	in := make([]byte, 0, len(salt)+4)
	in = append(in, salt...)
	in = append(in, []byte("8662")...)
	sum := md5.Sum(in)
	ascii := []byte(hex.EncodeToString(sum[:]))

	w := func(off int) uint64 { return binary.LittleEndian.Uint64(ascii[off : off+8]) }
	queue := [2]uint64{w(16), w(24)}
	left := w(0)
	right := w(8)
	var keys [34]uint64
	for i := 0; i < 34; i++ {
		keys[i] = left
		next := (bits.RotateLeft64(right, -8) + left) ^ uint64(i)
		left = next ^ bits.RotateLeft64(left, 3)
		right = queue[0]
		queue[0] = queue[1]
		queue[1] = next
	}

	data := pkcs7Pad16([]byte(fmt.Sprintf("%d-1611921764-8662", ts)))
	var out bytes.Buffer
	out.Write(salt)
	for at := 0; at < len(data); at += 16 {
		a := binary.LittleEndian.Uint64(data[at : at+8])
		b := binary.LittleEndian.Uint64(data[at+8 : at+16])
		for _, k := range keys {
			b = k ^ (a + bits.RotateLeft64(b, -8))
			a = b ^ bits.RotateLeft64(a, 3)
		}
		out.Write(le64(a))
		out.Write(le64(b))
	}
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

func signPlayerArgus(query string, body []byte, deviceID string, ts uint64, nonce []byte) string {
	rnd := uint64(binary.LittleEndian.Uint32(nonce[6:10])) & 0x7fffffff
	nested := playerProto([]struct {
		tag int
		val any
	}{
		{1, 1}, {2, 1}, {3, 1}, {7, uint64(3348294860)},
	})
	env := playerProto([]struct {
		tag int
		val any
	}{
		{1, "25053RT47C"}, {2, "16"}, {3, "update_64"}, {4, uint64(2147483648)},
	})
	var bMd5 [16]byte
	if body != nil {
		bMd5 = md5.Sum(body)
	}
	bSm3 := sm3Sum(bMd5[:])
	qSm3 := sm3Sum([]byte(query))
	msg := playerProto([]struct {
		tag int
		val any
	}{
		{1, 0x40401252}, {2, 2}, {3, rnd}, {4, "8662"}, {5, deviceID},
		{6, "1611921764"}, {7, "7.3.5.32"}, {8, "v04.07.01-ml-android"}, {9, 135135744},
		{10, make([]byte, 8)}, {11, 0}, {12, ts * 2},
		{13, bSm3[:6]}, {14, qSm3[:6]}, {15, nested}, {16, []byte{}},
		{20, "none"}, {21, 738}, {23, env}, {25, 2},
	})

	secret, _ := hex.DecodeString("ac1adaae95a7af94a5114ab3b3a97dd80050aa0a39314c40528caec95256c28c")
	mid, _ := hex.DecodeString("3ccc567b")
	derived := sm3Sum(append(append(append([]byte{}, secret...), mid...), secret...))
	var keys [72]uint64
	for i := 0; i < 4; i++ {
		keys[i] = binary.LittleEndian.Uint64(derived[i*8 : i*8+8])
	}
	for i := 4; i < 72; i++ {
		x := bits.RotateLeft64(keys[i-1], -3) ^ keys[i-3]
		keys[i] = (^keys[i-4]) ^ x ^ bits.RotateLeft64(x, -1) ^ ((uint64(0x3dc94c3a046d678b) >> ((i - 4) % 62)) & 1) ^ 3
	}

	blocks := pkcs7Pad16(msg)
	var enc bytes.Buffer
	for at := 0; at < len(blocks); at += 16 {
		a := binary.LittleEndian.Uint64(blocks[at : at+8])
		b := binary.LittleEndian.Uint64(blocks[at+8 : at+16])
		for _, k := range keys {
			next := a ^ (bits.RotateLeft64(b, 1) & bits.RotateLeft64(b, 8)) ^ bits.RotateLeft64(b, 2) ^ k
			a = b
			b = next
		}
		enc.Write(le64(a))
		enc.Write(le64(b))
	}

	mask, _ := hex.DecodeString("d04ffdffd04ffdff")
	encBytes := enc.Bytes()
	scrambled := make([]byte, len(encBytes))
	for i, b := range encBytes {
		scrambled[i] = b ^ mask[i%8]
	}
	combined := append(append([]byte{}, mask...), scrambled...)
	for i, j := 0, len(combined)-1; i < j; i, j = i+1, j-1 {
		combined[i], combined[j] = combined[j], combined[i]
	}
	hdr, _ := hex.DecodeString("a6e783ee7001100918")
	trl, _ := hex.DecodeString("567b")
	container := append(append(hdr, combined...), trl...)

	aesKey := md5.Sum(secret[:16])
	aesIV := md5.Sum(secret[16:32])
	blk, _ := aes.NewCipher(aesKey[:])
	paddedContainer := pkcs7Pad16(container)
	ct := make([]byte, len(paddedContainer))
	cipher.NewCBCEncrypter(blk, aesIV[:]).CryptBlocks(ct, paddedContainer)

	prefix, _ := hex.DecodeString("3ccc")
	return base64.StdEncoding.EncodeToString(append(prefix, ct...))
}

// SingleVideoHeaders builds X-Gorgon/X-Khronos/X-Argus/X-Ladon/X-SS-STUB for /novel/player/video_model/v1/.
func SingleVideoHeaders(query string, body []byte, deviceID string, nowMs int64) map[string]string {
	var nonce [11]byte
	_, _ = rand.Read(nonce[:])
	ts := uint64(nowMs / 1000)

	qMd5 := md5.Sum([]byte(query))
	var bMd5 [16]byte
	if body != nil {
		bMd5 = md5.Sum(body)
	}
	tail, _ := hex.DecodeString("00060b1c")
	input := make([]byte, 0, 20)
	input = append(input, qMd5[:4]...)
	input = append(input, bMd5[:4]...)
	input = append(input, 0, 0, 0, 0)
	input = append(input, tail...)
	input = append(input, be32(uint32(ts))...)

	key, _ := hex.DecodeString("44b9b9d9a4aef9fca493aa757ca3c2c4a496938f")
	for i := range input {
		input[i] ^= key[i]
	}
	for i := range input {
		v := input[i]
		swapped := (v << 4) | (v >> 4)
		next := input[(i+1)%len(input)]
		input[i] = bits.Reverse8(swapped^next) ^ 0xeb
	}
	gorgonPre, _ := hex.DecodeString("8404401c0000")
	h := map[string]string{
		"X-Gorgon":        hex.EncodeToString(append(gorgonPre, input...)),
		"X-Khronos":       fmt.Sprintf("%d", ts),
		"X-SS-Req-Ticket": fmt.Sprintf("%d", nowMs),
		"X-Argus":         signPlayerArgus(query, body, deviceID, ts, nonce[:]),
		"X-Ladon":         signPlayerLadon(ts, nonce[2:6]),
	}
	if body != nil {
		stub := md5.Sum(body)
		h["X-SS-STUB"] = strings.ToUpper(hex.EncodeToString(stub[:]))
	}
	return h
}

func sortedDeviceQuery(dev *RegisteredDevice, nowMs int64) string {
	m := map[string]string{
		"ac":                    "wifi",
		"aid":                   "8662",
		"app_name":              "novelread",
		"channel":               "update_64",
		"device_brand":          "Redmi",
		"device_id":             dev.DeviceID,
		"device_platform":       "android",
		"device_type":           "25053RT47C",
		"dpi":                   "520",
		"iid":                   dev.InstallID,
		"language":              "zh",
		"manifest_version_code": "73532",
		"os":                    "android",
		"os_api":                "36",
		"os_version":            "16",
		"resolution":            "1280*2772",
		"ssmix":                 "a",
		"update_version_code":   "73532",
		"version_code":          "73532",
		"version_name":          "7.3.5.32",
		"_rticket":              fmt.Sprintf("%d", nowMs),
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([][2]string, len(keys))
	for i, k := range keys {
		pairs[i] = [2]string{k, m[k]}
	}
	return encodeQueryOrdered(pairs)
}

func currentMillis() int64 { return time.Now().UnixMilli() }
