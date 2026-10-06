import qrcode
from PIL import Image, ImageDraw, ImageFont


LOGO_MAX_FRACTION = 0.25  # logo side as a fraction of the QR modules width
CAPTION_WIDTH_FRACTION = 0.90  # caption width as a fraction of the image width


def _fit_font(font_path: str, text: str, max_width: float, max_height: float):
    """Return the largest font that keeps the text within max_width x max_height."""
    best = ImageFont.truetype(font_path, 1)
    size = 1
    while size < 1000:
        candidate = ImageFont.truetype(font_path, size)
        left, top, right, bottom = candidate.getbbox(text)
        if right - left > max_width or bottom - top > max_height:
            break
        best = candidate
        size += 1
    return best


def _draw_caption(draw, text, font_path, qr_width, band_top, band_height, max_w, max_h):
    """Draw text centered (horizontally and vertically) in a horizontal band."""
    font = _fit_font(font_path, text, max_w, max_h)
    left, top, right, bottom = draw.textbbox((0, 0), text, font=font)
    x = (qr_width - (right - left)) / 2 - left
    y = band_top + (band_height - (bottom - top)) / 2 - top
    draw.text((x, y), text, font=font, fill=(0, 0, 0))


def generate_qr(
    image: str = "",
    output: str = "",
    data: str = "",
    caption_top: str = "",
    caption_bottom: str = "",
    font: str = "",
):
    """
    Generate a QR code
    """

    if not output:
        raise ValueError("Output file path is required")

    if not data:
        raise ValueError("Data for QR code is required")
    QRcode = qrcode.QRCode(error_correction=qrcode.constants.ERROR_CORRECT_H)

    # adding URL or text to QRcode
    QRcode.add_data(data)

    # generating QR code
    QRcode.make()

    # taking color name from user
    QRcolor = "Green"
    # adding color to QR code
    # QRimg = QRcode.make_image(
    #     fill_color=QRcolor, back_color="white").convert('RGB')

    QRimg = QRcode.make_image(fill_color="Black", back_color="white").convert("RGB")

    # taking image which user wants
    # in the QR code center

    qr_width, qr_height = QRimg.size
    border_px = QRcode.border * QRcode.box_size
    modules_px = qr_width - 2 * border_px

    if image:
        logo_img = Image.open(image)

        # keep the logo small enough for the error correction (level H) to cope
        max_logo = max(1, int(modules_px * LOGO_MAX_FRACTION))
        ratio = min(max_logo / logo_img.size[0], max_logo / logo_img.size[1])
        logo_size = (
            max(1, int(logo_img.size[0] * ratio)),
            max(1, int(logo_img.size[1] * ratio)),
        )
        logo_img = logo_img.convert("RGBA").resize(logo_size, Image.LANCZOS)

        pos = (
            (qr_width - logo_size[0]) // 2,
            (qr_height - logo_size[1]) // 2,
        )
        QRimg.paste(logo_img, pos, logo_img)

    if caption_top or caption_bottom:
        if not font:
            raise ValueError("Font file is required for caption")
        draw = ImageDraw.Draw(QRimg)
        # captions live in the quiet zone (border) above/below the modules
        margin = max(1, border_px // 8)
        max_height = border_px - 2 * margin
        max_width = CAPTION_WIDTH_FRACTION * qr_width

        if caption_top:
            _draw_caption(
                draw, caption_top, font, qr_width, 0, border_px, max_width, max_height
            )
        if caption_bottom:
            _draw_caption(
                draw,
                caption_bottom,
                font,
                qr_width,
                qr_height - border_px,
                border_px,
                max_width,
                max_height,
            )

    # save the QR code generated
    QRimg.save(output)

    print("QR code generated!")
