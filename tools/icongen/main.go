//go:build darwin

// Command icongen builds the app icon set from the source artwork,
// designs/icon/ccrouter.png. Run it through `make icon`.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/objc"
)

func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: icongen SOURCE_PNG ICONSET_DIR")
		os.Exit(2)
	}
	objc.WithAutoreleasePool(func() {
		src := appkit.NewImageWithContentsOfFile(os.Args[1])
		if src.IsNil() {
			fail(fmt.Errorf("cannot read %s", os.Args[1]))
		}
		if e := os.MkdirAll(os.Args[2], 0755); e != nil {
			fail(e)
		}
		// The artwork is free-form and nearly fills its canvas. Inside the app icon it keeps
		// the margin of the macOS icon grid, so it matches other icons in the Dock.
		for name, px := range map[string]int{
			"icon_16x16.png": 16, "icon_16x16@2x.png": 32, "icon_32x32.png": 32, "icon_32x32@2x.png": 64,
			"icon_128x128.png": 128, "icon_128x128@2x.png": 256, "icon_256x256.png": 256,
			"icon_256x256@2x.png": 512, "icon_512x512.png": 512, "icon_512x512@2x.png": 1024,
		} {
			render(src, filepath.Join(os.Args[2], name), px, float64(px)*100/1024)
		}
	})
}

func render(src appkit.Image, path string, px int, margin float64) {
	rep := appkit.NewBitmapImageRepWithBitmapDataPlanesPixelsWidePixelsHighBitsPerSampleSamplesPerPixelHasAlphaIsPlanarColorSpaceNameBytesPerRowBitsPerPixel(nil, px, px, 8, 4, true, false, appkit.DeviceRGBColorSpace, 0, 0)
	ctx := appkit.GraphicsContext_GraphicsContextWithBitmapImageRep(rep)
	appkit.GraphicsContext_SaveGraphicsState_()
	appkit.GraphicsContext_SetCurrentContext(ctx)
	ctx.SetImageInterpolation(appkit.ImageInterpolationHigh)
	side := float64(px) - 2*margin
	src.DrawInRectFromRectOperationFraction(foundation.Rect{Origin: foundation.Point{X: margin, Y: margin}, Size: foundation.Size{Width: side, Height: side}}, foundation.Rect{}, appkit.CompositingOperationSourceOver, 1)
	ctx.FlushGraphics()
	appkit.GraphicsContext_RestoreGraphicsState_()
	if e := os.WriteFile(path, rep.RepresentationUsingTypeProperties(appkit.BitmapImageFileTypePNG, nil), 0644); e != nil {
		fail(e)
	}
}

func fail(e error) {
	fmt.Fprintln(os.Stderr, "icongen:", e)
	os.Exit(1)
}
