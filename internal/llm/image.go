package llm

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// DataURL reads an image file and returns a base64 data URL, downscaling it
// first when maxEdge is set and the image is larger. Remote endpoints cannot
// reach local paths, so the bytes always travel inline.
func DataURL(path string, maxEdge int) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(path))

	// Formats the standard library cannot decode travel as-is.
	if maxEdge <= 0 || !decodableExt(ext) {
		return encodeDataURL(mimeOf(ext), raw), nil
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return encodeDataURL(mimeOf(ext), raw), nil
	}
	b := img.Bounds()
	if b.Dx() <= maxEdge && b.Dy() <= maxEdge {
		return encodeDataURL(mimeOf(ext), raw), nil
	}

	scaled := downscale(img, maxEdge)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 88}); err != nil {
		return encodeDataURL(mimeOf(ext), raw), nil
	}
	return encodeDataURL("image/jpeg", buf.Bytes()), nil
}

func decodableExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

func mimeOf(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	}
	return "image/jpeg"
}

func encodeDataURL(mime string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
}

// downscale box-filters an image so its longest edge equals maxEdge. A box
// filter is enough for feeding a vision model and avoids pulling in an
// external resizing dependency.
func downscale(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	scale := float64(maxEdge) / float64(max(sw, sh))
	dw := max(1, int(float64(sw)*scale))
	dh := max(1, int(float64(sh)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0 := b.Min.Y + y*sh/dh
		y1 := b.Min.Y + (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := range dw {
			x0 := b.Min.X + x*sw/dw
			x1 := b.Min.X + (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, as, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, bl, a := src.At(sx, sy).RGBA()
					rs += uint64(r >> 8)
					gs += uint64(g >> 8)
					bs += uint64(bl >> 8)
					as += uint64(a >> 8)
					n++
				}
			}
			if n == 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = uint8(rs / n)
			dst.Pix[i+1] = uint8(gs / n)
			dst.Pix[i+2] = uint8(bs / n)
			dst.Pix[i+3] = uint8(as / n)
		}
	}
	return dst
}
