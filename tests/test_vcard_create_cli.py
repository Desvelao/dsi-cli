import json

import pytest
from typer.testing import CliRunner

from src.dsipy.cli.app import main_app

runner = CliRunner()

N_FIELDS = 17  # fn .. source prompts
TAIL = "n\nn\nn\nn\nn\n"  # keys, feed, custom feeds, social, custom attrs


def answers(fn="", save="y"):
    lines = [fn] + [""] * (N_FIELDS - 2) + ["https://example.com/card.vcf"]
    return "\n".join(lines) + "\n" + TAIL + save + "\n"


@pytest.fixture
def cwd(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    return tmp_path


def create(*extra, **kw):
    return runner.invoke(main_app, ["vcard", "create", "-i", *extra], **kw)


def test_tmp_removed_after_successful_save(cwd):
    result = create(input=answers(fn="Alice"))
    assert result.exit_code == 0, result.output
    assert "FN:Alice" in (cwd / "dsi-card.vcf").read_text()
    assert not (cwd / "vcard_create.tmp").exists()
    assert not (cwd / "vcard_create.tmp.part").exists()


def test_tmp_kept_on_cancel_and_resumable(cwd):
    result = create(input=answers(fn="Alice", save="n"))
    assert result.exit_code == 1
    saved = json.loads((cwd / "vcard_create.tmp").read_text())
    assert saved["fn"] == "Alice"


def test_stale_tmp_ignored_without_resume(cwd):
    (cwd / "vcard_create.tmp").write_text(json.dumps({"fn": "Stale"}))
    result = create(input=answers(fn=""))
    assert result.exit_code == 0, result.output
    assert "Stale" not in (cwd / "dsi-card.vcf").read_text()
    assert not (cwd / "vcard_create.tmp").exists()


def test_resume_uses_values_as_defaults(cwd):
    (cwd / "vcard_create.tmp").write_text(json.dumps({"fn": "Resumed"}))
    result = create("--resume", input=answers(fn=""))
    assert result.exit_code == 0, result.output
    assert "FN:Resumed" in (cwd / "dsi-card.vcf").read_text()
    assert not (cwd / "vcard_create.tmp").exists()


def test_values_with_newline_or_equals_roundtrip(cwd):
    create(input=answers(fn="a=b", save="n"))
    result = create("--resume", input=answers(fn=""))
    assert result.exit_code == 0, result.output
    assert "FN:a=b" in (cwd / "dsi-card.vcf").read_text()


@pytest.mark.parametrize(
    "content",
    ["garbage without equals\nfn=Legacy\n=x\n", "{not json", "[1, 2]", "\x00\n"],
)
def test_malformed_tmp_does_not_crash(cwd, content):
    (cwd / "vcard_create.tmp").write_text(content)
    result = create("--resume", input=answers(fn=""))
    assert result.exit_code == 0, result.output


def test_legacy_lines_are_loaded_on_resume(cwd):
    (cwd / "vcard_create.tmp").write_text("junk\nfn=Legacy\n")
    result = create("--resume", input=answers(fn=""))
    assert "FN:Legacy" in (cwd / "dsi-card.vcf").read_text()
