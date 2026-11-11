//go:build ignore

// Export the approved raster masters at the sizes used by the bundle and UI.
package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

func readPNG(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	return img
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	fmt.Println(path)
}

func appIcon(master image.Image, size int) image.Image {
	scaled := image.NewRGBA(image.Rect(0, 0, size, size))
	inset := size * 4 / 100
	box := image.Rect(inset, inset, size-inset, size-inset)
	xdraw.CatmullRom.Scale(scaled, box, master, master.Bounds(), draw.Src, nil)
	mask := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect x="4" y="4" width="92" height="92" rx="22" fill="white"/></svg>`)).Image(size, size, nil)
	dst := image.NewRGBA(scaled.Bounds())
	draw.DrawMask(dst, dst.Bounds(), scaled, image.Point{}, mask, image.Point{}, draw.Src)
	return dst
}

func main() {
	master := readPNG("resources/brand/apilot-sunset.png")
	template := readPNG("resources/brand/apilot-template.png")
	transparent, ink := 0, 0
	for y := template.Bounds().Min.Y; y < template.Bounds().Max.Y; y++ {
		for x := template.Bounds().Min.X; x < template.Bounds().Max.X; x++ {
			_, _, _, a := template.At(x, y).RGBA()
			if a == 0 {
				transparent++
			}
			if a > 0 {
				ink++
			}
		}
	}
	if transparent == 0 || ink == 0 {
		panic("menu-bar master must contain both transparent space and artwork")
	}
	writePNG("resources/icon.png", appIcon(master, 1024))
	writePNG("internal/ui/assets/app.png", appIcon(master, 128))
	tray := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	xdraw.CatmullRom.Scale(tray, tray.Bounds(), template, template.Bounds(), draw.Src, nil)
	// A template is alpha-only; discard any residual generated color.
	for i := 0; i < len(tray.Pix); i += 4 {
		tray.Pix[i] = 0
		tray.Pix[i+1] = 0
		tray.Pix[i+2] = 0
	}
	writePNG("internal/ui/assets/tray.png", tray)
}
