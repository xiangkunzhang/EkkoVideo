package iptv

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/xiangkunzhang/Velan/internal/core"
)

// EPGStore holds parsed XMLTV programmes indexed by channel id and lowercase display name.
type EPGStore struct {
	byID   map[string][]core.Programme
	byName map[string][]core.Programme
}

func NewEPGStore() *EPGStore {
	return &EPGStore{
		byID:   make(map[string][]core.Programme),
		byName: make(map[string][]core.Programme),
	}
}

type xmltvDoc struct {
	Channels []struct {
		ID          string   `xml:"id,attr"`
		DisplayName []string `xml:"display-name"`
	} `xml:"channel"`
	Programmes []struct {
		Channel string `xml:"channel,attr"`
		Start   string `xml:"start,attr"`
		Stop    string `xml:"stop,attr"`
		Title   string `xml:"title"`
	} `xml:"programme"`
}

// ParseXMLTV decodes XMLTV (.xml or gzip-compressed .xml.gz) into an EPGStore,
// keeping programmes within [now-24h, now+48h] to bound memory usage.
func ParseXMLTV(raw []byte, now time.Time) (*EPGStore, error) {
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		unpacked, err := io.ReadAll(io.LimitReader(gz, 64<<20))
		if err != nil {
			return nil, err
		}
		raw = unpacked
	}

	var doc xmltvDoc
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	idToNames := make(map[string][]string, len(doc.Channels))
	for _, ch := range doc.Channels {
		for _, dn := range ch.DisplayName {
			if n := normalizeChannelKey(dn); n != "" {
				idToNames[ch.ID] = append(idToNames[ch.ID], n)
			}
		}
	}

	winStart := now.Add(-24 * time.Hour)
	winEnd := now.Add(48 * time.Hour)
	store := NewEPGStore()

	for _, p := range doc.Programmes {
		st, ok1 := parseXMLTVTime(p.Start)
		et, ok2 := parseXMLTVTime(p.Stop)
		if !ok1 || !ok2 || et.Before(winStart) || st.After(winEnd) {
			continue
		}
		prog := core.Programme{
			Title: strings.TrimSpace(p.Title),
			Start: st,
			End:   et,
		}
		if prog.Title == "" {
			continue
		}
		if p.Channel != "" {
			store.byID[p.Channel] = append(store.byID[p.Channel], prog)
			for _, nm := range idToNames[p.Channel] {
				store.byName[nm] = append(store.byName[nm], prog)
			}
			if nm := normalizeChannelKey(p.Channel); nm != "" {
				store.byName[nm] = append(store.byName[nm], prog)
			}
		}
	}

	sortMap := func(m map[string][]core.Programme) {
		for k, list := range m {
			sort.Slice(list, func(i, j int) bool { return list[i].Start.Before(list[j].Start) })
			m[k] = list
		}
	}
	sortMap(store.byID)
	sortMap(store.byName)
	return store, nil
}

func (e *EPGStore) ForChannel(tvgID, name string) []core.Programme {
	if e == nil {
		return nil
	}
	if tvgID != "" {
		if list := e.byID[tvgID]; len(list) > 0 {
			return list
		}
	}
	if name != "" {
		if list := e.byName[normalizeChannelKey(name)]; len(list) > 0 {
			return list
		}
	}
	return nil
}

// NowAndNext returns the currently airing programme and the next upcoming programme.
func (e *EPGStore) NowAndNext(tvgID, name string, now time.Time) (cur *core.Programme, next *core.Programme) {
	list := e.ForChannel(tvgID, name)
	for i := range list {
		p := list[i]
		if !now.Before(p.Start) && now.Before(p.End) {
			cur = &p
			if i+1 < len(list) {
				nx := list[i+1]
				next = &nx
			}
			return cur, next
		}
		if p.Start.After(now) {
			next = &p
			return nil, next
		}
	}
	return nil, nil
}

func parseXMLTVTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"20060102150405 -0700",
		"20060102150405-0700",
		"20060102150405",
		time.RFC3339,
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
