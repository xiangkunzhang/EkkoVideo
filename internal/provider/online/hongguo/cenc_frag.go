package hongguo

import (
	"encoding/binary"
	"fmt"
)

// 分片 CENC（fragmented CENC）解析。
//
// 红果的流有两种布局：
//
//	渐进式：样本表在 moov（stsz/stsc/stco + saiz/saio），样本数据在 mdat
//	分片式：样本表在每个 moof 的 traf（trun + senc），样本数据紧随其后的 mdat
//
// 两种布局最终都产出同一种表 —— 绝对偏移的加密字节区间 —— 所以下游的
// DecryptBlock 完全不用关心样本表是从哪来的。
//
// 分片式布局的坑：加密辅助信息（每样本 IV 与 subsample 明/密长度）在每个
// moof 里，不在 moov。只扫 moov 会得到"0 个样本"，于是一个字节都没解密，
// 而 stsd 里的 encv 又被改写成 avc1 —— 播放器于是把密文当明文解，画面就是
// 一帧噪声然后黑屏。

// subsampleInfo 描述一个样本内的"明/密"交替布局。
type subsampleInfo struct {
	clear int64 // 明文字节数
	enc   int64 // 密文字节数
}

// ParseFragmentSamples 解析 data 中的所有 movie fragment，产出加密区间表。
//
// base 是 data[0] 在整个文件中的绝对偏移。
func ParseFragmentSamples(data []byte, base int64) ([]CencSampleRange, error) {
	var out []CencSampleRange
	for at := 0; at+8 <= len(data); {
		box, ok := readMP4Box(data, at, len(data))
		if !ok {
			break
		}
		if box.typ == "moof" {
			samples, err := parseMoofSamples(data, box, base)
			if err != nil {
				return nil, err
			}
			out = append(out, samples...)
		}
		at = box.end
	}
	return out, nil
}

// parseMoofSamples 解析一个 moof 里的所有 traf。
func parseMoofSamples(data []byte, moof mp4Box, base int64) ([]CencSampleRange, error) {
	moofStart := base + int64(moof.start)
	var out []CencSampleRange
	for at := moof.body; at < moof.end; {
		box, ok := readMP4Box(data, at, moof.end)
		if !ok {
			break
		}
		at = box.end
		if box.typ != "traf" {
			continue
		}
		samples, err := parseTrafSamples(data, box, moofStart)
		if err != nil {
			return nil, err
		}
		out = append(out, samples...)
	}
	return out, nil
}

type trafInfo struct {
	tfhdFlags     uint32
	baseDataOff   int64
	haveBaseOff   bool
	defSampleSize uint32
	haveDefSize   bool

	trunFlags   uint32
	trunDataOff int32
	haveTrunOff bool
	sampleCount int
	sampleSizes []int64

	// senc 直接给出的每样本 IV 与 subsample 布局。
	senc     sencInfo
	haveSenc bool
	sencIVs  [][]byte
	sencSubs [][]subsampleInfo

	// 没有 senc 时的兜底：saiz 给出每样本 aux 长度，saio 给出 aux 偏移。
	saizSizes []int
	saioOffs  []int64
	haveSaiz  bool
	haveSaio  bool
}

// sencInfo 保存 senc 盒的原始内容，等样本表齐了再逐条解析。
type sencInfo struct {
	flags   uint32
	ivSize  int // override box 里声明的 IV 长度；没有则运行时推断
	payload []byte
}

// parseTrafSamples 解析一个 traf，产出该 traf 内所有样本的加密区间。
func parseTrafSamples(data []byte, traf mp4Box, moofStart int64) ([]CencSampleRange, error) {
	var info trafInfo
	for at := traf.body; at < traf.end; {
		box, ok := readMP4Box(data, at, traf.end)
		if !ok {
			break
		}
		at = box.end
		var err error
		switch box.typ {
		case "tfhd":
			err = parseTfhd(data, box, &info)
		case "trun":
			err = parseTrun(data, box, &info)
		case "senc":
			err = parseSenc(data, box, &info)
		case "saiz":
			err = parseSaiz(data, box, &info)
		case "saio":
			err = parseSaio(data, box, &info)
		}
		if err != nil {
			return nil, err
		}
	}

	if info.sampleCount == 0 || len(info.sampleSizes) == 0 {
		return nil, nil
	}

	// saio 的 chunk_offset 是相对所在 moof 起点的（ISO/IEC 14496-12），
	// 换算成绝对偏移，后面才能直接索引到文件里的辅助信息。
	for i := range info.saioOffs {
		info.saioOffs[i] += moofStart
	}

	// senc 的每样本记录要等样本表（saiz / trun）齐了才能解析：IV 长度可能
	// 只能靠"明+密必须等于样本大小"反推出来。
	if info.haveSenc {
		ivs, subs, err := resolveSenc(info.senc, info.saizSizes, info.sampleSizes)
		if err != nil {
			return nil, err
		}
		info.sencIVs = ivs
		info.sencSubs = subs
	}

	// 样本数据起点：tfhd 的 base-data-offset 优先，否则以 moof 起点为基准
	// （规范：未给出 base-data-offset 时，基准是所在 moof 的第一个字节）。
	dataBase := moofStart
	if info.haveBaseOff {
		dataBase = info.baseDataOff
	}
	firstSample := dataBase
	if info.haveTrunOff {
		firstSample = dataBase + int64(info.trunDataOff)
	}

	var out []CencSampleRange
	pos := firstSample
	for i := 0; i < info.sampleCount && i < len(info.sampleSizes); i++ {
		size := info.sampleSizes[i]
		if size <= 0 {
			continue
		}
		iv, subs, ok := info.sampleCrypto(data, i, size)
		if !ok {
			// 没有可用的加密信息：这个片段是明文，原样透传。
			pos += size
			continue
		}
		if len(subs) == 0 {
			out = append(out, CencSampleRange{Start: pos, End: pos + size, IV: iv})
			pos += size
			continue
		}
		off := pos
		var encOff int64
		for _, sb := range subs {
			off += sb.clear
			if sb.enc > 0 {
				out = append(out, CencSampleRange{Start: off, End: off + sb.enc, IV: iv, EncOff: encOff})
			}
			encOff += sb.enc
			off += sb.enc
		}
		pos += size
	}
	return out, nil
}

// sampleCrypto 取出第 i 个样本的 IV 与 subsample 布局。
func (t *trafInfo) sampleCrypto(data []byte, i int, size int64) ([16]byte, []subsampleInfo, bool) {
	var iv [16]byte
	if t.haveSenc {
		if i >= len(t.sencIVs) {
			return iv, nil, false
		}
		copy(iv[:], t.sencIVs[i])
		var subs []subsampleInfo
		if i < len(t.sencSubs) {
			subs = t.sencSubs[i]
		}
		return iv, subs, true
	}
	// 兜底：saiz + saio 指向的辅助信息。
	if !t.haveSaiz || !t.haveSaio || i >= len(t.saizSizes) || i >= len(t.saioOffs) {
		return iv, nil, false
	}
	auxSize := t.saizSizes[i]
	off := t.saioOffs[i]
	if auxSize <= 0 || off < 0 || off+int64(auxSize) > int64(len(data)) {
		return iv, nil, false
	}
	aux := data[off : off+int64(auxSize)]
	// 辅助信息可能是"IV(8) + subsample"或"IV(16)"，靠"明+密 == 样本大小"判定。
	if auxSize == 8 {
		copy(iv[:], aux[:8])
		return iv, nil, true
	}
	if auxSize == 16 {
		// 16 字节：可能是纯 IV(16)，也可能是 IV(8)+1 个 subsample。
		if subs, ok := decodeSubsamples(aux, 8, size); ok {
			copy(iv[:], aux[:8])
			return iv, subs, true
		}
		copy(iv[:], aux[:16])
		return iv, nil, true
	}
	if subs, ok := decodeSubsamples(aux, 8, size); ok {
		copy(iv[:], aux[:8])
		return iv, subs, true
	}
	if subs, ok := decodeSubsamples(aux, 16, size); ok {
		copy(iv[:], aux[:16])
		return iv, subs, true
	}
	return iv, nil, false
}

// decodeSubsamples 解析 aux 中 ivLen 之后的 subsample 列表，
// 并用"明+密必须等于样本大小"校验解释是否成立。
// sampleSize 传 -1 表示不做这项校验（样本大小未知时）。
func decodeSubsamples(aux []byte, ivLen int, sampleSize int64) ([]subsampleInfo, bool) {
	if len(aux) < ivLen+2 {
		return nil, false
	}
	n := int(binary.BigEndian.Uint16(aux[ivLen : ivLen+2]))
	if n <= 0 || ivLen+2+n*6 > len(aux) {
		return nil, false
	}
	subs := make([]subsampleInfo, 0, n)
	var total int64
	p := ivLen + 2
	for i := 0; i < n; i++ {
		clear := int64(binary.BigEndian.Uint16(aux[p : p+2]))
		enc := int64(binary.BigEndian.Uint32(aux[p+2 : p+6]))
		p += 6
		subs = append(subs, subsampleInfo{clear: clear, enc: enc})
		total += clear + enc
	}
	if sampleSize >= 0 && total != sampleSize {
		return nil, false
	}
	return subs, true
}

func parseTfhd(data []byte, box mp4Box, info *trafInfo) error {
	if box.body+8 > box.end {
		return fmt.Errorf("hongguo: tfhd truncated")
	}
	info.tfhdFlags = binary.BigEndian.Uint32(data[box.body : box.body+4])
	p := box.body + 8 // full box(4) + track_ID(4)
	if info.tfhdFlags&0x01 != 0 {
		if p+8 > box.end {
			return fmt.Errorf("hongguo: tfhd base_data_offset truncated")
		}
		info.baseDataOff = int64(binary.BigEndian.Uint64(data[p : p+8]))
		info.haveBaseOff = true
		p += 8
	}
	if info.tfhdFlags&0x02 != 0 {
		p += 4
	}
	if info.tfhdFlags&0x08 != 0 {
		p += 4
	}
	if info.tfhdFlags&0x10 != 0 {
		if p+4 > box.end {
			return fmt.Errorf("hongguo: tfhd default_sample_size truncated")
		}
		info.defSampleSize = binary.BigEndian.Uint32(data[p : p+4])
		info.haveDefSize = true
		p += 4
	}
	return nil
}

func parseTrun(data []byte, box mp4Box, info *trafInfo) error {
	if box.body+8 > box.end {
		return fmt.Errorf("hongguo: trun truncated")
	}
	info.trunFlags = binary.BigEndian.Uint32(data[box.body : box.body+4])
	info.sampleCount = int(binary.BigEndian.Uint32(data[box.body+4 : box.body+8]))
	p := box.body + 8
	if info.trunFlags&0x01 != 0 {
		if p+4 > box.end {
			return fmt.Errorf("hongguo: trun data_offset truncated")
		}
		info.trunDataOff = int32(binary.BigEndian.Uint32(data[p : p+4]))
		info.haveTrunOff = true
		p += 4
	}
	if info.trunFlags&0x04 != 0 {
		p += 4
	}
	info.sampleSizes = make([]int64, 0, info.sampleCount)
	for i := 0; i < info.sampleCount; i++ {
		if info.trunFlags&0x100 != 0 {
			p += 4
		}
		var size int64
		if info.trunFlags&0x200 != 0 {
			if p+4 > box.end {
				return fmt.Errorf("hongguo: trun sample_size truncated")
			}
			size = int64(binary.BigEndian.Uint32(data[p : p+4]))
			p += 4
		} else if info.haveDefSize {
			size = int64(info.defSampleSize)
		} else {
			return fmt.Errorf("hongguo: trun has no sample sizes")
		}
		if info.trunFlags&0x400 != 0 {
			p += 4
		}
		if info.trunFlags&0x800 != 0 {
			p += 4
		}
		info.sampleSizes = append(info.sampleSizes, size)
	}
	return nil
}

// parseSenc 记录 senc 盒的原始内容。
//
// 这里不逐条解析：没有 override box 时，规范里 IV 长度默认 8 字节，但红果的
// 流实际写的是 16 字节 IV 且不带 override。到底是多长只能靠 saiz 的每样本
// aux 长度 + "明+密必须等于样本大小"反推，所以留到 resolveSenc 再做。
func parseSenc(data []byte, box mp4Box, info *trafInfo) error {
	if box.body+4 > box.end {
		return fmt.Errorf("hongguo: senc truncated")
	}
	flags := binary.BigEndian.Uint32(data[box.body : box.body+4])
	p := box.body + 4
	ivSize := 0
	if flags&0x01 != 0 {
		// override_track_encryption_box_parameters: algorithm_id(4)+iv_size(4)+kid(16)
		if p+24 > box.end {
			return fmt.Errorf("hongguo: senc override parameters truncated")
		}
		ivSize = int(binary.BigEndian.Uint32(data[p+4 : p+8]))
		p += 24
	}
	info.haveSenc = true
	info.senc = sencInfo{flags: flags, ivSize: ivSize, payload: data[p:box.end]}
	return nil
}

// resolveSenc 解析 senc 的每样本记录，产出 IV 与 subsample 布局。
//
// IV 长度按候选逐个试：优先用 override box 声明的值，否则试 16 与 8，
// 用"明+密之和必须等于样本大小"判定哪个解释成立。
func resolveSenc(sec sencInfo, auxSizes []int, sampleSizes []int64) ([][]byte, [][]subsampleInfo, error) {
	if len(sec.payload) < 4 {
		return nil, nil, fmt.Errorf("hongguo: senc payload truncated")
	}
	cnt := int(binary.BigEndian.Uint32(sec.payload[0:4]))
	records := sec.payload[4:]
	if cnt <= 0 {
		return nil, nil, nil
	}
	// 每条记录的长度：优先用 saiz 给的 aux 长度，否则按均分推算。
	recordSize := 0
	if len(auxSizes) >= cnt {
		recordSize = auxSizes[0]
	} else if len(records) >= cnt {
		recordSize = len(records) / cnt
	}

	candidates := []int{}
	if sec.ivSize > 0 {
		candidates = append(candidates, sec.ivSize)
	}
	candidates = append(candidates, 16, 8)

	ivs := make([][]byte, 0, cnt)
	subs := make([][]subsampleInfo, 0, cnt)
	off := 0
	for i := 0; i < cnt; i++ {
		size := recordSize
		if i < len(auxSizes) {
			size = auxSizes[i]
		}
		if size <= 0 || off+size > len(records) {
			return nil, nil, fmt.Errorf("hongguo: senc record %d out of range", i)
		}
		rec := records[off : off+size]
		off += size

		var sampleSize int64 = -1
		if i < len(sampleSizes) {
			sampleSize = sampleSizes[i]
		}

		if sec.flags&0x02 == 0 {
			// 没有 subsample 信息：整条记录就是 IV。
			iv := make([]byte, len(rec))
			copy(iv, rec)
			ivs = append(ivs, iv)
			subs = append(subs, nil)
			continue
		}

		// 有 subsample 信息：IV 长度需要反推。
		var gotIV []byte
		var gotSubs []subsampleInfo
		ok := false
		for _, cand := range candidates {
			if cand <= 0 || cand > len(rec) {
				continue
			}
			if sampleSize >= 0 {
				s, sok := decodeSubsamples(rec, cand, sampleSize)
				if !sok {
					continue
				}
				gotIV = rec[:cand]
				gotSubs = s
				ok = true
				break
			}
			// 没有样本大小时只能按"能解析完且不越界"判定。
			if cand+2 <= len(rec) {
				n := int(binary.BigEndian.Uint16(rec[cand : cand+2]))
				if n > 0 && cand+2+n*6 <= len(rec) {
					gotIV = rec[:cand]
					gotSubs, _ = decodeSubsamples(rec, cand, -1)
					ok = true
					break
				}
			}
		}
		if !ok {
			return nil, nil, fmt.Errorf("hongguo: senc record %d 无法确定 IV 长度", i)
		}
		ivs = append(ivs, gotIV)
		subs = append(subs, gotSubs)
	}
	return ivs, subs, nil
}

func parseSaiz(data []byte, box mp4Box, info *trafInfo) error {
	if box.body+4 > box.end {
		return fmt.Errorf("hongguo: saiz truncated")
	}
	flags := binary.BigEndian.Uint32(data[box.body : box.body+4])
	p := box.body + 4
	if flags&0x01 != 0 {
		p += 8 // aux_info_type + aux_info_type_parameter
	}
	if p+5 > box.end {
		return fmt.Errorf("hongguo: saiz truncated")
	}
	defSize := int(data[p])
	cnt := int(binary.BigEndian.Uint32(data[p+1 : p+5]))
	p += 5
	info.haveSaiz = true
	info.saizSizes = make([]int, 0, cnt)
	for i := 0; i < cnt; i++ {
		if defSize > 0 {
			info.saizSizes = append(info.saizSizes, defSize)
			continue
		}
		if p+1 > box.end {
			return fmt.Errorf("hongguo: saiz sample sizes truncated")
		}
		info.saizSizes = append(info.saizSizes, int(data[p]))
		p++
	}
	return nil
}

func parseSaio(data []byte, box mp4Box, info *trafInfo) error {
	if box.body+4 > box.end {
		return fmt.Errorf("hongguo: saio truncated")
	}
	flags := binary.BigEndian.Uint32(data[box.body : box.body+4])
	p := box.body + 4
	if flags&0x01 != 0 {
		p += 8
	}
	if p+4 > box.end {
		return fmt.Errorf("hongguo: saio truncated")
	}
	cnt := int(binary.BigEndian.Uint32(data[p : p+4]))
	p += 4
	width := 4
	if flags&0x01 != 0 {
		width = 8
	}
	info.haveSaio = true
	info.saioOffs = make([]int64, 0, cnt)
	for i := 0; i < cnt; i++ {
		if p+width > box.end {
			return fmt.Errorf("hongguo: saio offsets truncated")
		}
		var off int64
		if width == 8 {
			off = int64(binary.BigEndian.Uint64(data[p : p+8]))
		} else {
			off = int64(binary.BigEndian.Uint32(data[p : p+4]))
		}
		p += width
		info.saioOffs = append(info.saioOffs, off)
	}
	return nil
}
