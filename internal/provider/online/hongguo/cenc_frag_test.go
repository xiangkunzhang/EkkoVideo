package hongguo

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"testing"
)

// --- 合成工具：构造一份分片式 CENC 文件 ---

func zzBox(typ string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], typ)
	copy(out[8:], payload)
	return out
}

func zzFullBox(typ string, flags uint32, payload []byte) []byte {
	pf := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(pf[0:4], flags)
	copy(pf[4:], payload)
	return zzBox(typ, pf)
}

// zzBuildFragmentedCENC 造一份分片式 CENC 文件：
//
//	ftyp + moov(空 stbl) + moof(traf: tfhd/tfdt/trun/saiz/saio/senc) + mdat
//
// 样本用 AES-128-CTR 加密，每个样本一个 IV。返回文件字节与明文样本。
func zzBuildFragmentedCENC(t *testing.T, key []byte, plain [][]byte) ([]byte, [][]byte) {
	return zzBuildFrag(t, key, plain, true)
}

// zzBuildFrag 造分片 CENC 文件。withOverride=false 时模仿红果的真实写法：
// senc 不带 override box，IV 却是 16 字节 —— 这时 IV 长度只能靠
// saiz 的 aux 长度 + "明+密=样本大小" 反推。
func zzBuildFrag(t *testing.T, key []byte, plain [][]byte, withOverride bool) ([]byte, [][]byte) {
	t.Helper()
	n := len(plain)

	// 加密样本
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	ivs := make([][]byte, n)
	enc := make([][]byte, n)
	for i := range plain {
		iv := make([]byte, 16)
		for j := range iv {
			iv[j] = byte(i*7 + j)
		}
		ivs[i] = iv
		ct := make([]byte, len(plain[i]))
		cipher.NewCTR(block, iv).XORKeyStream(ct, plain[i])
		enc[i] = ct
	}

	// ftyp
	ftyp := zzBox("ftyp", append([]byte("isom"), make([]byte, 28)...))

	// moov：一个最小 mvhd（stbl 为空 → moov 级样本表为空）
	mvhdPayload := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhdPayload[12:16], 1000) // timescale
	binary.BigEndian.PutUint32(mvhdPayload[16:20], 1000) // duration
	moov := zzBox("moov", zzFullBox("mvhd", 0, mvhdPayload))

	moofStart := len(ftyp) + len(moov)

	// 各 box 先拼一遍，真实长度以拼出来的结果为准（偏移要回填，不能手算）。
	mfhd := zzFullBox("mfhd", 0, []byte{0, 0, 0, 1})
	tfhd := zzFullBox("tfhd", 0x020000, []byte{0, 0, 0, 1})
	tfdt := zzFullBox("tfdt", 0, []byte{0, 0, 0, 0})

	// trun：data_offset 相对于 moof 起点，先占位后回填
	trunPayload := make([]byte, 4+4+n*4)
	binary.BigEndian.PutUint32(trunPayload[0:4], uint32(n))
	for i := 0; i < n; i++ {
		binary.BigEndian.PutUint32(trunPayload[8+i*4:12+i*4], uint32(len(enc[i])))
	}
	trun := zzFullBox("trun", 0x000201, trunPayload)

	// saiz：每样本 aux 长度 = 16(IV) + 2 + 6 = 24
	saizPayload := make([]byte, 1+4+n)
	binary.BigEndian.PutUint32(saizPayload[1:5], uint32(n))
	for i := 0; i < n; i++ {
		saizPayload[5+i] = 24
	}
	saiz := zzFullBox("saiz", 0, saizPayload)

	// saio：指向 senc 的 payload 起点，先占位后回填
	saioPayload := make([]byte, 8)
	binary.BigEndian.PutUint32(saioPayload[0:4], 1)
	saio := zzFullBox("saio", 0, saioPayload)

	// senc：16 字节 IV + subsample
	head := 4
	if withOverride {
		head = 28 // override_track_encryption_box_parameters: algorithm_id + iv_size + kid
	}
	sencPayload := make([]byte, head+n*24)
	if withOverride {
		binary.BigEndian.PutUint32(sencPayload[4:8], 16) // iv_size
	}
	binary.BigEndian.PutUint32(sencPayload[head-4:head], uint32(n))
	for i := 0; i < n; i++ {
		off := head + i*24
		copy(sencPayload[off:off+16], ivs[i])
		binary.BigEndian.PutUint16(sencPayload[off+16:off+18], 1) // subsample_count
		binary.BigEndian.PutUint16(sencPayload[off+18:off+20], 0) // clear
		binary.BigEndian.PutUint32(sencPayload[off+20:off+24], uint32(len(enc[i])))
	}
	sencFlags := uint32(0x02)
	if withOverride {
		sencFlags = 0x03
	}
	senc := zzFullBox("senc", sencFlags, sencPayload)

	traf := zzBox("traf", concat(tfhd, tfdt, trun, saiz, saio, senc))
	moof := zzBox("moof", concat(mfhd, traf))

	// 回填偏移：trun.data_offset 与 saio.offset
	mdatPayloadStart := moofStart + len(moof) + 8
	binary.BigEndian.PutUint32(trunPayload[4:8], uint32(mdatPayloadStart-moofStart))
	sencOffsetInMoof := len(mfhd) + len(tfhd) + len(tfdt) + len(trun) + len(saiz) + len(saio)
	binary.BigEndian.PutUint32(saioPayload[4:8], uint32(moofStart+sencOffsetInMoof+8))

	trun = zzFullBox("trun", 0x000201, trunPayload)
	saio = zzFullBox("saio", 0, saioPayload)
	traf = zzBox("traf", concat(tfhd, tfdt, trun, saiz, saio, senc))
	moof = zzBox("moof", concat(mfhd, traf))

	mdatPayload := concat(enc...)
	mdat := zzBox("mdat", mdatPayload)

	return concat(ftyp, moov, moof, mdat), plain
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestParseFragmentSamples 验证分片样本表解析出来的加密区间是对的。
func TestParseFragmentSamples(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := [][]byte{
		bytes.Repeat([]byte{0xAA}, 100),
		bytes.Repeat([]byte{0xBB}, 50),
		bytes.Repeat([]byte{0xCC}, 77),
	}
	file, _ := zzBuildFragmentedCENC(t, key, plain)

	samples, err := ParseFragmentSamples(file, 0)
	if err != nil {
		t.Fatalf("ParseFragmentSamples: %v", err)
	}
	if len(samples) != len(plain) {
		t.Fatalf("样本数 = %d, want %d", len(samples), len(plain))
	}
	// moov 级样本表在这种布局下必须是空的（这正是旧代码漏掉的地方）。
	moovSamples, err := ParseMoovSamples(file)
	if err != nil {
		t.Fatalf("ParseMoovSamples: %v", err)
	}
	if len(moovSamples) != 0 {
		t.Fatalf("moov 级样本数 = %d, want 0", len(moovSamples))
	}
	// 每个样本区间必须落在 mdat 里，且长度正确。
	mdatStart := bytes.Index(file, []byte("mdat")) - 4
	for i, s := range samples {
		if s.End-s.Start != int64(len(plain[i])) {
			t.Fatalf("样本 %d 长度 = %d, want %d", i, s.End-s.Start, len(plain[i]))
		}
		if s.Start < int64(mdatStart) {
			t.Fatalf("样本 %d 起点 %d 落在 mdat 之前", i, s.Start)
		}
	}
}

// TestDecryptFMP4Fragmented 验证整集解密能把分片 CENC 还原成明文。
func TestDecryptFMP4Fragmented(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := [][]byte{
		bytes.Repeat([]byte{0x11}, 1000),
		bytes.Repeat([]byte{0x22}, 333),
	}
	file, want := zzBuildFragmentedCENC(t, key, plain)

	out, err := DecryptFMP4(file, key)
	if err != nil {
		t.Fatalf("DecryptFMP4: %v", err)
	}
	if len(out) != len(file) {
		t.Fatalf("解密后长度 = %d, want %d", len(out), len(file))
	}
	// mdat 的内容必须还原成明文。
	mdatAt := bytes.Index(out, []byte("mdat"))
	if mdatAt < 0 {
		t.Fatal("解密后找不到 mdat")
	}
	payload := out[mdatAt+4:]
	var got []byte
	for _, p := range want {
		got = append(got, p...)
	}
	if !bytes.Equal(payload[:len(got)], got) {
		t.Fatalf("mdat 未还原成明文\n前 32 字节 got=%x\nwant=%x", payload[:32], got[:32])
	}
	// 加密声明必须被抹掉：播放器不该再以为这段数据要解密。
	if bytes.Contains(out, []byte("senc")) {
		t.Fatal("senc 仍然留在文件里")
	}
}

// TestDecryptBlockFragmented 验证按字节区间解密（Range 流式路径用），
// 与整集解密得到一致的结果。
func TestDecryptBlockFragmented(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := [][]byte{bytes.Repeat([]byte{0x33}, 4096)}
	file, want := zzBuildFragmentedCENC(t, key, plain)

	samples, err := ParseFragmentSamples(file, 0)
	if err != nil {
		t.Fatalf("ParseFragmentSamples: %v", err)
	}
	// 模拟 Range 路径：分块解密后再拼起来。
	out := make([]byte, len(file))
	copy(out, file)
	for cs := int64(0); cs < int64(len(out)); cs += 512 {
		ce := cs + 511
		if ce > int64(len(out))-1 {
			ce = int64(len(out)) - 1
		}
		if err := DecryptBlock(out[cs:ce+1], cs, samples, key); err != nil {
			t.Fatalf("DecryptBlock: %v", err)
		}
	}
	mdatAt := bytes.Index(out, []byte("mdat"))
	payload := out[mdatAt+4:]
	if !bytes.Equal(payload[:len(want[0])], want[0]) {
		t.Fatal("分块解密结果与明文不一致")
	}
}

// TestDecryptFMP4FragmentedNoOverride 验证红果真实写法（senc 不带 override
// box、IV 却是 16 字节）也能正确解密。
func TestDecryptFMP4FragmentedNoOverride(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := [][]byte{
		bytes.Repeat([]byte{0x44}, 777),
		bytes.Repeat([]byte{0x55}, 2048),
		bytes.Repeat([]byte{0x66}, 31),
	}
	file, want := zzBuildFrag(t, key, plain, false)

	out, err := DecryptFMP4(file, key)
	if err != nil {
		t.Fatalf("DecryptFMP4: %v", err)
	}
	mdatAt := bytes.Index(out, []byte("mdat"))
	if mdatAt < 0 {
		t.Fatal("解密后找不到 mdat")
	}
	payload := out[mdatAt+4:]
	var got []byte
	for _, p := range want {
		got = append(got, p...)
	}
	if !bytes.Equal(payload[:len(got)], got) {
		t.Fatalf("mdat 未还原成明文\n前 32 字节 got=%x\nwant=%x", payload[:32], got[:32])
	}
}
