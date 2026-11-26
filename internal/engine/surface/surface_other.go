//go:build !darwin

package surface

import (
	"errors"

	"github.com/egoist/mygo/ui"
	"github.com/xiangkunzhang/EkkoVideo/internal/engine/mpv"
)

type stubSurface struct{}

func New() VideoSurface { return &stubSurface{} }

func (s *stubSurface) Attach(uintptr, *mpv.Engine) error {
	return errors.New("surface not implemented for this OS yet")
}
func (s *stubSurface) SetBounds(ui.Rect, float64) {}
func (s *stubSurface) Show(bool)                  {}
func (s *stubSurface) Close()                     {}
func (s *stubSurface) OverlayMode() OverlayMode   { return OverlaySameCanvas }
