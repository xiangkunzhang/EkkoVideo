// Package iptv implements M3U/M3U8 playlist parsing, XMLTV (.xml/.gz) EPG
// parsing and caching, multi-line channel merging, and IPTV playback resolution.
package iptv

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

var attrPattern = regexp.MustCompile(`([a-zA-Z0-9_-]+)="([^"]*)"`)

// Channel represents a parsed IPTV channel with one or more merged stream lines.
type Channel struct {
	ID      string
	Number  int
	Name    string
	TVGID   string
	TVGName string
	Logo    string
	Group   string
	Lines   []core.Line
}

// Playlist is the parsed output of an M3U/M3U8 file.
type Playlist struct {
	EPGURL   string
	Channels []Channel
	Groups   []string
}

// ParseM3U parses an M3U/M3U8 stream, extracting x-tvg-url, channel metadata,
// #EXTVLCOPT headers, and merging channels with the same normalized name into Lines.
func ParseM3U(r io.Reader) (*Playlist, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	pl := &Playlist{}
	byKey := make(map[string]int)
	groupSeen := make(map[string]bool)

	var (
		hasExtInf bool
		curName   string
		curTVGID  string
		curTVG    string
		curLogo   string
		curGroup  string
		curHdrs   map[string]string
	)

	resetMeta := func() {
		hasExtInf = false
		curName = ""
		curTVGID = ""
		curTVG = ""
		curLogo = ""
		curGroup = ""
		curHdrs = nil
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTM3U") {
			attrs := parseAttrs(line)
			if u := firstNonEmpty(attrs["x-tvg-url"], attrs["url-tvg"]); u != "" {
				// x-tvg-url may be comma-separated; take the first URL.
				pl.EPGURL = strings.TrimSpace(strings.Split(u, ",")[0])
			}
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			resetMeta()
			hasExtInf = true
			attrs := parseAttrs(line)
			curTVGID = attrs["tvg-id"]
			curTVG = attrs["tvg-name"]
			curLogo = attrs["tvg-logo"]
			curGroup = strings.TrimSpace(attrs["group-title"])
			if idx := strings.LastIndexByte(line, ','); idx >= 0 && idx+1 < len(line) {
				curName = strings.TrimSpace(line[idx+1:])
			}
			if curName == "" {
				curName = firstNonEmpty(curTVG, curTVGID, "未命名频道")
			}
			continue
		}
		if strings.HasPrefix(line, "#EXTVLCOPT:") {
			opt := strings.TrimSpace(strings.TrimPrefix(line, "#EXTVLCOPT:"))
			if k, v, ok := strings.Cut(opt, "="); ok {
				if curHdrs == nil {
					curHdrs = make(map[string]string)
				}
				switch strings.ToLower(strings.TrimSpace(k)) {
				case "http-user-agent":
					curHdrs["User-Agent"] = strings.TrimSpace(v)
				case "http-referrer", "http-referer":
					curHdrs["Referer"] = strings.TrimSpace(v)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Stream URL line
		if !hasExtInf {
			curName = line
		}
		if curGroup == "" {
			curGroup = "未分组"
		}
		if !groupSeen[curGroup] {
			groupSeen[curGroup] = true
			pl.Groups = append(pl.Groups, curGroup)
		}

		normKey := normalizeChannelKey(curName)
		kind := core.StreamHLS
		low := strings.ToLower(line)
		switch {
		case strings.HasPrefix(low, "rtmp://") || strings.HasPrefix(low, "rtsp://"):
			kind = core.StreamRTMP
		case strings.Contains(low, ".flv"):
			kind = core.StreamFLV
		}

		if idx, exists := byKey[normKey]; exists {
			ch := &pl.Channels[idx]
			lineNo := len(ch.Lines) + 1
			ch.Lines = append(ch.Lines, core.Line{
				ID:      fmt.Sprintf("line-%d", lineNo),
				Label:   fmt.Sprintf("线路 %d", lineNo),
				URL:     line,
				Kind:    kind,
				Headers: curHdrs,
			})
			if ch.Logo == "" {
				ch.Logo = curLogo
			}
			if ch.TVGID == "" {
				ch.TVGID = curTVGID
			}
		} else {
			chID := channelID(normKey)
			ch := Channel{
				ID:      chID,
				Number:  len(pl.Channels) + 1,
				Name:    curName,
				TVGID:   curTVGID,
				TVGName: curTVG,
				Logo:    curLogo,
				Group:   curGroup,
				Lines: []core.Line{{
					ID:      "line-1",
					Label:   "线路 1",
					URL:     line,
					Kind:    kind,
					Headers: curHdrs,
				}},
			}
			byKey[normKey] = len(pl.Channels)
			pl.Channels = append(pl.Channels, ch)
		}
		resetMeta()
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return pl, nil
}

func parseAttrs(line string) map[string]string {
	out := make(map[string]string)
	for _, m := range attrPattern.FindAllStringSubmatch(line, -1) {
		if len(m) == 3 {
			out[strings.ToLower(m[1])] = m[2]
		}
	}
	return out
}

func normalizeChannelKey(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func channelID(normKey string) string {
	sum := sha1.Sum([]byte(normKey))
	return hex.EncodeToString(sum[:8])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
