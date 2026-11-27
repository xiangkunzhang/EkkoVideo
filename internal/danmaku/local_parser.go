package danmaku

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/xiangkunzhang/Velan/internal/core"
)

// ParseFile parses a local Bilibili .xml or .ass danmaku file.
func ParseFile(path string) ([]core.Comment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xml":
		return ParseBilibiliXML(data)
	case ".ass", ".ssa":
		return ParseASS(data), nil
	default:
		if strings.Contains(string(data[:min(len(data), 256)]), "<i") {
			return ParseBilibiliXML(data)
		}
		return ParseASS(data), nil
	}
}

// DiscoverSidecar looks for <videoStem>.xml or <videoStem>.danmaku.ass beside a local video.
func DiscoverSidecar(videoPath string) string {
	ext := filepath.Ext(videoPath)
	stem := strings.TrimSuffix(videoPath, ext)
	for _, cand := range []string{stem + ".xml", stem + ".danmaku.ass"} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return ""
}

type bilibiliXML struct {
	Items []struct {
		P    string `xml:"p,attr"`
		Text string `xml:",chardata"`
	} `xml:"d"`
}

// ParseBilibiliXML decodes Bilibili comment XML (<d p="time,mode,size,color,...">text</d>).
func ParseBilibiliXML(data []byte) ([]core.Comment, error) {
	var doc bilibiliXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make([]core.Comment, 0, len(doc.Items))
	for _, it := range doc.Items {
		parts := strings.Split(it.P, ",")
		if len(parts) == 0 {
			continue
		}
		sec, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil || sec < 0 {
			continue
		}
		var col uint32 = 0xffffff
		if len(parts) >= 4 {
			if c, err := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 32); err == nil && c > 0 {
				col = uint32(c)
			}
		}
		out = append(out, core.Comment{
			TimeSec: sec,
			Text:    it.Text,
			Color:   col,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimeSec < out[j].TimeSec })
	return out, nil
}

// ParseASS extracts Dialogue lines from an ASS/SSA subtitle/danmaku script.
func ParseASS(data []byte) []core.Comment {
	lines := strings.Split(string(data), "\n")
	var out []core.Comment
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Dialogue:"))
		// Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
		fields := strings.SplitN(rest, ",", 10)
		if len(fields) < 10 {
			continue
		}
		sec, ok := parseASSTime(fields[1])
		if !ok {
			continue
		}
		txt := stripASSOverrides(fields[9])
		if txt == "" {
			continue
		}
		out = append(out, core.Comment{TimeSec: sec, Text: txt, Color: 0xffffff})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimeSec < out[j].TimeSec })
	return out
}

func parseASSTime(s string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	return float64(h*3600+m*60) + sec, true
}

func stripASSOverrides(s string) string {
	var b strings.Builder
	inBrace := false
	for _, r := range s {
		if r == '{' {
			inBrace = true
			continue
		}
		if r == '}' {
			inBrace = false
			continue
		}
		if !inBrace {
			b.WriteRune(r)
		}
	}
	out := strings.ReplaceAll(b.String(), `\N`, " ")
	out = strings.ReplaceAll(out, `\n`, " ")
	return strings.TrimSpace(out)
}
