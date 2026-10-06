from pathlib import Path

from PIL import Image
from typer.testing import CliRunner

from src.dsipy.cli.app import main_app
from tests.test_qr import FONT, needs_font

runner = CliRunner()


def test_qr_plain_text_data(tmp_path):
    out = tmp_path / "q.png"
    result = runner.invoke(main_app, ["vcard", "qr", "hello world", "-o", str(out)])
    assert result.exit_code == 0, result.output
    assert Image.open(out).size[0] > 0


def test_qr_from_file(tmp_path):
    src = tmp_path / "c.vcf"
    src.write_text("BEGIN:VCARD\nVERSION:4.0\nFN:A\nEND:VCARD\n")
    out = tmp_path / "q.png"
    result = runner.invoke(main_app, ["vcard", "qr", str(src), "-o", str(out)])
    assert result.exit_code == 0, result.output
    assert out.exists()


@needs_font
def test_qr_with_caption_and_font(tmp_path):
    out = tmp_path / "q.png"
    plain = tmp_path / "p.png"
    runner.invoke(main_app, ["vcard", "qr", "data", "-o", str(plain)])
    result = runner.invoke(
        main_app,
        ["vcard", "qr", "data", "-o", str(out), "--caption-top", "Hi", "--font", FONT],
    )
    assert result.exit_code == 0, result.output
    assert Image.open(out).tobytes() != Image.open(plain).tobytes()


def test_qr_caption_missing_font(tmp_path):
    out = tmp_path / "q.png"
    result = runner.invoke(
        main_app,
        [
            "vcard",
            "qr",
            "data",
            "-o",
            str(out),
            "--caption-top",
            "Hi",
            "--font",
            str(tmp_path / "nope.ttf"),
        ],
    )
    assert result.exit_code == 1
    assert "font file does not exist" in result.output
    assert not out.exists()


def test_qr_caption_without_font(tmp_path):
    result = runner.invoke(
        main_app,
        ["vcard", "qr", "data", "-o", str(tmp_path / "q.png"), "-t", "Hi"],
    )
    assert result.exit_code == 1
    assert "font file must be specified" in result.output
