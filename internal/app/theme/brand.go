package theme

import (
	_ "embed"
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/velan_mark.png
var velanMarkPNG []byte

//go:embed assets/velan_badge.png
var velanBadgePNG []byte

var (
	brandOnce  sync.Once
	brandMark  ui.ImageSource
	brandBadge ui.ImageSource
)

func initBrandAssets() {
	brandOnce.Do(func() {
		if bmp, err := ui.DecodeBitmap(velanMarkPNG); err == nil {
			brandMark = bmp
		}
		if bmp, err := ui.DecodeBitmap(velanBadgePNG); err == nil {
			brandBadge = bmp
		}
	})
}

// BrandMark returns the transparent Velan glass sail/ripple emblem.
func BrandMark() ui.ImageSource {
	initBrandAssets()
	return brandMark
}

// BrandBadge returns the full-bleed square Velan app emblem.
func BrandBadge() ui.ImageSource {
	initBrandAssets()
	return brandBadge
}

// BrandLoader renders the Velan glass emblem with an optional gentle floating
// breathing animation (matching the velan-glass-loader.svg 2.6s cycle).
func BrandLoader(c *ui.Context, size float32, label string, textColor ui.Color, animated bool) {
	mark := BrandMark()
	floatY := float32(0)
	if animated {
		phase := float64(c.Now().UnixNano()%2600000000) / 2.6e9 * 2 * math.Pi
		wave := float32(0.5 - 0.5*math.Cos(phase)) // 0..1..0 smooth 2.6s cycle
		floatY = -5 * wave
		c.After(33 * time.Millisecond)
	}

	ui.Column(c).Center().Gap(12).Children(func() {
		ui.Box(c).Size(size, size+10).Padding(5+floatY, 0, 5-floatY, 0).Center().Children(func() {
			if mark != nil {
				ui.Image(c, mark).Size(size, size)
			} else {
				ui.Spinner(c).FontSize(26)
			}
		})
		if label != "" {
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(textColor)
		}
	})
}
