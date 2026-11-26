package core

import "time"

// WatchRecord tracks playback progress for a media item (max 500 entries, most recent first).
type WatchRecord struct {
	Ref       ItemRef   `json:"ref"`
	SeriesID  string    `json:"seriesId,omitempty"`
	Episode   int       `json:"episode,omitempty"`
	Title     string    `json:"title"`
	Poster    string    `json:"poster,omitempty"`
	Position  float64   `json:"position"` // seconds
	Duration  float64   `json:"duration"` // seconds
	Completed bool      `json:"completed,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const maxHistoryEntries = 500

// History manages recently watched items in memory (main-thread owned).
type History struct {
	Records []WatchRecord
}

func NewHistory(initial []WatchRecord) *History {
	return &History{Records: append([]WatchRecord(nil), initial...)}
}

// Record inserts or updates a watch record, moving it to the front.
func (h *History) Record(rec WatchRecord) {
	if rec.Ref.ID == "" {
		return
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now()
	}
	if rec.Duration > 0 && rec.Position/rec.Duration >= 0.95 {
		rec.Completed = true
	}
	out := make([]WatchRecord, 0, min(len(h.Records)+1, maxHistoryEntries))
	out = append(out, rec)
	for _, existing := range h.Records {
		if existing.Ref == rec.Ref {
			continue
		}
		out = append(out, existing)
		if len(out) >= maxHistoryEntries {
			break
		}
	}
	h.Records = out
}

func (h *History) Get(ref ItemRef) (WatchRecord, bool) {
	for _, r := range h.Records {
		if r.Ref == ref {
			return r, true
		}
	}
	return WatchRecord{}, false
}

// ResumePosition returns the recommended start offset (3s before last position, 0 if completed).
func (h *History) ResumePosition(ref ItemRef) time.Duration {
	r, ok := h.Get(ref)
	if !ok || r.Completed || r.Position <= 3 {
		return 0
	}
	return time.Duration((r.Position - 3) * float64(time.Second))
}
