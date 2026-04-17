//go:build ignore

// Command gen-icons produces the Shelf raster icons procedurally.
// This is a one-shot build-time utility, not part of the shipped binary.
// Run it via `go run cmd/gen-icons/main.go` from the repo root to
// regenerate:
//
//	internal/http/static/icon-192.png
//	internal/http/static/icon-512.png
//	internal/tray/icon.ico          (16/32/48 PNG-embedded icons)
//
// The design is pure geometry — a rounded-square background in the
// Shelf accent color with three stacked "shelf + book spine" bands.
// No external assets, no build-time dependencies beyond the Go stdlib.
//
// The //go:build ignore tag keeps `go build ./...` and `go test ./...`
// from trying to compile this as part of the main module — it has
// relaxed file permissions (this is a repo-local developer tool writing
// into the source tree) that wouldn't pass gosec for production code.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

func main() {
	outRoot := "."
	if len(os.Args) > 1 {
		outRoot = os.Args[1]
	}

	// Large PNGs for PWA manifest.
	for _, size := range []int{192, 512} {
		path := filepath.Join(outRoot, "internal", "http", "static", fmt.Sprintf("icon-%d.png", size))
		if err := writePNG(path, renderIcon(size)); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		fmt.Println("wrote", path)
	}

	// Windows .ico for the tray. Embeds 16/32/48 PNG sub-images per the
	// modern ICO format (Vista+ treats PNG-encoded entries natively).
	icoSizes := []int{16, 32, 48}
	icoPath := filepath.Join(outRoot, "internal", "tray", "icon.ico")
	if err := writeICO(icoPath, icoSizes); err != nil {
		log.Fatalf("write %s: %v", icoPath, err)
	}
	fmt.Println("wrote", icoPath)
}

// Design palette. Accent matches the web theme (<meta theme-color>
// #2a6f97); book spines are drawn in three muted tones so small sizes
// still read as "books on a shelf."
var (
	colorBG     = color.RGBA{R: 42, G: 111, B: 151, A: 255}  // #2a6f97
	colorShelf  = color.RGBA{R: 24, G: 70, B: 100, A: 255}   // darker shelf line
	colorSpine1 = color.RGBA{R: 230, G: 167, B: 70, A: 255}  // warm ochre
	colorSpine2 = color.RGBA{R: 213, G: 239, B: 213, A: 255} // pale green
	colorSpine3 = color.RGBA{R: 207, G: 232, B: 255, A: 255} // pale blue
	colorSpine4 = color.RGBA{R: 235, G: 195, B: 215, A: 255} // rose
)

// renderIcon returns a size×size RGBA image of the Shelf mark.
func renderIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Rounded-square background.
	radius := size * 8 / 100
	if radius < 1 {
		radius = 1
	}
	fillRoundRect(img, 0, 0, size, size, radius, colorBG)

	// Three shelves evenly spaced vertically. Each "shelf" is a thin
	// darker line with 3–4 book spines sitting on top of it.
	inset := size * 12 / 100
	shelfLeft := inset
	shelfRight := size - inset
	shelfWidth := shelfRight - shelfLeft

	spineColors := []color.RGBA{colorSpine1, colorSpine2, colorSpine3, colorSpine4}

	shelves := 3
	for i := 0; i < shelves; i++ {
		// Position shelves at roughly 30% / 55% / 80% of height.
		shelfY := size*(30+25*i)/100 + size/20
		shelfThickness := max(1, size/64)
		fillRect(img, shelfLeft, shelfY, shelfRight, shelfY+shelfThickness, colorShelf)

		// Book spines above this shelf.
		spineBottom := shelfY
		spineTop := shelfY - size*18/100
		if spineTop < inset {
			spineTop = inset
		}
		countOffset := i // rotate spine count by row: 3/4/3
		count := 3 + (countOffset % 2)
		gap := max(1, size/96)
		spineWidth := (shelfWidth - gap*(count+1)) / count
		for j := 0; j < count; j++ {
			x0 := shelfLeft + gap + j*(spineWidth+gap)
			x1 := x0 + spineWidth
			// Slight height variation so spines don't look identical.
			heightDelta := (j * size / 24) % (size / 16)
			y0 := spineTop + heightDelta
			fillRect(img, x0, y0, x1, spineBottom, spineColors[(i*3+j)%len(spineColors)])
		}
	}
	return img
}

// fillRect paints the half-open rectangle [x0,x1) × [y0,y1) with c.
func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.Set(x, y, c)
		}
	}
}

// fillRoundRect fills a rectangle with rounded corners. Corner test is
// a pure-circle inside distance check — good enough for an icon.
func fillRoundRect(img *image.RGBA, x0, y0, x1, y1, r int, c color.Color) {
	if r <= 0 {
		fillRect(img, x0, y0, x1, y1, c)
		return
	}
	// Body (sides, no corners).
	fillRect(img, x0+r, y0, x1-r, y1, c)
	fillRect(img, x0, y0+r, x0+r, y1-r, c)
	fillRect(img, x1-r, y0+r, x1, y1-r, c)

	// Corners — quarter-disk fills via squared-distance comparison.
	r2 := r * r
	for i := 0; i < r; i++ {
		for j := 0; j < r; j++ {
			dx := r - 1 - i
			dy := r - 1 - j
			if dx*dx+dy*dy <= r2 {
				img.Set(x0+i, y0+j, c)           // TL
				img.Set(x1-1-i, y0+j, c)         // TR
				img.Set(x0+i, y1-1-j, c)         // BL
				img.Set(x1-1-i, y1-1-j, c)       // BR
			}
		}
	}
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// writeICO writes a multi-resolution Windows .ico file. Each entry
// embeds a full PNG payload (supported on Windows Vista+).
func writeICO(path string, sizes []int) error {
	if len(sizes) == 0 {
		return fmt.Errorf("no icon sizes")
	}

	type entry struct {
		size int
		data []byte
	}
	entries := make([]entry, 0, len(sizes))
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, renderIcon(s)); err != nil {
			return fmt.Errorf("encode %d: %w", s, err)
		}
		entries = append(entries, entry{size: s, data: buf.Bytes()})
	}

	// ICONDIR (6 bytes): reserved(2)=0, type(2)=1, count(2)=N.
	const dirSize = 6
	const entrySize = 16
	var out bytes.Buffer
	writeLE := func(v any) { _ = binary.Write(&out, binary.LittleEndian, v) }
	writeLE(uint16(0))
	writeLE(uint16(1))
	writeLE(uint16(len(entries)))

	// Directory entries.
	offset := dirSize + entrySize*len(entries)
	for _, e := range entries {
		w := uint8(e.size)
		if e.size >= 256 {
			w = 0 // 0 means "256" in ICO
		}
		writeLE(w)                      // width
		writeLE(w)                      // height
		writeLE(uint8(0))               // palette (none)
		writeLE(uint8(0))               // reserved
		writeLE(uint16(1))              // planes
		writeLE(uint16(32))             // bit count
		writeLE(uint32(len(e.data)))    // bytes in resource
		writeLE(uint32(offset))         // offset to data
		offset += len(e.data)
	}

	// Image data in the same order.
	for _, e := range entries {
		out.Write(e.data)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
