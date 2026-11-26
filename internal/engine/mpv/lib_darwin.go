//go:build darwin

package mpv

import (
	"os"
	"path/filepath"

	"github.com/ebitengine/purego"
)

func FindLibrary() string {
	if p := os.Getenv("EKKO_LIBMPV"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "..", "Resources", "player", "libmpv.dylib"),
		filepath.Join(exeDir, "..", "Resources", "player", "libmpv.2.dylib"),
		filepath.Join(exeDir, "libmpv.dylib"),
		"/Applications/IINA.app/Contents/Frameworks/libmpv.2.dylib",
		"/opt/homebrew/opt/mpv/lib/libmpv.dylib",
		"/usr/local/opt/mpv/lib/libmpv.dylib",
		"/opt/homebrew/lib/libmpv.dylib",
		"/usr/local/lib/libmpv.dylib",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func openLibrary(path string) (func(string) (uintptr, error), error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) {
		return purego.Dlsym(h, name)
	}, nil
}

func platformOptions() [][2]string {
	return [][2]string{
		{"vo", "libmpv"},
		{"ao", "coreaudio"},
		{"video-timing-offset", "0"},
	}
}
