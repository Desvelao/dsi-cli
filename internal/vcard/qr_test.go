package vcard

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/Desvelao/dsipy/internal/testutil"
)

const qrData = "https://example.com/vcard"

func loadImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// decodeQR reads the QR code of an image with an independent decoder.
func decodeQR(t *testing.T, img image.Image) string {
	t.Helper()
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, hints)
	if err != nil {
		t.Fatalf("QR code does not decode (%dx%d): %v", img.Bounds().Dx(), img.Bounds().Dy(), err)
	}
	return res.GetText()
}

func TestQRMatchesPythonSizeAndDecodes(t *testing.T) {
	var cases []struct {
		Data string
		Rows []string
	}
	testutil.GoldenJSON(t, "qr/matrices.json", &cases)
	exact := 0
	for _, c := range cases {
		out := filepath.Join(t.TempDir(), "qr.png")
		if err := GenerateQR(QROptions{Output: out, Data: c.Data}); err != nil {
			t.Fatal(err)
		}
		img := loadImage(t, out)
		if want := len(c.Rows) * 10; img.Bounds().Dx() != want || img.Bounds().Dy() != want {
			t.Errorf("%.20q: size %v, want %dx%d", c.Data, img.Bounds().Size(), want, want)
		}
		if got := decodeQR(t, img); got != c.Data {
			t.Errorf("decoded %q, want %q", got, c.Data)
		}
		// count identical module matrices (same mask/segmentation as Python's qrcode)
		same := true
		for y, row := range c.Rows {
			for x, ch := range row {
				r, _, _, _ := img.At(x*10+5, y*10+5).RGBA()
				if (r == 0) != (ch == '1') {
					same = false
				}
			}
		}
		if same {
			exact++
		}
	}
	t.Logf("%d/%d QR matrices identical to Python's, all decode", exact, len(cases))
}

func TestQRRequiresOutputAndData(t *testing.T) {
	if err := GenerateQR(QROptions{Data: qrData}); err == nil {
		t.Error("output is required")
	}
	if err := GenerateQR(QROptions{Output: filepath.Join(t.TempDir(), "x.png")}); err == nil {
		t.Error("data is required")
	}
	if err := GenerateQR(QROptions{Output: filepath.Join(t.TempDir(), "x.xyz"), Data: qrData}); err == nil || !strings.Contains(err.Error(), "unknown file extension: .xyz") {
		t.Errorf("unknown extension: %v", err)
	}
	if err := GenerateQR(QROptions{Output: filepath.Join(t.TempDir(), "x.png"), Data: qrData, CaptionTop: "x"}); err == nil {
		t.Error("caption without font must fail")
	}
}

func TestQRFormats(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".gif"} {
		out := filepath.Join(t.TempDir(), "qr"+ext)
		if err := GenerateQR(QROptions{Output: out, Data: qrData}); err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got := decodeQR(t, loadImage(t, out)); got != qrData {
			t.Errorf("%s decodes to %q", ext, got)
		}
	}
}

func writeFont(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "font.ttf")
	if err := os.WriteFile(path, goregular.TTF, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestQRCaptionsDrawInTheQuietZone(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain.png")
	GenerateQR(QROptions{Output: plain, Data: qrData})
	plainImg := loadImage(t, plain)
	font := writeFont(t)

	for _, c := range []struct{ top, bottom string }{{"Top caption", ""}, {"", "Bottom caption"}, {"Top", "Bottom"}} {
		out := filepath.Join(t.TempDir(), "qr.png")
		if err := GenerateQR(QROptions{Output: out, Data: qrData, CaptionTop: c.top, CaptionBottom: c.bottom, Font: font}); err != nil {
			t.Fatal(err)
		}
		img := loadImage(t, out)
		if img.Bounds() != plainImg.Bounds() {
			t.Errorf("size changed with captions: %v", img.Bounds())
		}
		dark := func(y0, y1 int) bool {
			for y := y0; y < y1; y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 {
						return true
					}
				}
			}
			return false
		}
		h := img.Bounds().Dy()
		if (c.top != "") != dark(0, 40) || (c.bottom != "") != dark(h-40, h) {
			t.Errorf("captions %+v: wrong pixels in the quiet zone", c)
		}
		if got := decodeQR(t, img); got != qrData {
			t.Errorf("captions break decoding: %q", got)
		}
	}
}

func TestQRLogoStaysDecodable(t *testing.T) {
	logo := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			logo.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	dir := t.TempDir()
	logoPath := filepath.Join(dir, "logo.png")
	f, _ := os.Create(logoPath)
	png.Encode(f, logo)
	f.Close()

	out := filepath.Join(dir, "qr.png")
	if err := GenerateQR(QROptions{Output: out, Data: qrData, Image: logoPath}); err != nil {
		t.Fatal(err)
	}
	img := loadImage(t, out)
	// the logo is centered and red
	r, g, _, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	if r < 0xc000 || g > 0x4000 {
		t.Errorf("center pixel is not the logo color: r=%x g=%x", r, g)
	}
	if got := decodeQR(t, img); got != qrData {
		t.Errorf("logo breaks decoding: %q", got)
	}
	if err := GenerateQR(QROptions{Output: out, Data: qrData, Image: filepath.Join(dir, "missing.png")}); err == nil {
		t.Error("missing logo must fail")
	}
}
