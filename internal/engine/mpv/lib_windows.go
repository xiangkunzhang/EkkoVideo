//go:build windows

package mpv

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func FindLibrary() string {
	if p := os.Getenv("VELAN_LIBMPV"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "libmpv-2.dll"),
		filepath.Join(exeDir, "mpv-2.dll"),
		filepath.Join(exeDir, "player", "libmpv-2.dll"),
		filepath.Join(exeDir, "player", "mpv-2.dll"),
		filepath.Join(exeDir, "resources", "player", "libmpv-2.dll"),
		filepath.Join(exeDir, "resources", "player", "mpv-2.dll"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func openLibrary(path string) (func(string) (uintptr, error), error) {
	h, err := windows.LoadLibrary(path)
	if err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) {
		return windows.GetProcAddress(h, name)
	}, nil
}

func platformOptions() [][2]string {
	return [][2]string{
		{"vo", "gpu"},
		{"ao", "wasapi"},
		{"video-timing-offset", "0"},
	}
}
