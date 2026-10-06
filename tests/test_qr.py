import glob
import os

import pytest
from PIL import Image, ImageFont

from src.dsipy.vcard.qr import _fit_font, generate_qr


def _find_font():
    candidates = []
    try:
        import matplotlib  # noqa: F401
    except Exception:
        pass
    for pattern in (
        "/usr/share/fonts/**/DejaVuSans.ttf",
        "/usr/share/fonts/**/*Regular.ttf",
        "/usr/share/fonts/**/*.ttf",
        "/usr/local/share/fonts/**/*.ttf",
        "/Library/Fonts/*.ttf",
        "C:/Windows/Fonts/*.ttf",
    ):
        candidates += glob.glob(pattern, recursive=True)
    for path in candidates:
        try:
            ImageFont.truetype(path, 12)
            return path
        except Exception:
            continue
    return None


FONT = _find_font()
needs_font = pytest.mark.skipif(FONT is None, reason="no TrueType font on system")

DATA = "https://example.com/vcard"


def _plain_size(tmp_path):
    out = tmp_path / "plain.png"
    generate_qr(output=str(out), data=DATA)
    return Image.open(out).size


def test_plain_qr(tmp_path):
    out = tmp_path / "qr.png"
    generate_qr(output=str(out), data=DATA)
    assert out.exists()
    w, h = Image.open(out).size
    assert w == h and w > 100


def test_requires_output_and_data(tmp_path):
    with pytest.raises(ValueError):
        generate_qr(data=DATA)
    with pytest.raises(ValueError):
        generate_qr(output=str(tmp_path / "x.png"))


@needs_font
@pytest.mark.parametrize(
    "top,bottom", [("Top caption", ""), ("", "Bottom caption"), ("Top", "Bottom")]
)
def test_captions(tmp_path, top, bottom):
    out = tmp_path / "qr.png"
    generate_qr(
        output=str(out), data=DATA, caption_top=top, caption_bottom=bottom, font=FONT
    )
    img = Image.open(out)
    assert img.size == _plain_size(tmp_path)
    # caption pixels must appear in the quiet-zone band (all white without it)
    border = 40
    gray = img.convert("L")
    if top:
        assert gray.crop((0, 0, img.width, border)).getextrema()[0] < 128
    if bottom:
        assert gray.crop((0, img.height - border, img.width, img.height)).getextrema()[0] < 128


@needs_font
def test_caption_requires_font(tmp_path):
    with pytest.raises(ValueError):
        generate_qr(output=str(tmp_path / "q.png"), data=DATA, caption_top="x")


@needs_font
def test_fit_font_is_largest_that_fits():
    text = "Hello world"
    font = _fit_font(FONT, text, 200, 30)
    l, t, r, b = font.getbbox(text)
    assert r - l <= 200 and b - t <= 30
    bigger = ImageFont.truetype(FONT, font.size + 1)
    l, t, r, b = bigger.getbbox(text)
    assert r - l > 200 or b - t > 30


def test_logo_scaled_relative_to_qr(tmp_path):
    logo = tmp_path / "logo.png"
    Image.new("RGBA", (600, 300), (255, 0, 0, 255)).save(logo)
    out = tmp_path / "qr.png"
    generate_qr(image=str(logo), output=str(out), data=DATA)
    img = Image.open(out).convert("RGB")
    assert img.size == _plain_size(tmp_path)
    # centre is logo red; logo width stays under a third of the image
    assert img.getpixel((img.width // 2, img.height // 2)) == (255, 0, 0)
    reds = [x for x in range(img.width) if img.getpixel((x, img.height // 2)) == (255, 0, 0)]
    assert 0 < len(reds) < img.width / 3
