package hongguo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
)

// CencSampleRange describes a contiguous encrypted byte range inside the file,
// using absolute file offsets. This is the universal output of moov-level CENC
// parsing and the input to DecryptBlock for arbitrary chunks — no moof/mdat
// pairing required.
type CencSampleRange struct {
	Start  int64
	End    int64
	IV     [16]byte
	EncOff int64 // decryptor's byte offset within the sample (subsample clear prefix)
}

// --- mp4 box primitives (shared by ParseMoovSamples) ---

type mp4Box struct {
	start, end, body int
	typ              string
}

func readMP4Box(data []byte, at, limit int) (mp4Box, bool) {
	if at < 0 || at+8 > limit {
		return mp4Box{}, false
	}
	sz := int64(binary.BigEndian.Uint32(data[at : at+4]))
	hdr := 8
	if sz == 1 {
		if at+16 > limit {
			return mp4Box{}, false
		}
		sz = int64(binary.BigEndian.Uint64(data[at+8 : at+16]))
		hdr = 16
	}
	if sz == 0 {
		sz = int64(limit - at)
	}
	if sz < int64(hdr) || sz > int64(limit-at) {
		return mp4Box{}, false
	}
	return mp4Box{start: at, end: at + int(sz), body: at + hdr, typ: string(data[at+4 : at+8])}, true
}

func findMP4Box(data []byte, typ string, start, limit int) (mp4Box, bool) {
	for at := start; at <= limit-8; at++ {
		if data[at+4] == typ[0] && string(data[at+4:at+8]) == typ {
			if b, ok := readMP4Box(data, at, limit); ok {
				return b, true
			}
		}
	}
	return mp4Box{}, false
}

// ParseMoovSamples walks the moov header of a CENC MP4 and produces the
// global absolute-offset sample table used by DecryptBlock. The input must
// contain ftyp + full moov (trailing bytes after moov are ignored). Works for
// both plain MP4 and fMP4 — both use the same stbl/saio absolute offsets.
func ParseMoovSamples(header []byte) ([]CencSampleRange, error) {
	if len(header) < 16 {
		return nil, fmt.Errorf("hongguo: header too short")
	}
	ftyp, ok := readMP4Box(header, 0, len(header))
	if !ok || ftyp.typ != "ftyp" {
		return nil, fmt.Errorf("hongguo: invalid mp4 ftyp")
	}
	moov, ok := readMP4Box(header, ftyp.end, len(header))
	if !ok || moov.typ != "moov" {
		return nil, fmt.Errorf("hongguo: moov not after ftyp")
	}

	var samples []CencSampleRange
	for cur := moov.body; cur < moov.end; {
		trak, ok := readMP4Box(header, cur, moov.end)
		if !ok {
			break
		}
		cur = trak.end
		if trak.typ != "trak" {
			continue
		}
		stbl, ok := findMP4Box(header, "stbl", trak.body, trak.end)
		if !ok {
			continue
		}
		saiz, ok := findMP4Box(header, "saiz", stbl.body, stbl.end)
		if !ok {
			continue // this trak has no CENC → skip (audio may be clear)
		}
		saio, ok := findMP4Box(header, "saio", stbl.body, stbl.end)
		if !ok {
			return nil, fmt.Errorf("hongguo: missing saio")
		}
		stsz, ok := findMP4Box(header, "stsz", stbl.body, stbl.end)
		if !ok {
			return nil, fmt.Errorf("hongguo: missing stsz")
		}
		stsc, ok := findMP4Box(header, "stsc", stbl.body, stbl.end)
		if !ok {
			return nil, fmt.Errorf("hongguo: missing stsc")
		}
		chunks, ok := findMP4Box(header, "stco", stbl.body, stbl.end)
		if !ok {
			chunks, ok = findMP4Box(header, "co64", stbl.body, stbl.end)
			if !ok {
				return nil, fmt.Errorf("hongguo: missing stco/co64")
			}
		}

		n := int(binary.BigEndian.Uint32(header[stsz.body+8 : stsz.body+12]))
		defSize := int64(binary.BigEndian.Uint32(header[stsz.body+4 : stsz.body+8]))
		sizes := make([]int64, n)
		for i := 0; i < n; i++ {
			if defSize > 0 {
				sizes[i] = defSize
			} else {
				sizes[i] = int64(binary.BigEndian.Uint32(header[stsz.body+12+i*4 : stsz.body+16+i*4]))
			}
		}

		chunkCnt := int(binary.BigEndian.Uint32(header[chunks.body+4 : chunks.body+8]))
		cWidth := 4
		if chunks.typ == "co64" {
			cWidth = 8
		}
		offsets := make([]int64, chunkCnt)
		for i := 0; i < chunkCnt; i++ {
			off := chunks.body + 8 + i*cWidth
			if cWidth == 8 {
				offsets[i] = int64(binary.BigEndian.Uint64(header[off : off+8]))
			} else {
				offsets[i] = int64(binary.BigEndian.Uint32(header[off : off+4]))
			}
		}

		entryCnt := int(binary.BigEndian.Uint32(header[stsc.body+4 : stsc.body+8]))
		type stscEntry struct{ first, count int }
		entries := make([]stscEntry, entryCnt)
		for i := 0; i < entryCnt; i++ {
			off := stsc.body + 8 + i*12
			entries[i] = stscEntry{
				first: int(binary.BigEndian.Uint32(header[off : off+4])),
				count: int(binary.BigEndian.Uint32(header[off+4 : off+8])),
			}
		}

		saizFlags := binary.BigEndian.Uint32(header[saiz.body : saiz.body+4])
		sizeBase := saiz.body + 4
		if saizFlags&1 != 0 {
			sizeBase += 8
		}
		auxDef := int(header[sizeBase])
		auxCnt := int(binary.BigEndian.Uint32(header[sizeBase+1 : sizeBase+5]))
		if auxCnt != n {
			return nil, fmt.Errorf("hongguo: saiz count %d != stsz %d", auxCnt, n)
		}
		auxSizes := make([]int, n)
		for i := 0; i < n; i++ {
			if auxDef > 0 {
				auxSizes[i] = auxDef
			} else {
				auxSizes[i] = int(header[sizeBase+5+i])
			}
		}

		saioFlags := binary.BigEndian.Uint32(header[saio.body : saio.body+4])
		offsetBase := saio.body + 4
		if saioFlags&1 != 0 {
			offsetBase += 8
		}
		saioCnt := int(binary.BigEndian.Uint32(header[offsetBase : offsetBase+4]))
		if saioCnt <= 0 {
			return nil, fmt.Errorf("hongguo: empty saio")
		}
		saioOffsets := make([]int64, saioCnt)
		saioWidth := 4
		if header[saio.body] == 1 {
			saioWidth = 8
		}
		for i := 0; i < saioCnt; i++ {
			off := offsetBase + 4 + i*saioWidth
			if saioWidth == 8 {
				saioOffsets[i] = int64(binary.BigEndian.Uint64(header[off : off+8]))
			} else {
				saioOffsets[i] = int64(binary.BigEndian.Uint32(header[off : off+4]))
			}
		}

		// Determine per-sample IV size from tenc (box body + 7 is default_PerSampleIVSize);
		// default to 8 if tenc is absent or declares 0.
		ivSize := 8
		if tenc, ok := findMP4Box(header, "tenc", stbl.body, stbl.end); ok && tenc.body+8 <= tenc.end {
			if declared := int(header[tenc.body+7]); declared == 8 || declared == 16 {
				ivSize = declared
			}
		}

		aux := saioOffsets[0]
		sampleIdx := 0
		entryIdx := 0
		for chunk := range offsets {
			if saioCnt == len(offsets) {
				aux = saioOffsets[chunk]
			}
			for entryIdx+1 < len(entries) && entries[entryIdx+1].first <= chunk+1 {
				entryIdx++
			}
			cnt := entries[entryIdx].count
			pos := offsets[chunk]
			for k := 0; k < cnt; k++ {
				length := sizes[sampleIdx]
				auxSz := auxSizes[sampleIdx]
				curIVSize := ivSize
				if auxSz < curIVSize {
					curIVSize = auxSz
				}
				if aux < 0 || aux+int64(auxSz) > int64(len(header)) {
					return nil, fmt.Errorf("hongguo: saio offset %d+%d out of header bounds %d", aux, auxSz, len(header))
				}
				var iv [16]byte
				copy(iv[:curIVSize], header[aux:aux+int64(curIVSize)])
				if auxSz == curIVSize {
					samples = append(samples, CencSampleRange{Start: pos, End: pos + length, IV: iv})
				} else if aux+int64(curIVSize)+2 <= int64(len(header)) {
					subs := int(binary.BigEndian.Uint16(header[aux+int64(curIVSize) : aux+int64(curIVSize)+2]))
					off := pos
					var encOff int64
					for s := 0; s < subs; s++ {
						at := aux + int64(curIVSize) + 2 + int64(s)*6
						if at+6 > aux+int64(auxSz) {
							break
						}
						clearSz := int64(binary.BigEndian.Uint16(header[at : at+2]))
						encSz := int64(binary.BigEndian.Uint32(header[at+2 : at+6]))
						off += clearSz
						if encSz > 0 {
							samples = append(samples, CencSampleRange{Start: off, End: off + encSz, IV: iv, EncOff: encOff})
						}
						encOff += encSz
						off += encSz
					}
				}
				pos += length
				aux += int64(auxSz)
				sampleIdx++
			}
		}
	}
	return samples, nil
}

// StripCencInHeader rewrites encryption boxes inside a moov header in-place.
// Caller must pass the moov **body** (everything after the "moov" + 4-byte size).
func StripCencInHeader(body []byte) {
	var walk func(b []byte)
	walk = func(b []byte) {
		for off := 0; off+8 <= len(b); {
			sz := int(binary.BigEndian.Uint32(b[off : off+4]))
			if sz < 8 || off+sz > len(b) {
				break
			}
			typ := string(b[off+4 : off+8])
			switch typ {
			case "trak", "mdia", "minf", "stbl", "moof", "traf":
				walk(b[off+8 : off+sz])
			case "stsd":
				if off+16 <= off+sz {
					stripProtectionInStsd(b[off+16 : off+sz])
				}
			case "sinf", "saio", "saiz", "senc":
				copy(b[off+4:off+8], "free")
				for z := off + 8; z < off+sz; z++ {
					b[z] = 0
				}
			}
			off += sz
		}
	}
	walk(body)
}

// DecryptBlock decrypts an arbitrary byte range from a CENC MP4 given the
// global sample table produced by ParseMoovSamples. rangeStart is where data
// begins inside the whole file (absolute file offset). Clear portions pass
// through untouched; encrypted ranges are XOR'd in place with AES-128-CTR.
func DecryptBlock(data []byte, rangeStart int64, samples []CencSampleRange, key []byte) error {
	if len(key) != 16 {
		return fmt.Errorf("hongguo: invalid aes key length %d", len(key))
	}
	if len(data) == 0 || len(samples) == 0 {
		return nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	rangeEnd := rangeStart + int64(len(data))
	for _, s := range samples {
		if s.End <= rangeStart || s.Start >= rangeEnd {
			continue
		}
		localStart := s.Start - rangeStart
		localEnd := s.End - rangeStart
		if localStart < 0 {
			localStart = 0
		}
		if localEnd > int64(len(data)) {
			localEnd = int64(len(data))
		}
		if localEnd <= localStart {
			continue
		}
		ctr := cipher.NewCTR(block, s.IV[:])
		// CTR 的 keystream 必须在样本内连续推进：先跳过本样本里本区间之前
		// 的加密字节（EncOff，subsample 的明文前缀不算），再跳过本区间起点
		// 之前的部分。少了这一步，跨块切开的样本后半段会用错计数器解出乱码。
		discard := s.EncOff
		if skip := rangeStart - s.Start; skip > 0 {
			discard += skip
		}
		if discard > 0 {
			buf := make([]byte, discard)
			ctr.XORKeyStream(buf, buf)
		}
		ctr.XORKeyStream(data[localStart:localEnd], data[localStart:localEnd])
	}
	return nil
}
