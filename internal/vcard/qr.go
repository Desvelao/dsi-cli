package vcard

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp" // logo decoding
	"rsc.io/qr"
)

const (
	qrBorder             = 4  // modules of quiet zone
	qrBoxSize            = 10 // pixels per module
	logoMaxFraction      = 0.25
	captionWidthFraction = 0.90
)

// QROptions configures GenerateQR.
type QROptions struct {
	Image         string // optional logo drawn in the center
	Output        string // output file (.png, .jpg, .jpeg or .gif)
	Data          string
	CaptionTop    string
	CaptionBottom string
	Font          string // TrueType/OpenType font file, required for captions
}

// GenerateQR renders a QR code (error correction level H, 10px modules, 4
// module quiet zone) with an optional logo and captions in the quiet zone.
func GenerateQR(o QROptions) error {
	if o.Output == "" {
		return errors.New("Output file path is required")
	}
	if o.Data == "" {
		return errors.New("Data for QR code is required")
	}
	code, err := qr.Encode(o.Data, qr.H)
	if err != nil {
		return err
	}
	n := code.Size
	size := (n + 2*qrBorder) * qrBoxSize
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if code.Black(x, y) {
				px := image.Rect((x+qrBorder)*qrBoxSize, (y+qrBorder)*qrBoxSize, (x+qrBorder+1)*qrBoxSize, (y+qrBorder+1)*qrBoxSize)
				draw.Draw(img, px, image.NewUniform(color.Black), image.Point{}, draw.Src)
			}
		}
	}
	borderPx := qrBorder * qrBoxSize
	modulesPx := size - 2*borderPx

	if o.Image != "" {
		if err := drawLogo(img, o.Image, modulesPx); err != nil {
			return err
		}
	}
	if o.CaptionTop != "" || o.CaptionBottom != "" {
		if o.Font == "" {
			return errors.New("Font file is required for caption")
		}
		fontFile, err := os.ReadFile(o.Font)
		if err != nil {
			return err
		}
		parsed, err := opentype.Parse(fontFile)
		if err != nil {
			return fmt.Errorf("cannot open font resource: %w", err)
		}
		// captions live in the quiet zone (border) above/below the modules
		margin := borderPx / 8
		if margin < 1 {
			margin = 1
		}
		maxHeight := float64(borderPx - 2*margin)
		maxWidth := captionWidthFraction * float64(size)
		if o.CaptionTop != "" {
			drawCaption(img, o.CaptionTop, parsed, 0, borderPx, maxWidth, maxHeight)
		}
		if o.CaptionBottom != "" {
			drawCaption(img, o.CaptionBottom, parsed, size-borderPx, borderPx, maxWidth, maxHeight)
		}
	}
	return saveImage(o.Output, img)
}

func drawLogo(dst *image.RGBA, path string, modulesPx int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	logo, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("cannot identify image file '%s': %w", path, err)
	}
	maxLogo := int(float64(modulesPx) * logoMaxFraction)
	if maxLogo < 1 {
		maxLogo = 1
	}
	w, h := logo.Bounds().Dx(), logo.Bounds().Dy()
	ratio := min(float64(maxLogo)/float64(w), float64(maxLogo)/float64(h))
	lw, lh := max(1, int(float64(w)*ratio)), max(1, int(float64(h)*ratio))

	src := image.NewNRGBA(logo.Bounds())
	draw.Draw(src, src.Bounds(), logo, logo.Bounds().Min, draw.Src)
	scaled := image.NewNRGBA(image.Rect(0, 0, lw, lh))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Src, nil)

	x0, y0 := (dst.Bounds().Dx()-lw)/2, (dst.Bounds().Dy()-lh)/2
	draw.Draw(dst, image.Rect(x0, y0, x0+lw, y0+lh), scaled, image.Point{}, draw.Over)
	return nil
}

func faceAt(f *opentype.Font, size int) (font.Face, error) {
	return opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
}

// fitFont returns the largest face whose text fits maxWidth x maxHeight.
func fitFont(f *opentype.Font, text string, maxWidth, maxHeight float64) font.Face {
	best, _ := faceAt(f, 1)
	for size := 1; size < 1000; size++ {
		face, err := faceAt(f, size)
		if err != nil {
			break
		}
		b, _ := font.BoundString(face, text)
		w, h := fixedToFloat(b.Max.X-b.Min.X), fixedToFloat(b.Max.Y-b.Min.Y)
		if w > maxWidth || h > maxHeight {
			break
		}
		best = face
	}
	return best
}

func fixedToFloat(v fixed.Int26_6) float64 { return float64(v) / 64 }

// drawCaption draws text centered horizontally and vertically in a band.
func drawCaption(img *image.RGBA, text string, f *opentype.Font, bandTop, bandHeight int, maxW, maxH float64) {
	face := fitFont(f, text, maxW, maxH)
	b, _ := font.BoundString(face, text)
	centerX := float64(img.Bounds().Dx()) / 2
	centerY := float64(bandTop) + float64(bandHeight)/2
	x := centerX - (fixedToFloat(b.Min.X)+fixedToFloat(b.Max.X))/2
	baseline := centerY - (fixedToFloat(b.Min.Y)+fixedToFloat(b.Max.Y))/2
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.Black),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(baseline * 64)},
	}
	d.DrawString(text)
}

func saveImage(path string, img image.Image) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".png":
		return png.Encode(out, img)
	case ".jpg", ".jpeg":
		return jpeg.Encode(out, img, &jpeg.Options{Quality: 75})
	case ".gif":
		return gif.Encode(out, img, nil)
	default:
		return fmt.Errorf("unknown file extension: %s", ext)
	}
}
