// Package surface abstracts native video surface compositing with MyGo UI.
package surface

import (
	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/mpv"
)

type OverlayMode int

const (
	OverlaySameCanvas OverlayMode = iota
	OverlayChildWindow
	OverlayBitmap
)

type VideoSurface interface {
	Attach(window uintptr, engine *mpv.Engine) error
	SetBounds(r ui.Rect, windowHeight float64)
	Show(visible bool)
	Close()
	OverlayMode() OverlayMode
}
