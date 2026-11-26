//go:build !darwin

package imageloader

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

func decodeAndScalePlatform(data []byte, _ int) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}
