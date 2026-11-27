//go:build !darwin && !windows

package mpv

import "errors"

func FindLibrary() string { return "" }

func openLibrary(string) (func(string) (uintptr, error), error) {
	return nil, errors.New("libmpv not yet wired for this OS")
}

func platformOptions() [][2]string { return nil }
