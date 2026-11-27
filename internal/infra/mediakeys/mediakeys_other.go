//go:build !darwin

package mediakeys

type Handlers struct {
	OnPlay   func()
	OnPause  func()
	OnToggle func()
	OnNext   func()
	OnPrev   func()
}

func Init(Handlers) {}

func UpdateNowPlaying(string, bool, bool, float64, float64) {}
