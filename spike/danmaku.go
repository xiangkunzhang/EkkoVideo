package main

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// danmakuSprite caches a 2x stroked bitmap of one danmaku line.
// Rasterization happens in a single background slot so MyGo's glyph atlas is never touched concurrently.
type danmakuSprite struct {
	bmp  atomic.Pointer[ui.Bitmap]
	w, h float32
}

type danmakuItem struct {
	text    string
	color   ui.Color
	lane    int
	started time.Time
	sprite  *danmakuSprite
}

type danmakuStress struct {
	enabled      bool
	ratePerMin   int
	items        []danmakuItem
	lanes        [12]time.Time
	slots        chan struct{}
	lastSpawn    time.Time
	seq          int
	totalSpawned int
}

func newDanmakuStress() *danmakuStress {
	return &danmakuStress{
		enabled:    true,
		ratePerMin: 1000, // M0 benchmark target: 1000 msgs/min
		slots:      make(chan struct{}, 1),
	}
}

var sampleTexts = []string{
	"前方高能预警！！",
	"这画质也太清晰了吧",
	"Pure Go + libmpv 丝滑起飞",
	"233333333333",
	"原声大碟好评！",
	"这一段我看了十遍",
	"Mac 原生 UI 帧率稳如老狗",
	"弹幕护体！",
}

var sampleColors = []ui.Color{
	ui.Hex("#ffffff"),
	ui.Hex("#ffe38a"),
	ui.Hex("#a9d5ff"),
	ui.Hex("#ffbbd3"),
}

func (d *danmakuStress) tick(now time.Time, paused bool, width float32) {
	if !d.enabled || paused || width <= 0 {
		return
	}
	interval := time.Minute / time.Duration(max(1, d.ratePerMin))
	if d.lastSpawn.IsZero() {
		d.lastSpawn = now
	}
	for now.Sub(d.lastSpawn) >= interval {
		d.lastSpawn = d.lastSpawn.Add(interval)
		d.spawn(d.lastSpawn, width)
		if len(d.items) > 200 {
			break
		}
	}
	// Prune off-screen items
	keep := d.items[:0]
	for _, it := range d.items {
		x := d.itemX(it, now, width)
		if x+it.sprite.w > -40 {
			keep = append(keep, it)
		}
	}
	d.items = keep
}

func (d *danmakuStress) spawn(now time.Time, width float32) {
	lane := -1
	for i := range d.lanes {
		if now.After(d.lanes[i]) {
			lane = i
			break
		}
	}
	if lane < 0 {
		lane = d.seq % len(d.lanes)
	}
	d.seq++
	d.totalSpawned++
	text := fmt.Sprintf("%s #%d", sampleTexts[d.seq%len(sampleTexts)], d.seq)
	col := sampleColors[d.seq%len(sampleColors)]
	sp := d.makeSprite(text, col)
	d.lanes[lane] = now.Add(900 * time.Millisecond)
	d.items = append(d.items, danmakuItem{
		text:    text,
		color:   col,
		lane:    lane,
		started: now,
		sprite:  sp,
	})
}

func (d *danmakuStress) makeSprite(text string, col ui.Color) *danmakuSprite {
	// Estimate box cleanly; actual text fits within 360x36 DIPs.
	const estW, estH = float32(340), float32(34)
	sp := &danmakuSprite{w: estW, h: estH}
	slots := d.slots
	go func() {
		slots <- struct{}{}
		defer func() { <-slots }()
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				ink := ui.RGBA(0, 0, 0, 0.85)
				outline := ui.Span{Text: text, Size: 20, Weight: 600, Color: ink}
				for _, off := range [][2]float32{{-1.2, 0}, {1.2, 0}, {0, -1.2}, {0, 1.2}} {
					p.RichText(r.X+4+off[0], r.Y+4+off[1], estW-8, outline)
				}
				p.RichText(r.X+4, r.Y+4, estW-8, ui.Span{Text: text, Size: 20, Weight: 600, Color: col})
			})
		}, int(math.Ceil(float64(estW))), int(math.Ceil(float64(estH))), 2)
		sp.bmp.Store(ui.NewBitmap(img))
	}()
	return sp
}

func (d *danmakuStress) itemX(it danmakuItem, now time.Time, width float32) float32 {
	speed := float32(180) * max(1, width/1280)
	return width - float32(now.Sub(it.started).Seconds())*speed
}

func (d *danmakuStress) draw(p *ui.Painter, r ui.Rect, now time.Time) {
	if !d.enabled {
		return
	}
	for _, it := range d.items {
		bmp := it.sprite.bmp.Load()
		if bmp == nil {
			continue
		}
		x := r.X + d.itemX(it, now, r.W)
		y := r.Y + 48 + float32(it.lane)*30
		if y+it.sprite.h > r.Y+r.H*0.6 {
			continue
		}
		p.Image(bmp, ui.Rect{X: x, Y: y, W: it.sprite.w, H: it.sprite.h}, ui.Contain)
	}
}
