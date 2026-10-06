import pytest


@pytest.fixture(autouse=True)
def plain_cli_output(monkeypatch):
    """Keep rich/typer help output free of ANSI codes (CI forces colors)."""
    monkeypatch.delenv("FORCE_COLOR", raising=False)
    monkeypatch.setenv("NO_COLOR", "1")
    monkeypatch.setenv("TERM", "dumb")
    monkeypatch.setenv("COLUMNS", "200")
