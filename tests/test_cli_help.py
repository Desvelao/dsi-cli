import pytest
from typer.testing import CliRunner

from src.dsipy.cli.app import main_app

runner = CliRunner()

SUBAPPS = {
    "vcard": "vCard processing",
    "feeds": "feeds processing",
    "connections": "connections processing",
    "key": "related to keys",
}


def test_top_level_help_lists_subapp_descriptions():
    result = runner.invoke(main_app, ["--help"])
    assert result.exit_code == 0
    assert "DSI Tools" in result.output
    for text in SUBAPPS.values():
        assert text in result.output


@pytest.mark.parametrize("name,text", SUBAPPS.items())
def test_subapp_help(name, text):
    result = runner.invoke(main_app, [name, "--help"])
    assert result.exit_code == 0
    assert text in result.output
    assert "generate RSS feeds from markdown files" not in result.output


def test_feeds_add_has_help():
    result = runner.invoke(main_app, ["feeds", "--help"])
    line = next(l for l in result.output.splitlines() if " add " in l)
    assert "Create a new feed" in line
