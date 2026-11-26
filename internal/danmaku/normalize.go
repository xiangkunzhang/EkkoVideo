// Package danmaku implements EkkoVideo's pure-Go danmaku normalization,
// filtering, clock-synchronized lane scheduling, XML/ASS parsing, and 2x bitmap rendering.
package danmaku

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

var emojiReplacer = strings.NewReplacer(
	"[笑哭]", "😂",
	"[捂脸]", "🤦",
	"[大哭]", "😭",
	"[微笑]", "🙂",
	"[哈哈]", "😄",
	"[赞]", "👍",
	"[强]", "👍",
	"[爱心]", "❤️",
	"[比心]", "🫶",
	"[吃瓜]", "🍉",
	"[doge]", "🐶",
	"[狗头]", "🐶",
	"[生气]", "😠",
	"[震惊]", "😱",
	"[加油]", "💪",
)

// NormalizeText cleans control/bidi characters, maps common bracketed emoji
// codes, collapses whitespace, and truncates long messages to 40 runes.
func NormalizeText(raw string) string {
	s := emojiReplacer.Replace(raw)
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			continue
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > 40 {
		runes := []rune(out)
		out = string(runes[:40]) + "…"
	}
	return out
}

// CleanAndFilter normalizes, removes blocked keywords, and deduplicates
// identical messages appearing within dedupWindowSec (default 20s).
func CleanAndFilter(comments []core.Comment, blockedKeywords []string, dedupWindowSec float64) []core.Comment {
	if dedupWindowSec <= 0 {
		dedupWindowSec = 20
	}
	kws := make([]string, 0, len(blockedKeywords))
	for _, k := range blockedKeywords {
		k = strings.ToLower(strings.TrimSpace(k))
		if k != "" {
			kws = append(kws, k)
		}
	}

	lastSeen := make(map[string]float64, len(comments))
	out := make([]core.Comment, 0, len(comments))
	for _, c := range comments {
		txt := NormalizeText(c.Text)
		if txt == "" {
			continue
		}
		if len(kws) > 0 {
			low := strings.ToLower(txt)
			blocked := false
			for _, kw := range kws {
				if strings.Contains(low, kw) {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
		}
		if prev, ok := lastSeen[txt]; ok && (c.TimeSec-prev) < dedupWindowSec && (c.TimeSec-prev) >= -1 {
			continue
		}
		lastSeen[txt] = c.TimeSec
		c.Text = txt
		if c.Color == 0 {
			c.Color = 0xffffff
		}
		out = append(out, c)
	}
	return out
}
