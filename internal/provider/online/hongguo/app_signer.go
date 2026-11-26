package hongguo

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net/url"
	"strings"
	"time"
)

const sboxBase64 = "+n0Ia5xZs0sEXznQOEqRmQBnpiCf9U2Ccybu3xhmgzOAAxn72f6uqqmwUsYL83klTni0NqxdGieeiNu9PGPsSRXBMB/cuFbUbM3KCUPINaPvHvSW0vwOcnuUhNHqRVpiAj/TEoE0K91+5ijypUYTATsh9mE3KSoN7Yyvv51cuyR2D3XkU4nhmI2xmmVwT1RMWKtub4sjxAcRDLrPoKSO2AU9FLLadMPX577Wf95IFj6FkKFVt3dCIsmGUC4X+WQxLJvxbRxEaOPpqJOXyzJX6+Vxaq3AzMfF/WAdoi1Hp+JRaV56zgpBtpWP97mH4DoGEIq1+FvV8LyS/3wvwugbQOwb2r26mJEMsiuDQTRn+wrYdrVGBVlhI3WQhyrjUBVMrLF5667llUcEaPCGPVGLD8qO5LlO8hKCvA7V9+8oJc9bXelqVQLhM76T5/WtnT45JKji+hdX0HoNCDDWuKON/QeaxB5uImSX0h2wv0VmP2zd2yeApxHcpsVS+MC2yFwAc2B7oBkTqsk1SEvTpM2fmfMQREBUfin0Bh+iq6EvPPavhWI2IX9e3yAas7Tm/3KEj2UmlFp36kN4x0rMLBRrxuh0U/zUHM4xcAMYjJY4MonxOl/X+alpt2M3WMI7w3HLnpIBigtNiJu7T21v4P6lSd5WFgntnMEt7oF92XzRLkJbTcGmXepE/UVOG6E/0YnhfS+q26utWcuxzpooyeD2cDlK1/8w9d28VzsRjbLuALbmGlp8+d7EzS6Au7lMpZ+ECMZvQmzwJ+eLOpxR+2chdUExp8ogQyq3v9l68rX4jCwjg0+PYKAEEzcU4wHFY2ZcdIHfWL1okD3SszT0GZMyKdZJrg1L2AeerB4tC0C4crp2EHGo5FYdSP7lwkeR2ocmnR+Ia8CYviUJlzOjhRZef9xuVOn3qcjow3fQgivsAmKKkg4+sA8F8/GWeDiGNhg8JM8KtFPMYWWkx5TVFX5t73kiNRJqjlIGVXtGZFCV4gzt0xcDopuZ6xz8r9Rzafpf9ywev8jh8592gHFIqpStZPuJxmDDMrNN0uBE3V+oscdoIzTJbRJ/t+sVvqnReJOgDJKk10fjisJwqyZBmnmn2BSFj8BvVtCMEbkuPOKdzw7eA11GPs04Qw8zWtkaZWwiO/wwpojqN6K0jY5RnNZA7vn4hPSul+nKCkVnVwQvg1zVxcSCtqORmB9KrJaBbssbCQivGJVJfVTt+hYxOtq4ZvWl8f4QAQZ0zGPffCgl9s6yT4vlvIdpu4YhBwA25wtQWZsc6GJYGWHyvSdeuh3mmUI9DSq13Fsp8C1MU3tqc04/df9LoTUXVXI5INOw/e8C7Hd+5CvbkMEFnnrUUmskEw=="

var hashTable = [64]uint32{
	0xebb64faf, 0x07aadcc2, 0xcf3187bf, 0xe01138ff,
	0x6d0bfcff, 0x5a30a3be, 0xb41ad638, 0x34180eb8,
	0xf233eb6f, 0xb1a584cc, 0xccc30dc7, 0x47d1db51,
	0xd55653de, 0x70a84fa1, 0x57473c12, 0xf76f0288,
	0x2c077f0a, 0xda0dcad0, 0xfbb86f6c, 0xfdc4cf00,
	0x688a020d, 0xe676c6a6, 0x8cd6338b, 0x1a3c8d0e,
	0xcce8b06b, 0x6ad0ed0b, 0xa0522717, 0xdc71ac83,
	0x2285db71, 0xd5b4dda6, 0x736f8650, 0x6560306c,
	0x617ce2a6, 0xe423417e, 0x0a40e143, 0x544e4032,
	0x88dffb2a, 0x716c1ae0, 0x4c467a88, 0x05b23bb3,
	0xe1d0b866, 0xbaa3dcb8, 0xae3374d3, 0xc3381a50,
	0x1702f75b, 0xfe6da368, 0xf0b4cf48, 0x4e0ffbb8,
	0x72aad10d, 0x26c53a3d, 0xf2bce0f6, 0xb4557581,
	0x4a257fdd, 0x8c3182a2, 0xab0b3b86, 0x3d5dfb14,
	0x4f103634, 0xd37b52d7, 0x444eff16, 0xeb0a33d1,
	0x6ca86f6e, 0x00284ba7, 0x08387cfa, 0x5fb37586,
}

var sboxBytes []byte

func init() {
	b, err := base64.StdEncoding.DecodeString(sboxBase64)
	if err != nil || len(b) < 1024 {
		panic("invalid sbox")
	}
	sboxBytes = b
}

type AppSigner struct {
	processNonce   string
	processStarted int64
}

func NewAppSigner() *AppSigner {
	var b [18]byte
	_, _ = rand.Read(b[:])
	return &AppSigner{
		processNonce:   base64.RawURLEncoding.EncodeToString(b[:]),
		processStarted: time.Now().UnixMilli(),
	}
}

func randUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.LittleEndian.Uint32(b[:])
}

func le32(v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return b[:]
}

func be32(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

func le64(v uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return b[:]
}

func xorBytes(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i%len(b)]
	}
	return out
}

func appendVarint(buf *bytes.Buffer, v uint64) {
	for v > 127 {
		buf.WriteByte(byte((v & 127) | 128))
		v >>= 7
	}
	buf.WriteByte(byte(v))
}

type pbField struct {
	tag  int
	kind string // "sint", "float", "bytes"
	ival int64
	fval float32
	bval []byte
}

func pbSint(tag int, v int64) pbField    { return pbField{tag: tag, kind: "sint", ival: v} }
func pbFloat(tag int, v float32) pbField { return pbField{tag: tag, kind: "float", fval: v} }
func pbStr(tag int, v string) pbField    { return pbField{tag: tag, kind: "bytes", bval: []byte(v)} }
func pbBytes(tag int, v []byte) pbField  { return pbField{tag: tag, kind: "bytes", bval: v} }

func encodeProto(fields ...pbField) []byte {
	var buf bytes.Buffer
	for _, f := range fields {
		switch f.kind {
		case "sint":
			appendVarint(&buf, uint64(f.tag)<<3)
			var zz uint64
			if f.ival < 0 {
				zz = uint64(-f.ival*2 - 1)
			} else {
				zz = uint64(f.ival * 2)
			}
			appendVarint(&buf, zz)
		case "float":
			appendVarint(&buf, (uint64(f.tag)<<3)|5)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(f.fval))
			buf.Write(b[:])
		default:
			if len(f.bval) > 0 {
				appendVarint(&buf, (uint64(f.tag)<<3)|2)
				appendVarint(&buf, uint64(len(f.bval)))
				buf.Write(f.bval)
			}
		}
	}
	return buf.Bytes()
}

func getIv(initial uint32, data []byte) uint32 {
	v := initial
	for i, b := range data {
		if i&1 == 0 {
			v = (v >> 4) ^ v ^ (v << 6) ^ uint32(b)
		} else {
			v = ^((v >> 7) ^ v ^ (uint32(b) | (v << 12)))
		}
	}
	return v
}

func calcIV(query, body []byte, ts uint32) uint32 {
	return getIv(getIv(getIv(0x20230928, query), body), le32(ts))
}

func hashBranch(query string, body []byte, ts uint32) int {
	qSm3 := sm3Sum([]byte(query))
	var bMd5 [16]byte
	if body != nil {
		bMd5 = md5.Sum(body)
	}
	v := calcIV(qSm3[:], bMd5[:], ts)
	return int((v & 15) % 3)
}

func sumMd5(data []byte) uint32 {
	check := uint32(0x20220420)
	for i := 0; i < 12; i++ {
		shift := 5
		if i&1 == 0 {
			shift = 3
		}
		temp := (check >> shift) ^ check
		if i&1 == 0 {
			check = uint32(data[i]) ^ (check << 7)
		} else {
			check = ^(uint32(data[i]) | (check << 11))
		}
		check ^= temp
	}
	return (check | 4) ^ 0x1000000
}

// hashF13 是旧版（v3）分支的摘要入口。
// 轮函数与其他分支一致，差异只在准入条件、种子、轮表选择与校验和附加位。
func hashF13(query, body []byte, ts uint32) ([]byte, error) {
	v := calcIV(query, body, ts)
	if (v&15)%3 != 0 {
		return nil, errors.New("unsupported hash branch")
	}
	index := (v & 15) / 3
	seeds := [6]uint32{0xc4a78580, 0xb3c0fd39, 0xc58c5686, 0xc9aa3ba7, 0xf5a7adf2, 0x963c2ed1}
	chosenAll := [6][11]int{
		{101, 5, 7, 6, 3, 2, 1, 0, 5, 4, 3},
		{96, 0, 6, 7, 5, 3, 2, 1, 5, 4, 4},
		{96, 7, 6, 2, 1, 4, 0, 5, 4, 3, 5},
		{99, 3, 6, 2, 4, 5, 1, 0, 0, 7, 6},
		{96, 0, 5, 6, 7, 3, 1, 2, 5, 4, 4},
		{100, 2, 0, 3, 5, 4, 6, 7, 2, 1, 5},
	}
	return hashF13Spec(query, body, ts, seeds[index], chosenAll[index], 0)
}

// hashF13Spec 是各分支共用的摘要轮函数。
//
// seed 同时充当两处角色：轮次计数 count 的起点，以及每轮取表的基址。
// chosen 是 11 个下标的取值表，含义见轮函数内部。
// checksumBit 追加在校验和上，用来标记摘要所属的协议版本。
func hashF13Spec(query, body []byte, ts uint32, seed uint32, chosen [11]int, checksumBit uint32) ([]byte, error) {
	count := seed + ts

	var tt [64]uint32
	rotCount := int((count + 1) & 255)
	for i := 0; i < 64; i++ {
		tt[i] = bits.RotateLeft32(hashTable[i], -rotCount)
	}

	shift := (count + 2) & 7
	padSrc := [4]int{0xfa, 0x45, 0x61, 0xd7}
	var pad [4]byte
	for i, p := range padSrc {
		pad[i] = byte((p | (p << 8)) >> shift)
	}
	tail, _ := hex.DecodeString("00000000000001a0")

	data := make([]byte, 0, len(query)+len(body)+16)
	data = append(data, query...)
	data = append(data, body...)
	data = append(data, le32(ts)...)
	data = append(data, pad[:]...)
	data = append(data, tail...)

	di := make([]uint32, 0, len(data)/4+112)
	for i := 0; i < len(data); i += 4 {
		di = append(di, binary.BigEndian.Uint32(data[i:i+4]))
	}
	for i := 0; i < 112; i++ {
		x := di[i+1]
		y := di[i+14]
		di = append(di, di[i]+di[i+9]+
			(bits.RotateLeft32(x, 14)^bits.RotateLeft32(x, 25)^(x>>3))+
			(bits.RotateLeft32(y, 13)^bits.RotateLeft32(y, 15)^(y>>10)))
	}

	initialSrc := [8]uint32{0x7aba4fc8, 0x67166507, 0x6403fa00, 0x340f512f, 984304912, 3005047866, 2874125293, 2152413264}
	var initial [8]uint32
	for i, val := range initialSrc {
		initial[i] = bits.RotateLeft32(val, -int(count&31))
	}

	d := initial

	for i := 0; i < chosen[0]; i++ {
		base := int(seed) + i
		n1 := ((d[chosen[3]] ^ d[chosen[4]]) & d[chosen[1]]) ^ d[chosen[3]]
		n2 := bits.RotateLeft32(d[chosen[1]], 26) ^ bits.RotateLeft32(d[chosen[1]], 21) ^ bits.RotateLeft32(d[chosen[1]], 7)
		n4 := di[uint32(base)&127] + n1 + n2 + tt[uint32(base)&63] + d[chosen[5]]
		n5 := bits.RotateLeft32(d[chosen[2]], 30) ^ bits.RotateLeft32(d[chosen[2]], 19) ^ bits.RotateLeft32(d[chosen[2]], 10)
		n6 := (d[chosen[2]] & d[chosen[6]]) | ((d[chosen[2]] | d[chosen[6]]) & d[chosen[7]])
		old := d[chosen[9]]
		last := d[7]
		copy(d[1:], d[:7])
		d[0] = last
		d[chosen[10]] = n5 + n6 + n4
		d[chosen[8]] = old + n4
	}

	ret := make([]byte, 0, 32)
	for i := 0; i < 8; i++ {
		ret = append(ret, be32(d[i]+initial[i])...)
	}
	folded := xorBytes(ret[:16], ret[16:32])
	return append(folded, le32(sumMd5(folded)|checksumBit)...), nil
}

func signGorgon(query string, body []byte, ts, rnd uint32) string {
	qMd5 := md5.Sum([]byte(query))
	var bMd5 [16]byte
	if body != nil {
		bMd5 = md5.Sum(body)
	}
	input := make([]byte, 0, 20)
	input = append(input, qMd5[:4]...)
	input = append(input, bMd5[:4]...)
	input = append(input, 0, 0, 0, 0)
	input = append(input, le32(67503104)...)
	input = append(input, be32(ts)...)

	key := [8]int{0x4a, 0x40, 0x16, int((rnd >> 8) & 255), 0x47, 0x6c, 1, int(rnd & 255)}
	var s [256]int
	for i := range s {
		s[i] = i
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + s[i] + key[i%8]) & 255
		s[i] = s[j]
	}
	j = 0
	out := make([]byte, len(input))
	for n, b := range input {
		i := n + 1
		j += s[i]
		y := s[j&255]
		s[i] = y
		out[n] = byte(int(b) ^ s[(y+y)&255])
	}
	for i := range out {
		v := ((out[i] >> 4) | (out[i] << 4))
		next := out[(i+1)%len(out)]
		rev := bits.Reverse8(next ^ v)
		out[i] = ^(rev ^ 20)
	}
	res := make([]byte, 0, 6+len(out))
	res = append(res, 0x84, 0x04, byte(rnd), byte(rnd>>8), 0x40, 0x01)
	res = append(res, out...)
	return hex.EncodeToString(res)
}

// signHeliosWith 生成 helios 头。license 是协议版本相关的固定串，
// 它参与明文构造，服务端按版本核对。
func signHeliosWith(ts uint32, license string) string {
	rnd := randUint32()
	sum := md5.Sum(append(le32(rnd), []byte("8662")...))
	ascii := []byte(hex.EncodeToString(sum[:]))
	words := make([]uint64, 4, 38)
	for i := 0; i < 4; i++ {
		words[i] = binary.LittleEndian.Uint64(ascii[i*8 : i*8+8])
	}
	table := make([]uint64, 1, 35)
	table[0] = words[0]
	b0 := words[0]
	b8 := words[1]
	words = words[2:]

	for i := 0; i < 34; i++ {
		x8 := (bits.RotateLeft64(b8, -8) + b0) ^ uint64(i)
		words = append(words, x8)
		x8 ^= bits.RotateLeft64(b0, -61)
		table = append(table, x8)
		b0 = x8
		b8 = words[0]
		words = words[1:]
	}

	text := []byte(fmt.Sprintf("%d-%s-8662", ts, license))
	pad := 16 - (len(text) % 16)
	data := make([]byte, len(text), len(text)+pad)
	copy(data, text)
	for i := 0; i < pad; i++ {
		data = append(data, byte(pad))
	}

	out := make([]byte, 0, 4+len(data))
	out = append(out, le32(rnd)...)
	for at := 0; at < len(data); at += 16 {
		a := binary.LittleEndian.Uint64(data[at : at+8])
		b := binary.LittleEndian.Uint64(data[at+8 : at+16])
		for i := 0; i < 34; i++ {
			b = table[i] ^ (a + bits.RotateLeft64(b, -8))
			a = b ^ bits.RotateLeft64(a, -61)
		}
		out = append(out, le64(a)...)
		out = append(out, le64(b)...)
	}
	return base64.StdEncoding.EncodeToString(out)
}

// aesV3 是红果使用的私有分组变换（不是标准 AES）。
//
// 分组骨架在所有版本里都一样，版本之间的差异全部落在四组表上：
//   - box     字节替换表，按时间戳低 2 位从 1024 字节的 sbox 里取一段
//   - con     列置换表
//   - order   字节重排表
//   - wire/compact 明文字节与密文字节之间的位映射
//
// 初始轮常量另有一组，按同样的 word 下标选取。
type aesV3 struct {
	box     []int
	con     [4]int
	order   [16]int
	keys    [12][4]int
	wire    [8]int
	compact [8]int
}

// aesV3Cons / aesV3Orders / aesV3Initials / aesV3Wire / aesV3Compact 是旧版（v3）参数。
var (
	aesV3Cons   = [4][4]int{{1, 0, 2, 3}, {2, 0, 3, 1}, {0, 1, 3, 2}, {1, 0, 2, 3}}
	aesV3Orders = [4][16]int{
		{0, 9, 14, 11, 4, 13, 2, 7, 8, 1, 6, 15, 12, 5, 10, 3},
		{0, 9, 14, 15, 4, 13, 2, 7, 8, 1, 6, 3, 12, 5, 10, 11},
		{0, 9, 14, 7, 4, 13, 2, 11, 8, 1, 6, 3, 12, 5, 10, 15},
		{0, 9, 14, 11, 4, 13, 2, 7, 8, 1, 6, 15, 12, 5, 10, 3},
	}
	aesV3Initials = [4]uint32{0xca025ddc, 0x823dc546, 0xc9420583, 0xc298225f}
	aesV3Wire     = [8]int{5, 6, 2, 4, 3, 1, 0, 7}
	aesV3Compact  = [8]int{1, 6, 0, 7, 2, 4, 5, 3}
)

func newAesV3(key []byte, ts uint32) *aesV3 {
	return newAesV3WithTables(key, ts, sboxBytes, aesV3Cons, aesV3Orders, aesV3Initials, aesV3Wire, aesV3Compact)
}

// newAesV3WithTables 按给定的表组构造变换器。
// word 由时间戳低 2 位决定 —— sbox、列置换、重排表与初始轮常量都按它下标选取。
func newAesV3WithTables(key []byte, ts uint32, sbox []byte, cons [4][4]int, orders [4][16]int, initials [4]uint32, wire, compact [8]int) *aesV3 {
	word := int(ts & 3)
	box := make([]int, 256)
	for i := 0; i < 256; i++ {
		box[i] = int(sbox[word*256+i])
	}
	initial := initials[word]
	initLE := le32(initial)
	init16 := append(append(append(append([]byte{}, initLE...), initLE...), initLE...), initLE...)
	xored := xorBytes(init16, key)

	var expanded [48]int
	for i := 0; i < 16; i++ {
		expanded[i] = int(xored[i])
	}
	rounds := 8
	for i := 4; i < 12; i++ {
		at := 4 * (i - 1)
		a := expanded[at]
		b := expanded[at+1]
		c := expanded[at+2]
		d := expanded[at+3]
		if i&3 == 0 {
			t := (int(int32(initial)>>(rounds&24)) ^ box[b]) & 255
			b = box[c]
			c = box[d]
			d = box[a]
			a = t
		}
		rounds += 2
		expanded[at+4] = a ^ expanded[at-12]
		expanded[at+5] = b ^ expanded[at-11]
		expanded[at+6] = c ^ expanded[at-10]
		expanded[at+7] = d ^ expanded[at-9]
	}
	var keys [12][4]int
	for i := 0; i < 12; i++ {
		copy(keys[i][:], expanded[i*4:i*4+4])
	}
	return &aesV3{
		box:     box,
		con:     cons[word],
		order:   orders[word],
		keys:    keys,
		wire:    wire,
		compact: compact,
	}
}

func (a *aesV3) block(val []byte) []byte {
	var rows [4][4]int
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			rows[i][j] = int(val[i*4+j])
		}
	}
	addCon := func(off int) {
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				rows[i][j] ^= a.keys[off+i][a.con[j]]
			}
		}
	}
	addCon(0)
	for round := 1; round <= 2; round++ {
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				rows[i][j] = a.box[rows[i][j]]
			}
		}
		before := rows
		for i := 0; i < 4; i++ {
			rows[i] = before[a.con[i]]
		}
		var flat [16]int
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				flat[i*4+j] = rows[i][j]
			}
		}
		for i := 0; i < 16; i++ {
			rows[i/4][i%4] = flat[a.order[i]]
		}
		if round == 1 {
			for i := 0; i < 4; i++ {
				old := rows[i]
				for j := 0; j < 4; j++ {
					rows[i][j] = old[a.con[j]]
				}
			}
			xt := func(x int) int {
				r := x << 1
				if x&128 != 0 {
					r ^= 0x1b
				}
				return r & 255
			}
			for i := 0; i < 4; i++ {
				t := rows[0][i] ^ rows[1][i] ^ rows[2][i] ^ rows[3][i]
				u := rows[0][i]
				rows[0][i] = rows[0][i] ^ t ^ xt(rows[0][i]^rows[1][i])
				rows[1][i] = rows[1][i] ^ t ^ xt(rows[1][i]^rows[2][i])
				rows[2][i] = rows[2][i] ^ t ^ xt(rows[2][i]^rows[3][i])
				rows[3][i] = rows[3][i] ^ t ^ xt(rows[3][i]^u)
			}
		}
		addCon(round * 4)
	}
	out := make([]byte, 16)
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			out[i*4+j] = byte(rows[i][j] ^ a.keys[4+i][j])
		}
	}
	return out
}

// encrypt 把明文按 wire/compact 的位映射摊进 32 字节分组，跑两轮分组变换，
// 再把分组结果按同一映射塞回源字节，返回首字节为分组末字节的密文。
func (a *aesV3) encrypt(source, iv []byte) []byte {
	var plain [32]byte
	for i := 0; i < 31; i++ {
		for j := 0; j < 8; j++ {
			bit := (int(source[i*8+j]) >> uint(a.wire[j])) & 1
			plain[i] |= byte(bit << uint(a.compact[j]))
		}
	}
	plain[31] = 1
	first := a.block(xorBytes(plain[:16], iv))
	second := a.block(xorBytes(plain[16:32], first))
	key := append(first, second...)

	out := make([]byte, 1+len(source))
	out[0] = key[len(key)-1]
	copy(out[1:], source)
	for i := 0; i < 31; i++ {
		for j := 0; j < 8; j++ {
			at := 1 + i*8 + j
			bit := (int(key[i]) >> uint(a.compact[j])) & 1
			mask := byte(1) << uint(a.wire[j])
			out[at] = (out[at] &^ mask) | byte(bit<<uint(a.wire[j]))
		}
	}
	return out
}

func xmxor(data, key []byte) []byte {
	n := len(data)
	encoded := make([]int, n)
	for i := 0; i < n; i++ {
		at := (i * 4) & 28
		d0 := int(key[at])
		d1 := int(key[at+1])
		v := int(data[i])
		d2 := (((v << 4) | (v >> 4)) & 255) + d0
		d2 = (^d2) ^ d1
		d2 = (((d2 & 255) << 3) | ((d2 & 255) >> 5)) & 255
		d2 = (d2 + d1) & 255
		d2 = (d2 ^ d0) & 255
		encoded[n-i-1] = (^d2) & 255
	}
	last := encoded[n-1] ^ encoded[n-2]
	first := encoded[0]
	encoded[0] = ((^last) + first) & 255
	encoded[1] = ((encoded[0] ^ encoded[n-1] ^ 254) + encoded[1]) & 255
	encoded[2] = (encoded[2] + ((last - first) ^ (((encoded[1] << 3) | (encoded[1] >> 5)) & 255) ^ 2)) & 255
	for i := 0; i < n-4; i++ {
		temp := (((encoded[i+2] << 3) | (encoded[i+2] >> 5)) & 255) ^ encoded[i+1] ^ (i + 3)
		encoded[i+3] = ((^temp) + encoded[i+3]) & 255
	}
	encoded[n-1] = encoded[n-1] ^ encoded[n-2]
	sumRest := 0
	for _, x := range encoded[1:] {
		sumRest += x
	}
	encoded[0] = ((encoded[0] ^ encoded[1]) + sumRest) & 255
	out := make([]byte, n)
	for i, x := range encoded {
		out[i] = byte(x)
	}
	return out
}

func (s *AppSigner) deviceProto(devID string) []byte {
	return encodeProto(
		pbSint(1, 1), pbSint(2, 2), pbStr(3, "8662"), pbStr(4, devID),
		pbStr(5, s.processNonce), pbStr(6, "!noperm!"),
		pbSint(7, -888888), pbSint(8, -888888), pbSint(9, 3), pbSint(10, -888888),
		pbStr(11, "!notset!"), pbStr(12, "Asia/Shanghai,8"), pbStr(13, "zh_CN"),
		pbSint(14, 4),
		pbFloat(16, 0), pbFloat(17, 0), pbFloat(18, 0), pbFloat(19, 0), pbFloat(20, 0), pbFloat(21, 0),
		pbStr(22, "16"), pbSint(23, 41), pbSint(24, 36),
		pbSint(25, s.processStarted), pbSint(26, s.processStarted),
		pbSint(27, s.processStarted), pbSint(28, s.processStarted),
		pbSint(29, -1), pbStr(30, "25053RT47C"), pbStr(31, "Redmi"),
		pbStr(32, "25053RT47C"), pbStr(33, "25053RT47C"),
		pbStr(34, "Xiaomi"), pbStr(35, "Redmi"), pbStr(36, "Redmi"),
		pbSint(38, 31),
	)
}

func (s *AppSigner) medusa(rawURL string, body []byte, ts uint32, devID, versionName string) (string, error) {
	var bMd5 [16]byte
	if body != nil {
		bMd5 = md5.Sum(body)
	}
	q := ""
	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		q = rawURL[idx+1:]
	}
	qBytes := []byte(q)
	qSm3 := sm3Sum(qBytes)
	qBodyTs, err := hashF13(qSm3[:], bMd5[:], ts)
	if err != nil {
		return "", err
	}

	nested := encodeProto(pbSint(1, 111), pbSint(2, 10), pbSint(3, 694367), pbSint(5, 586952199))
	envLaunch := int64(randUint32()%21 + 100)
	envPid := int64(randUint32()%2000 + 10001)
	nowSec := time.Now().Unix()
	env := encodeProto(
		pbSint(1, envLaunch), pbSint(2, 146331399), pbSint(3, 146331396),
		pbSint(5, 7), pbStr(6, "v04.06.04.03-bugfix"), pbSint(7, envPid),
		pbBytes(12, s.deviceProto(devID)),
		pbBytes(13, encodeProto(pbSint(1, nowSec), pbSint(2, -2), pbSint(4, 200))),
		pbStr(14, versionName),
	)
	qHashIn := make([]byte, 0, len(qBytes)+16+4)
	qHashIn = append(qHashIn, qBytes...)
	qHashIn = append(qHashIn, bMd5[:]...)
	qHashIn = append(qHashIn, []byte("none")...)
	qHash := sm3Sum(qHashIn)

	magic1, _ := hex.DecodeString("f7e85ffad7d7dc3bd62ac87057cf6118")
	magic10, _ := hex.DecodeString("4001000000000000")
	msg := encodeProto(
		pbBytes(1, magic1), pbSint(2, 3), pbSint(3, int64(randUint32())),
		pbStr(4, "8662"), pbStr(5, devID), pbStr(6, "1588093228"),
		pbStr(7, versionName), pbStr(8, "v04.06.04-ml-android"), pbSint(9, 67503104),
		pbBytes(10, magic10), pbSint(12, int64(ts)), pbBytes(13, qBodyTs),
		pbBytes(14, qSm3[:6]), pbBytes(15, nested), pbStr(16, s.processNonce),
		pbSint(17, int64(ts)), pbBytes(19, qHash[:]), pbStr(20, "none"),
		pbSint(21, 312), pbBytes(23, env),
		pbStr(24, `{"cmr":16777216,"cmr2":16777216,"un_h":1879194040,"vpn":0,"kd":0,"fkd":3672518972,"pd":-1872573247,"dyn":"","do":0,"tk":true}`),
	)

	r := randUint32()
	signKey, _ := hex.DecodeString("8ebdfa3806ecc5cee79423e6029ed82540bc2218bb7eaef71cb691f7aa8aa2f5")
	hashIn := append(append(append([]byte{}, signKey...), le32(r)...), signKey...)
	hash := sm3Sum(hashIn)

	d1 := (r >> 16) & 255
	seed := le32(^(((d1 << 11) | (r >> 24)) ^ (d1 >> 5) ^ d1))
	rawTrans := append(append([]byte{}, magic10...), xmxor(msg, hash[:])...)
	for i, j := 0, len(rawTrans)-1; i < j; i, j = i+1, j-1 {
		rawTrans[i], rawTrans[j] = rawTrans[j], rawTrans[i]
	}
	for i := range rawTrans {
		rawTrans[i] ^= seed[(^i)&3]
	}
	check := ((uint32(qSm3[0]) & 63) << 14) | 0x18000001 | ((uint32(qBodyTs[0]) & 63) << 8)
	packed := make([]byte, 0, 9+len(rawTrans)+2)
	packed = append(packed, 0x35)
	packed = append(packed, le32(randUint32())...)
	packed = append(packed, le32(check)...)
	packed = append(packed, rawTrans...)
	packed = append(packed, byte(r>>16), byte(r>>24))

	aesKey, _ := hex.DecodeString("f1593376766ea98d34f31b057a9d5be4")
	aesIV, _ := hex.DecodeString("1fe109a4125283f418de9e051a969e12")
	encrypted := newAesV3(aesKey, ts).encrypt(packed, aesIV)

	verBytes, _ := hex.DecodeString("03000000f7e85ffad7d7dc3bd62ac87057cf6118")
	out := make([]byte, 0, 24+len(encrypted))
	for i := 0; i < 20; i += 4 {
		w := binary.LittleEndian.Uint32(verBytes[i : i+4])
		out = append(out, le32(w^ts)...)
	}
	out = append(out, byte(r), byte(r>>8), 0, 1)
	out = append(out, encrypted...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func encodeQueryParam(s string) string {
	esc := url.QueryEscape(s)
	esc = strings.ReplaceAll(esc, "+", "%20")
	esc = strings.ReplaceAll(esc, "%7E", "~")
	return esc
}

func encodeQueryOrdered(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, kv := range pairs {
		parts[i] = encodeQueryParam(kv[0]) + "=" + encodeQueryParam(kv[1])
	}
	return strings.Join(parts, "&")
}

// SignPlayback builds a v3-signed request for fqnovel.com endpoints.
//
// 播放接口已迁到 v5 签名（见 SignPlaybackV5）；这条 v3 路径现在只服务
// 仍然使用 v3 的非播放接口（弹幕、评论、推荐流）。
func (s *AppSigner) SignPlayback(base string, dev *RegisteredDevice, body []byte) (string, map[string]string, error) {
	return s.SignRequestWithParams(base, dev, nil, body)
}

// SignRequestWithParams builds a v3-signed request for fqnovel.com endpoints
// with optional endpoint-specific query parameters appended before _rticket.
func (s *AppSigner) SignRequestWithParams(base string, dev *RegisteredDevice, extra [][2]string, body []byte) (string, map[string]string, error) {
	now := time.Now().UnixMilli()
	ts := uint32(now / 1000)

	basePairs := [][2]string{
		{"iid", dev.InstallID},
		{"device_id", dev.DeviceID},
		{"cdid", dev.CDID},
		{"ac", "wifi"},
		{"channel", "update_64"},
		{"aid", "8662"},
		{"app_name", "novelread"},
		{"version_code", "73932"},
		{"version_name", "7.3.9.32"},
		{"device_platform", "android"},
		{"os", "android"},
		{"ssmix", "a"},
		{"device_type", "25053RT47C"},
		{"device_brand", "Redmi"},
		{"language", "zh"},
		{"os_api", "36"},
		{"os_version", "16"},
		{"manifest_version_code", "73932"},
		{"resolution", "1280*2772"},
		{"dpi", "520"},
		{"update_version_code", "73932"},
		{"host_abi", "arm64-v8a"},
		{"dragon_device_type", "phone"},
		{"pv_player", "73932"},
		{"compliance_status", "0"},
		{"need_personal_recommend", "1"},
		{"player_so_load", "1"},
		{"is_android_pad_screen", "0"},
	}
	if len(extra) > 0 {
		basePairs = append(basePairs, extra...)
	}

	for offset := int64(0); offset <= 127; offset++ {
		ticket := now + offset
		pairs := append(append([][2]string{}, basePairs...), [2]string{"_rticket", fmt.Sprintf("%d", ticket)})
		query := encodeQueryOrdered(pairs)
		if hashBranch(query, body, ts) != 0 {
			continue
		}
		fullURL := base + "?" + query
		medusa, err := s.medusa(fullURL, body, ts, dev.DeviceID, "7.3.9.32")
		if err != nil {
			continue
		}
		headers := map[string]string{
			"User-Agent":      playerUA,
			"Accept":          "application/json; charset=utf-8,application/x-protobuf",
			"x-ss-req-ticket": fmt.Sprintf("%d", ticket),
			"sdk-version":     "2",
			"x-xs-from-web":   "0",
			"x-khronos":       fmt.Sprintf("%d", ts),
			"x-ladon":         base64.StdEncoding.EncodeToString(be32(ts)),
			"x-argus":         base64.StdEncoding.EncodeToString(le32(ts)),
			"x-gorgon":        signGorgon(query, body, ts, randUint32()&0xffff),
			"x-helios":        signHeliosWith(ts, "1588093228"),
			"x-medusa":        medusa,
		}
		if dev.CookieHeader != "" {
			headers["Cookie"] = dev.CookieHeader
		}
		if body != nil {
			headers["Content-Type"] = "application/json; charset=utf-8"
			stub := md5.Sum(body)
			headers["x-ss-stub"] = strings.ToUpper(hex.EncodeToString(stub[:]))
		}
		return fullURL, headers, nil
	}
	return "", nil, errors.New("app_signer: failed to find branch 0")
}
