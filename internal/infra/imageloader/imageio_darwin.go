//go:build darwin

package imageloader

import (
	"errors"
	"image"
	"image/color"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	imageioOnce sync.Once
	imageioErr  error

	cfDataCreate                     func(allocator uintptr, bytes *byte, length int64) uintptr
	cfRelease                        func(cf uintptr)
	cfDictCreate                     func(allocator uintptr, keys, values *uintptr, num int64, kcb, vcb uintptr) uintptr
	cfNumCreate                      func(allocator uintptr, theType int32, valPtr unsafe.Pointer) uintptr
	cgImageSourceCreateWithData      func(data uintptr, opts uintptr) uintptr
	cgImageSourceCreateThumbAtIndex  func(isrc uintptr, index uint64, opts uintptr) uintptr
	cgImageSourceCreateImageAtIndex  func(isrc uintptr, index uint64, opts uintptr) uintptr
	cgImageGetWidth                  func(img uintptr) uint64
	cgImageGetHeight                 func(img uintptr) uint64
	cgColorSpaceCreateDeviceRGB      func() uintptr
	cgColorSpaceRelease              func(cs uintptr)
	cgBitmapContextCreate            func(data unsafe.Pointer, w, h, bpc, bpr uint64, cs uintptr, info uint32) uintptr
	cgContextSetInterpolationQuality func(ctx uintptr, q int32)
	cgContextDrawImage               func(ctx uintptr, x, y, w, h float64, img uintptr)
	cgContextRelease                 func(ctx uintptr)
	cgImageRelease                   func(img uintptr)

	kCFBooleanTrue          uintptr
	kCFTypeDictKeyCallBacks uintptr
	kCFTypeDictValCallBacks uintptr
	kThumbCreateAlways      uintptr
	kThumbMaxPixelSize      uintptr
	kThumbTransform         uintptr
)

func initImageIO() error {
	imageioOnce.Do(func() {
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			imageioErr = err
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			imageioErr = err
			return
		}
		io, err := purego.Dlopen("/System/Library/Frameworks/ImageIO.framework/ImageIO", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			imageioErr = err
			return
		}

		reg := func(h uintptr, name string, fn any) {
			if imageioErr != nil {
				return
			}
			addr, err := purego.Dlsym(h, name)
			if err != nil {
				imageioErr = err
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		symPtr := func(h uintptr, name string) uintptr {
			if imageioErr != nil {
				return 0
			}
			addr, err := purego.Dlsym(h, name)
			if err != nil {
				imageioErr = err
				return 0
			}
			return *(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&addr)))
		}

		reg(cf, "CFDataCreate", &cfDataCreate)
		reg(cf, "CFRelease", &cfRelease)
		reg(cf, "CFDictionaryCreate", &cfDictCreate)
		reg(cf, "CFNumberCreate", &cfNumCreate)
		reg(io, "CGImageSourceCreateWithData", &cgImageSourceCreateWithData)
		reg(io, "CGImageSourceCreateThumbnailAtIndex", &cgImageSourceCreateThumbAtIndex)
		reg(io, "CGImageSourceCreateImageAtIndex", &cgImageSourceCreateImageAtIndex)
		reg(cg, "CGImageGetWidth", &cgImageGetWidth)
		reg(cg, "CGImageGetHeight", &cgImageGetHeight)
		reg(cg, "CGColorSpaceCreateDeviceRGB", &cgColorSpaceCreateDeviceRGB)
		reg(cg, "CGColorSpaceRelease", &cgColorSpaceRelease)
		reg(cg, "CGBitmapContextCreate", &cgBitmapContextCreate)
		reg(cg, "CGContextSetInterpolationQuality", &cgContextSetInterpolationQuality)
		reg(cg, "CGContextDrawImage", &cgContextDrawImage)
		reg(cg, "CGContextRelease", &cgContextRelease)
		reg(cg, "CGImageRelease", &cgImageRelease)

		kCFBooleanTrue = symPtr(cf, "kCFBooleanTrue")
		kCFTypeDictKeyCallBacks, _ = purego.Dlsym(cf, "kCFTypeDictionaryKeyCallBacks")
		kCFTypeDictValCallBacks, _ = purego.Dlsym(cf, "kCFTypeDictionaryValueCallBacks")
		kThumbCreateAlways = symPtr(io, "kCGImageSourceCreateThumbnailFromImageAlways")
		kThumbMaxPixelSize = symPtr(io, "kCGImageSourceThumbnailMaxPixelSize")
		kThumbTransform = symPtr(io, "kCGImageSourceCreateThumbnailWithTransform")
	})
	return imageioErr
}

// decodeAndScalePlatform decodes HEIC/WebP/JPEG/PNG via macOS ImageIO without CGO
// and scales down so the longest edge is <= maxWidth*2.
func decodeAndScalePlatform(data []byte, maxWidth int) (image.Image, error) {
	if len(data) == 0 {
		return nil, errors.New("empty image data")
	}
	if err := initImageIO(); err != nil {
		return nil, err
	}
	cfData := cfDataCreate(0, &data[0], int64(len(data)))
	if cfData == 0 {
		return nil, errors.New("CFDataCreate failed")
	}
	defer cfRelease(cfData)

	src := cgImageSourceCreateWithData(cfData, 0)
	if src == 0 {
		return nil, errors.New("CGImageSourceCreateWithData failed")
	}
	defer cfRelease(src)

	maxPx := int32(maxWidth * 2)
	cfMax := cfNumCreate(0, 3, unsafe.Pointer(&maxPx)) // kCFNumberSInt32Type = 3
	if cfMax == 0 {
		return nil, errors.New("CFNumberCreate failed")
	}
	defer cfRelease(cfMax)

	keys := [...]uintptr{kThumbCreateAlways, kThumbMaxPixelSize, kThumbTransform}
	vals := [...]uintptr{kCFBooleanTrue, cfMax, kCFBooleanTrue}
	opts := cfDictCreate(0, &keys[0], &vals[0], 3, kCFTypeDictKeyCallBacks, kCFTypeDictValCallBacks)
	if opts == 0 {
		return nil, errors.New("CFDictionaryCreate failed")
	}
	defer cfRelease(opts)

	cgImg := cgImageSourceCreateThumbAtIndex(src, 0, opts)
	if cgImg == 0 {
		cgImg = cgImageSourceCreateImageAtIndex(src, 0, 0)
	}
	if cgImg == 0 {
		return nil, errors.New("decode image failed")
	}
	defer cgImageRelease(cgImg)

	w := int(cgImageGetWidth(cgImg))
	h := int(cgImageGetHeight(cgImg))
	if w <= 0 || h <= 0 {
		return nil, errors.New("zero image dimensions")
	}

	cs := cgColorSpaceCreateDeviceRGB()
	if cs == 0 {
		return nil, errors.New("CGColorSpaceCreateDeviceRGB failed")
	}
	defer cgColorSpaceRelease(cs)

	rgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	// kCGImageAlphaPremultipliedLast (1) | kCGBitmapByteOrder32Big (4<<12) -> actually RGBA straight/premul
	// Use kCGImageAlphaPremultipliedLast = 1
	ctx := cgBitmapContextCreate(unsafe.Pointer(&rgba.Pix[0]), uint64(w), uint64(h), 8, uint64(rgba.Stride), cs, 1)
	if ctx == 0 {
		return nil, errors.New("CGBitmapContextCreate failed")
	}
	defer cgContextRelease(ctx)

	cgContextSetInterpolationQuality(ctx, 3) // kCGInterpolationHigh
	cgContextDrawImage(ctx, 0, 0, float64(w), float64(h), cgImg)
	// Un-premultiply? MyGo NewBitmap accepts RGBA/NRGBA cleanly; convert premultiplied RGBA
	res := image.NewRGBA(rgba.Rect)
	copy(res.Pix, rgba.Pix)
	_ = color.NRGBAModel
	return res, nil
}
