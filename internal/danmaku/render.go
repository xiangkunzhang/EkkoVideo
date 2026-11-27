package danmaku

import (
	"math"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// Config controls danmaku appearance and filtering.
type Config struct {
	Enabled  bool
	FontSize float64
	Opacity  float64
	Speed    float64
	Area     float64 // 0.25 | 0.50 | 1.0
	Keywords []string
}

func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		FontSize: 20,
		Opacity:  0.88,
		Speed:    1.0,
		Area:     0.50,
	}
}

// Sprite caches a 2x stroked bitmap for one danmaku line.
type Sprite struct {
	bmp  atomic.Pointer[ui.Bitmap]
	w, h float32
}

// Renderer rasterizes danmaku sprites on a single background slot to avoid
// concurrent access to MyGo's shared glyph atlas.
type Renderer struct {
	slots chan struct{}
}

func NewRenderer() *Renderer {
	return &Renderer{slots: make(chan struct{}, 1)}
}

func (r *Renderer) MakeSprite(text string, col uint32, cfg Config) *Sprite {
	fontSz := float32(max(14, min(36, cfg.FontSize)))
	estW := estimateWidth(text, fontSz) + 6
	estH := fontSz + 16
	sp := &Sprite{w: estW, h: estH}

	color := ui.RGB(uint8(col>>16), uint8(col>>8), uint8(col))
	if col == 0 {
		color = ui.RGB(255, 255, 255)
	}
	alpha := float32(max(0.2, min(1.0, cfg.Opacity)))
	color = color.Alpha(alpha)
	ink := ui.RGBA(0, 0, 0, 0.92*alpha)
	shadow := ui.RGBA(0, 0, 0, 0.58*alpha)

	slots := r.slots
	go func() {
		slots <- struct{}{}
		defer func() { <-slots }()
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, rect ui.Rect) {
				// Soft drop shadow for bright scenes
				p.RichText(rect.X+5.5, rect.Y+5.5, estW-6, ui.Span{Text: text, Size: fontSz, Weight: 600, Color: shadow})
				// 8-direction high-contrast outline
				outline := ui.Span{Text: text, Size: fontSz, Weight: 600, Color: ink}
				for _, off := range [][2]float32{
					{-1.25, 0}, {1.25, 0}, {0, -1.25}, {0, 1.25},
					{-1, -1}, {1, -1}, {-1, 1}, {1, 1},
				} {
					p.RichText(rect.X+4+off[0], rect.Y+4+off[1], estW-6, outline)
				}
				p.RichText(rect.X+4, rect.Y+4, estW-6, ui.Span{Text: text, Size: fontSz, Weight: 600, Color: color})
			})
		}, int(math.Ceil(float64(estW))), int(math.Ceil(float64(estH))), 2)
		sp.bmp.Store(ui.NewBitmap(img))
	}()
	return sp
}

// VideoContentRect computes the actual rendered video rectangle inside container r,
// ensuring danmaku stays within portrait/letterboxed video bounds.
func VideoContentRect(container ui.Rect, videoW, videoH int64, aspectOverride string) ui.Rect {
	if container.W <= 0 || container.H <= 0 {
		return container
	}
	if aspectOverride == "fill" {
		return container
	}
	aspect := float32(16.0 / 9.0)
	switch aspectOverride {
	case "16:9":
		aspect = 16.0 / 9.0
	case "4:3":
		aspect = 4.0 / 3.0
	default:
		if videoW > 0 && videoH > 0 {
			aspect = float32(videoW) / float32(videoH)
		}
	}
	res := container
	if container.W/container.H > aspect {
		res.W = container.H * aspect
		res.X = container.X + (container.W-res.W)/2
	} else {
		res.H = container.W / aspect
		res.Y = container.Y + (container.H-res.H)/2
	}
	return res
}

// Draw paints all active flying danmaku inside videoRect.
func Draw(p *ui.Painter, videoRect ui.Rect, sched *Scheduler, posSec float64, cfg Config) {
	if !cfg.Enabled || sched == nil || len(sched.Active) == 0 || videoRect.W <= 0 {
		return
	}
	speedPx := float32(155) * max(0.5, float32(cfg.Speed)) * max(0.8, videoRect.W/1280)
	lineH := float32(cfg.FontSize) + 10

	p.Clip(videoRect, 0, func() {
		for _, it := range sched.Active {
			if it.Sprite == nil {
				continue
			}
			bmp := it.Sprite.bmp.Load()
			if bmp == nil {
				continue
			}
			x := videoRect.X + videoRect.W - float32(posSec-it.StartTime)*speedPx
			y := videoRect.Y + 12 + float32(it.Lane)*lineH
			if y+it.Sprite.h > videoRect.Y+videoRect.H {
				continue
			}
			p.Image(bmp, ui.Rect{X: x, Y: y, W: it.Sprite.w, H: it.Sprite.h}, ui.Contain)
		}
	})
}
