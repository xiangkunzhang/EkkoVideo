package danmaku

import (
	"math"
	"sort"
	"unicode/utf8"

	"github.com/xiangkunzhang/EkkoVideo/internal/core"
)

// FlyingItem represents an active danmaku item moving across the video canvas.
type FlyingItem struct {
	Text      string
	Color     uint32
	Lane      int
	StartTime float64 // media clock (seconds) when it entered from right edge
	Width     float32 // estimated DIP width
	Height    float32
	Sprite    *Sprite
}

type laneState struct {
	lastStart float64
	lastWidth float32
}

// Scheduler dispatches timed comments onto non-overlapping horizontal lanes
// synchronized strictly to the player's media clock.
type Scheduler struct {
	comments   []core.Comment
	nextIdx    int
	lastPosSec float64
	hasPos     bool
	lanes      [24]laneState
	Active     []FlyingItem
}

func NewScheduler() *Scheduler { return &Scheduler{} }

func (s *Scheduler) Load(comments []core.Comment, keywords []string) {
	cleaned := CleanAndFilter(comments, keywords, 20)
	sort.Slice(cleaned, func(i, j int) bool { return cleaned[i].TimeSec < cleaned[j].TimeSec })
	s.comments = cleaned
	s.Reset()
}

func (s *Scheduler) Count() int { return len(s.comments) }

func (s *Scheduler) Reset() {
	s.Active = s.Active[:0]
	s.lanes = [24]laneState{}
	s.nextIdx = 0
	s.hasPos = false
}

// Seek repositions the read cursor and clears active on-screen danmaku.
func (s *Scheduler) Seek(posSec float64) {
	s.Active = s.Active[:0]
	s.lanes = [24]laneState{}
	s.nextIdx = sort.Search(len(s.comments), func(i int) bool {
		return s.comments[i].TimeSec >= posSec-0.5
	})
	s.lastPosSec = posSec
	s.hasPos = true
}

// Step advances the scheduler to posSec, spawning due items and pruning exited items.
func (s *Scheduler) Step(posSec float64, vpW, vpH float32, cfg Config, makeSprite func(string, uint32, Config) *Sprite) {
	if !cfg.Enabled || vpW <= 20 || vpH <= 20 {
		if !cfg.Enabled && len(s.Active) > 0 {
			s.Active = s.Active[:0]
		}
		return
	}
	if !s.hasPos || math.Abs(posSec-s.lastPosSec) > 1.5 {
		s.Seek(posSec)
	}
	s.lastPosSec = posSec

	speedPx := float32(155) * max(0.5, float32(cfg.Speed)) * max(0.8, vpW/1280)
	lineH := float32(cfg.FontSize) + 10
	maxLanes := max(1, min(len(s.lanes), int((vpH*float32(cfg.Area)-16)/lineH)))

	// Prune items that have exited the left edge.
	keep := s.Active[:0]
	for _, it := range s.Active {
		x := vpW - float32(posSec-it.StartTime)*speedPx
		if x+it.Width > -24 && posSec >= it.StartTime-0.5 {
			keep = append(keep, it)
		}
	}
	s.Active = keep

	// Spawn comments up to posSec (skipping comments older than 1.5s).
	for s.nextIdx < len(s.comments) {
		c := s.comments[s.nextIdx]
		if c.TimeSec > posSec {
			break
		}
		s.nextIdx++
		if posSec-c.TimeSec > 1.5 {
			continue
		}
		if len(s.Active) >= 80 {
			continue
		}
		estW := estimateWidth(c.Text, float32(cfg.FontSize))
		lane := s.pickLane64(posSec, vpW, speedPx, estW, maxLanes)
		if lane < 0 {
			continue
		}
		s.lanes[lane] = laneState{lastStart: posSec, lastWidth: estW}
		var sp *Sprite
		if makeSprite != nil {
			sp = makeSprite(c.Text, c.Color, cfg)
		}
		s.Active = append(s.Active, FlyingItem{
			Text:      c.Text,
			Color:     c.Color,
			Lane:      lane,
			StartTime: posSec,
			Width:     estW,
			Height:    float32(cfg.FontSize) + 10,
			Sprite:    sp,
		})
	}
}

func (s *Scheduler) pickLane(now, vpW, speedPx, newW float32, maxLanes int) int {
	return s.pickLane64(float64(now), vpW, speedPx, newW, maxLanes)
}

func (s *Scheduler) pickLane64(now float64, vpW, speedPx, newW float32, maxLanes int) int {
	const gap = float32(28)
	for i := 0; i < maxLanes; i++ {
		st := s.lanes[i]
		if st.lastWidth == 0 {
			return i
		}
		elapsed := float32(now - st.lastStart)
		if elapsed < 0 {
			continue
		}
		// Previous item's right edge must have cleared the right screen edge + gap.
		if elapsed*speedPx >= st.lastWidth+gap {
			return i
		}
	}
	_ = vpW
	_ = newW
	return -1
}

func estimateWidth(text string, fontSize float32) float32 {
	n := utf8.RuneCountInString(text)
	return float32(n)*fontSize*0.92 + 16
}
